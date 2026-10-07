// Package orgchange は組織変更と異動を扱う（docs/plan.md「2.15 組織変更と異動」）。
//
//   - 組織変更の予約: 施策の移動・ユニットの所属の変更・ユニットの統合・担当者の変更をまとめて登録し、有効日に自動で適用する。
//     適用は全部か無しか。失敗したら予約を失敗にして理由を残し、FP&A にお知らせを送る
//   - ユニットの統合と廃止
//   - 無効化のときの後任者への付け替え
//
// 集計は常に今の組織で行う（組替え）ので、変更の後は過去の版も新しい組織で集計される。
package orgchange

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/rnapyzz/f-panda-app/api/internal/audit"
	"github.com/rnapyzz/f-panda-app/api/internal/dbx"
	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
)

// 変更の種類。
const (
	KindMoveActivity = "move_activity" // 施策のユニットの移動
	KindMoveUnit     = "move_unit"     // ユニットの所属（セグメント・組織）の変更
	KindMergeUnit    = "merge_unit"    // ユニットの統合
	KindChangeOwner  = "change_owner"  // 施策の担当者・ユニットのマネージャーの変更
)

// item は予約の1つの変更。
type item struct {
	ID             int64  `json:"id"`
	Kind           string `json:"kind"`
	ActivityID     *int64 `json:"activity_id"`
	UnitID         *int64 `json:"unit_id"`
	TargetUnitID   *int64 `json:"target_unit_id"`
	SegmentID      *int64 `json:"segment_id"`
	OrganizationID *int64 `json:"organization_id"`
	OwnerUserID    *int64 `json:"owner_user_id"`
}

