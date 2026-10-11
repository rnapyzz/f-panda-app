package scenario

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rnapyzz/f-panda-app/api/internal/activity"
	"github.com/rnapyzz/f-panda-app/api/internal/auth"
	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
)

// 説明へのコメント（docs/plan.md「2.24」）。施策 × シナリオの今回の見込の説明に、関係者がコメントを書いてやり取りする。
//
// 施策を見られる人（ログインユーザー全員）が書ける。書いた本人は編集・削除できる。ロック済みのシナリオには書けない・編集・削除できない。
// コメントは値ではないので、変更セット・監査ログには残さず、更新の状態（未着手・入力中・完了）も変えない。
// 書くと、施策の担当者（いなければユニットのマネージャー）と、そのやり取りにすでに書いた人にアプリ内のお知らせを送る。

const commentMaxLen = 1000

// comment はコメントの1件。消したコメントは本文を返さない。
type comment struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"user_id"`
	UserName  string    `json:"user_name"`
	Body      string    `json:"body"`
	Deleted   bool      `json:"deleted"`
	CreatedAt time.Time `json:"created_at"`
	// EditedAt は最後に編集した日時（編集していなければ null）。画面に「編集済み」と出す
	EditedAt *time.Time `json:"edited_at"`
	// CanEdit・CanDelete はログインユーザーが編集・削除できるか（書いた本人で、消しておらず、シナリオがロックされていない）
	CanEdit   bool `json:"can_edit"`
	CanDelete bool `json:"can_delete"`
}

type commentList struct {
	Items []comment `json:"items"`
	// CanPost はコメントを書けるか（シナリオがロックされていない）
	CanPost bool `json:"can_post"`
}

func (h *Handler) listComments(w http.ResponseWriter, r *http.Request) error {
	scenarioID, activityID, err := pathIDs(r)
	if err != nil {
		return err
	}
	u, err := currentUser(r)
	if err != nil {
		return err
	}
	return h.respondComments(w, r.Context(), u, scenarioID, activityID, http.StatusOK)
}

func (h *Handler) respondComments(w http.ResponseWriter, ctx context.Context, u auth.User, scenarioID, activityID int64, status int) error {
	s, err := findScenario(ctx, h.db, scenarioID, "")
	if err != nil {
		return err
	}
	if _, err := activity.Load(ctx, h.db, u, activityID); err != nil {
		return err
	}
	rows, err := h.db.QueryContext(ctx, `
		SELECT c.id, c.user_id, u.name, c.body, c.deleted_at IS NOT NULL, c.created_at, c.edited_at
		FROM activity_scenario_comments c JOIN users u ON u.id = c.user_id
		WHERE c.scenario_id = ? AND c.activity_id = ?
		ORDER BY c.id`, scenarioID, activityID)
	if err != nil {
		return err
	}
	defer rows.Close()
	list := commentList{Items: []comment{}, CanPost: !s.IsLocked}
	for rows.Next() {
		var c comment
		var edited sql.NullTime
		if err := rows.Scan(&c.ID, &c.UserID, &c.UserName, &c.Body, &c.Deleted, &c.CreatedAt, &edited); err != nil {
			return err
		}
		if c.Deleted {
			c.Body = ""
		} else if edited.Valid {
			c.EditedAt = &edited.Time
		}
		c.CanEdit = !c.Deleted && !s.IsLocked && c.UserID == u.ID
		c.CanDelete = c.CanEdit
		list.Items = append(list.Items, c)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.WriteJSON(w, status, list)
	return nil
}

// postComment は POST /api/scenarios/{id}/activities/{aid}/comments。
func (h *Handler) postComment(w http.ResponseWriter, r *http.Request) error {
	scenarioID, activityID, err := pathIDs(r)
	if err != nil {
		return err
	}
	u, err := currentUser(r)
	if err != nil {
		return err
	}
	var req struct {
		Body string `json:"body"`
	}
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := httpx.Validator{}
	body := v.Text("body", "コメント", req.Body, commentMaxLen)
	if err := v.Err(); err != nil {
		return err
	}

	ctx := r.Context()
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	s, err := findScenario(ctx, tx, scenarioID, " FOR SHARE")
	if err != nil {
		return err
	}
	if s.IsLocked {
		return httpx.Conflict("ロックされたシナリオにはコメントを書けません")
	}
	a, err := activity.Load(ctx, tx, u, activityID)
	if err != nil {
		return err
	}
	// お知らせの宛先: 担当者（いなければユニットのマネージャー）と、すでに書いた人。書いた本人と無効なユーザーは除く
	rows, err := tx.QueryContext(ctx, `
		SELECT DISTINCT r.user_id FROM (
			SELECT COALESCE(a.owner_user_id, un.owner_user_id) AS user_id
			FROM activities a JOIN units un ON un.id = a.unit_id WHERE a.id = ?
			UNION
			SELECT user_id FROM activity_scenario_comments WHERE scenario_id = ? AND activity_id = ?
		) r JOIN users us ON us.id = r.user_id
		WHERE us.is_active AND r.user_id <> ?
		ORDER BY r.user_id`, activityID, scenarioID, activityID, u.ID)
	if err != nil {
		return err
	}
	var recipients []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		recipients = append(recipients, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, "INSERT INTO activity_scenario_comments (scenario_id, activity_id, user_id, body) VALUES (?, ?, ?, ?)",
		scenarioID, activityID, u.ID, body); err != nil {
		return err
	}
	title := fmt.Sprintf("「%s」の説明に%sさんがコメントしました", a.Name, u.Name)
	text := fmt.Sprintf("%s（%s）\n%s", a.Name, s.Name, excerpt(body, 200))
	link := fmt.Sprintf("/activities/%d?tab=update&scenario=%d", activityID, scenarioID)
	for _, id := range recipients {
		if _, err := tx.ExecContext(ctx, "INSERT INTO notifications (user_id, kind, scenario_id, title, body, link) VALUES (?, 'comment', ?, ?, ?, ?)",
			id, scenarioID, title, text, link); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return h.respondComments(w, ctx, u, scenarioID, activityID, http.StatusCreated)
}

// lockOwnComment は、ロックされていないシナリオの、自分のコメントを行ロック付きで読む。消したコメントなら deleted が true。
func lockOwnComment(ctx context.Context, tx *sql.Tx, u auth.User, scenarioID, activityID, commentID int64, action string) (deleted bool, err error) {
	s, err := findScenario(ctx, tx, scenarioID, " FOR SHARE")
	if err != nil {
		return false, err
	}
	if s.IsLocked {
		return false, httpx.Conflict("ロックされたシナリオのコメントは" + action + "できません")
	}
	var authorID int64
	err = tx.QueryRowContext(ctx, "SELECT user_id, deleted_at IS NOT NULL FROM activity_scenario_comments WHERE id = ? AND scenario_id = ? AND activity_id = ? FOR UPDATE",
		commentID, scenarioID, activityID).Scan(&authorID, &deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return false, httpx.NotFound("コメントが見つかりません")
	}
	if err != nil {
		return false, err
	}
	if authorID != u.ID {
		return false, httpx.Forbidden()
	}
	return deleted, nil
}

// commentPath は {id}・{aid}・{cid} を読む。
func commentPath(r *http.Request) (scenarioID, activityID, commentID int64, err error) {
	if scenarioID, activityID, err = pathIDs(r); err != nil {
		return
	}
	commentID, err = httpx.PathID(r, "cid")
	return
}

// putComment は PUT /api/scenarios/{id}/activities/{aid}/comments/{cid}。書いた本人だけが本文を編集できる。
// 編集した日時を残す（編集前の本文は残さない）。編集ではお知らせを送らない。
func (h *Handler) putComment(w http.ResponseWriter, r *http.Request) error {
	scenarioID, activityID, commentID, err := commentPath(r)
	if err != nil {
		return err
	}
	u, err := currentUser(r)
	if err != nil {
		return err
	}
	var req struct {
		Body string `json:"body"`
	}
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := httpx.Validator{}
	body := v.Text("body", "コメント", req.Body, commentMaxLen)
	if err := v.Err(); err != nil {
		return err
	}
	ctx := r.Context()
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	deleted, err := lockOwnComment(ctx, tx, u, scenarioID, activityID, commentID, "編集")
	if err != nil {
		return err
	}
	if deleted {
		return httpx.Conflict("削除したコメントは編集できません")
	}
	// 本文が変わったときだけ、編集した日時を残す
	if _, err := tx.ExecContext(ctx, "UPDATE activity_scenario_comments SET edited_at = IF(body = ?, edited_at, NOW()), body = ? WHERE id = ?", body, body, commentID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return h.respondComments(w, ctx, u, scenarioID, activityID, http.StatusOK)
}

// deleteComment は DELETE /api/scenarios/{id}/activities/{aid}/comments/{cid}。書いた本人だけが消せる。
func (h *Handler) deleteComment(w http.ResponseWriter, r *http.Request) error {
	scenarioID, activityID, commentID, err := commentPath(r)
	if err != nil {
		return err
	}
	u, err := currentUser(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	deleted, err := lockOwnComment(ctx, tx, u, scenarioID, activityID, commentID, "削除")
	if err != nil {
		return err
	}
	if !deleted {
		if _, err := tx.ExecContext(ctx, "UPDATE activity_scenario_comments SET body = '', deleted_at = NOW() WHERE id = ?", commentID); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return h.respondComments(w, ctx, u, scenarioID, activityID, http.StatusOK)
}

// excerpt は先頭 n 文字（超えたら「…」を付ける）。改行は空白にする。
func excerpt(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}