// activityRow・unitRow は監査ログに残す施策・ユニット（変えた項目を含む一部）。
type activityRow struct {
	ID          int64  `json:"id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	UnitID      int64  `json:"unit_id"`
	OwnerUserID *int64 `json:"owner_user_id"`
}

type unitRow struct {
	ID             int64  `json:"id"`
	Code           string `json:"code"`
	Name           string `json:"name"`
	SegmentID      int64  `json:"segment_id"`
	OrganizationID int64  `json:"organization_id"`
	OwnerUserID    *int64 `json:"owner_user_id"`
	IsArchived     bool   `json:"is_archived"`
}

// itemError は何番目の変更で失敗したかを含むエラー。
type itemError struct {
	index int
	err   error
}

func (e *itemError) Error() string {
	return fmt.Sprintf("%d 件目の変更: %s", e.index+1, message(e.err))
}

// message は API のエラーなら、利用者向けのメッセージ（項目ごとの内容を含む）を返す。
func message(err error) string {
	var apiErr *httpx.Error
	if errors.As(err, &apiErr) {
		if len(apiErr.Details) > 0 {
			for _, m := range apiErr.Details {
				return m
			}
		}
		return apiErr.Message
	}
	return err.Error()
}

func findActivity(ctx context.Context, tx *sql.Tx, id int64) (activityRow, error) {
	var a activityRow
	var owner sql.NullInt64
	err := tx.QueryRowContext(ctx, "SELECT id, code, name, unit_id, owner_user_id FROM activities WHERE id = ? FOR UPDATE", id).
		Scan(&a.ID, &a.Code, &a.Name, &a.UnitID, &owner)
	if errors.Is(err, sql.ErrNoRows) {
		return a, httpx.Conflict(fmt.Sprintf("施策 #%d が見つかりません（削除された可能性があります）", id))
	}
	a.OwnerUserID = dbx.PtrInt64(owner)
	return a, err
}

func findUnit(ctx context.Context, tx *sql.Tx, id int64) (unitRow, error) {
	var u unitRow
	var owner sql.NullInt64
	err := tx.QueryRowContext(ctx, "SELECT id, code, name, segment_id, organization_id, owner_user_id, is_archived FROM units WHERE id = ? FOR UPDATE", id).
		Scan(&u.ID, &u.Code, &u.Name, &u.SegmentID, &u.OrganizationID, &owner, &u.IsArchived)
	if errors.Is(err, sql.ErrNoRows) {
		return u, httpx.Conflict(fmt.Sprintf("ユニット #%d が見つかりません（削除された可能性があります）", id))
	}
	u.OwnerUserID = dbx.PtrInt64(owner)
	return u, err
}

// activeUnit は、施策を所属させられる（廃止していない）ユニットを返す。
func activeUnit(ctx context.Context, tx *sql.Tx, id int64) (unitRow, error) {
	u, err := findUnit(ctx, tx, id)
	if err == nil && u.IsArchived {
		return u, httpx.Conflict(fmt.Sprintf("ユニット「%s」は廃止されています", u.Name))
	}
	return u, err
}

// checkLeaf は、ノード（セグメント・組織）があり、子を持たない末端であることを確認する。
func checkLeaf(ctx context.Context, tx *sql.Tx, table, label string, id int64) error {
	var name string
	err := tx.QueryRowContext(ctx, "SELECT name FROM "+table+" WHERE id = ? FOR SHARE", id).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return httpx.Conflict(fmt.Sprintf("%s #%d が見つかりません", label, id))
	}
	if err != nil {
		return err
	}
	n, err := dbx.Count(ctx, tx, "SELECT COUNT(*) FROM "+table+" WHERE parent_id = ?", id)
	if err != nil {
		return err
	}
	if n > 0 {
		return httpx.Conflict(fmt.Sprintf("%s「%s」は子を持つため、ユニットを所属させられません（末端のノードを選んでください）", label, name))
	}
	return nil
}

// checkActiveUser は、担当者にするユーザーが有効であることを確認する。nil（未設定）はよい。
func checkActiveUser(ctx context.Context, tx *sql.Tx, id *int64) error {
	if id == nil {
		return nil
	}
	var name string
	var active bool
	err := tx.QueryRowContext(ctx, "SELECT name, is_active FROM users WHERE id = ? FOR SHARE", *id).Scan(&name, &active)
	if errors.Is(err, sql.ErrNoRows) {
		return httpx.Conflict(fmt.Sprintf("ユーザー #%d が見つかりません", *id))
	}
	if err != nil {
		return err
	}
	if !active {
		return httpx.Conflict(fmt.Sprintf("ユーザー「%s」は無効です", name))
	}
	return nil
}

// moveActivity は施策を別のユニットへ移す。
func moveActivity(ctx context.Context, tx *sql.Tx, rec *audit.Recorder, activityID, unitID int64) error {
	before, err := findActivity(ctx, tx, activityID)
	if err != nil {
		return err
	}
	if _, err := activeUnit(ctx, tx, unitID); err != nil {
		return err
	}
	if before.UnitID == unitID {
		return nil
	}
	if _, err := tx.ExecContext(ctx, "UPDATE activities SET unit_id = ? WHERE id = ?", unitID, activityID); err != nil {
		return err
	}
	after := before
	after.UnitID = unitID
	return rec.Update(ctx, "activities", activityID, before, after)
}

// moveUnit はユニットの所属（セグメント・組織の末端ノード）を変える。
func moveUnit(ctx context.Context, tx *sql.Tx, rec *audit.Recorder, unitID, segmentID, organizationID int64) error {
	before, err := activeUnit(ctx, tx, unitID)
	if err != nil {
		return err
	}
	if err := checkLeaf(ctx, tx, "segments", "セグメント", segmentID); err != nil {
		return err
	}
	if err := checkLeaf(ctx, tx, "organizations", "組織", organizationID); err != nil {
		return err
	}
	if before.SegmentID == segmentID && before.OrganizationID == organizationID {
		return nil
	}
	if _, err := tx.ExecContext(ctx, "UPDATE units SET segment_id = ?, organization_id = ? WHERE id = ?", segmentID, organizationID, unitID); err != nil {
		return err
	}
	after := before
	after.SegmentID, after.OrganizationID = segmentID, organizationID
	return rec.Update(ctx, "units", unitID, before, after)
}

// MergeUnit はユニットを統合する。施策をすべて統合先へ移し、元のユニットを廃止にする。移した施策の数を返す。
func MergeUnit(ctx context.Context, tx *sql.Tx, rec *audit.Recorder, unitID, targetID int64) (int, error) {
	if unitID == targetID {
		return 0, httpx.Conflict("統合先に自分自身は選べません")
	}
	before, err := activeUnit(ctx, tx, unitID)
	if err != nil {
		return 0, err
	}
	if _, err := activeUnit(ctx, tx, targetID); err != nil {
		return 0, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT id FROM activities WHERE unit_id = ? ORDER BY id FOR UPDATE", unitID)
	if err != nil {
		return 0, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, id := range ids {
		if err := moveActivity(ctx, tx, rec, id, targetID); err != nil {
			return 0, err
		}
	}
	if err := setArchived(ctx, tx, rec, before, true); err != nil {
		return 0, err
	}
	return len(ids), nil
}

func setArchived(ctx context.Context, tx *sql.Tx, rec *audit.Recorder, before unitRow, archived bool) error {
	if _, err := tx.ExecContext(ctx, "UPDATE units SET is_archived = ? WHERE id = ?", archived, before.ID); err != nil {
		return err
	}
	after := before
	after.IsArchived = archived
	return rec.Update(ctx, "units", before.ID, before, after)
}

// changeActivityOwner は施策の担当者を変える（nil は未設定）。
func changeActivityOwner(ctx context.Context, tx *sql.Tx, rec *audit.Recorder, activityID int64, owner *int64) error {
	before, err := findActivity(ctx, tx, activityID)
	if err != nil {
		return err
	}
	if err := checkActiveUser(ctx, tx, owner); err != nil {
		return err
	}
	if equal(before.OwnerUserID, owner) {
		return nil
	}
	if _, err := tx.ExecContext(ctx, "UPDATE activities SET owner_user_id = ? WHERE id = ?", dbx.NullInt64(owner), activityID); err != nil {
		return err
	}
	after := before
	after.OwnerUserID = owner
	return rec.Update(ctx, "activities", activityID, before, after)
}

// changeUnitOwner はユニットのマネージャーを変える（nil は未設定）。
func changeUnitOwner(ctx context.Context, tx *sql.Tx, rec *audit.Recorder, unitID int64, owner *int64) error {
	before, err := findUnit(ctx, tx, unitID)
	if err != nil {
		return err
	}
	if err := checkActiveUser(ctx, tx, owner); err != nil {
		return err
	}
	if equal(before.OwnerUserID, owner) {
		return nil
	}
	if _, err := tx.ExecContext(ctx, "UPDATE units SET owner_user_id = ? WHERE id = ?", dbx.NullInt64(owner), unitID); err != nil {
		return err
	}
	after := before
	after.OwnerUserID = owner
	return rec.Update(ctx, "units", unitID, before, after)
}

func equal(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// applyItems は変更を順に適用する。どれかが失敗したら、何番目の変更かを含むエラーを返す（呼び出し側でロールバックする）。
func applyItems(ctx context.Context, tx *sql.Tx, rec *audit.Recorder, items []item) error {
	for i, it := range items {
		var err error
		switch it.Kind {
		case KindMoveActivity:
			err = moveActivity(ctx, tx, rec, *it.ActivityID, *it.TargetUnitID)
		case KindMoveUnit:
			err = moveUnit(ctx, tx, rec, *it.UnitID, *it.SegmentID, *it.OrganizationID)
		case KindMergeUnit:
			_, err = MergeUnit(ctx, tx, rec, *it.UnitID, *it.TargetUnitID)
		case KindChangeOwner:
			if it.ActivityID != nil {
				err = changeActivityOwner(ctx, tx, rec, *it.ActivityID, it.OwnerUserID)
			} else {
				err = changeUnitOwner(ctx, tx, rec, *it.UnitID, it.OwnerUserID)
			}
		default:
			err = fmt.Errorf("変更の種類 %q はありません", it.Kind)
		}
		if err != nil {
			return &itemError{index: i, err: err}
		}
	}
	return nil
}
