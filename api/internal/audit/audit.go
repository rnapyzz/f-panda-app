// Package audit は変更セット（change_sets）と監査ログ（audit_logs）を記録する。
//
// 更新処理と同じトランザクションの中で Begin を呼び、変更したレコードごとに Insert / Update / Delete を呼ぶ。
package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
)

// Recorder は1つの変更セットに監査ログを記録する。
type Recorder struct {
	tx          *sql.Tx
	changeSetID int64
}

// Begin は変更セットを作成する。reason が空の場合は NULL で保存する。
// scenarioID はシナリオに紐づかない変更（マスタ変更など）では nil。
func Begin(ctx context.Context, tx *sql.Tx, userID int64, scenarioID *int64, reason string) (*Recorder, error) {
	var r sql.NullString
	if s := strings.TrimSpace(reason); s != "" {
		r = sql.NullString{String: s, Valid: true}
	}
	res, err := tx.ExecContext(ctx,
		"INSERT INTO change_sets (user_id, scenario_id, reason) VALUES (?, ?, ?)",
		userID, scenarioID, r,
	)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &Recorder{tx: tx, changeSetID: id}, nil
}

// ChangeSetID は変更セットの ID を返す。
func (r *Recorder) ChangeSetID() int64 { return r.changeSetID }

// Insert はレコードの作成を記録する。
func (r *Recorder) Insert(ctx context.Context, table string, id int64, after any) error {
	return r.record(ctx, table, id, "insert", nil, after)
}

// Update はレコードの更新を記録する。
func (r *Recorder) Update(ctx context.Context, table string, id int64, before, after any) error {
	return r.record(ctx, table, id, "update", before, after)
}

// Delete はレコードの削除を記録する。
func (r *Recorder) Delete(ctx context.Context, table string, id int64, before any) error {
	return r.record(ctx, table, id, "delete", before, nil)
}

// Change は監査ログの1件（Many でまとめて記録する）。Action は insert / update / delete。
type Change struct {
	Table         string
	ID            int64
	Action        string
	Before, After any
}

// manyBatch は Many で1回の INSERT にまとめる件数。
const manyBatch = 500

// Many は監査ログをまとめて記録する（件数の多い取込で、1件ずつ INSERT しないため。I-15）。
func (r *Recorder) Many(ctx context.Context, changes []Change) error {
	for start := 0; start < len(changes); start += manyBatch {
		end := min(start+manyBatch, len(changes))
		var sb strings.Builder
		sb.WriteString("INSERT INTO audit_logs (change_set_id, table_name, record_id, action, before_json, after_json) VALUES ")
		args := make([]any, 0, (end-start)*6)
		for i, c := range changes[start:end] {
			b, err := toJSON(c.Before)
			if err != nil {
				return err
			}
			a, err := toJSON(c.After)
			if err != nil {
				return err
			}
			if i > 0 {
				sb.WriteString(",")
			}
			sb.WriteString("(?, ?, ?, ?, ?, ?)")
			args = append(args, r.changeSetID, c.Table, c.ID, c.Action, b, a)
		}
		if _, err := r.tx.ExecContext(ctx, sb.String(), args...); err != nil {
			return err
		}
	}
	return nil
}

func (r *Recorder) record(ctx context.Context, table string, id int64, action string, before, after any) error {
	b, err := toJSON(before)
	if err != nil {
		return err
	}
	a, err := toJSON(after)
	if err != nil {
		return err
	}
	_, err = r.tx.ExecContext(ctx,
		"INSERT INTO audit_logs (change_set_id, table_name, record_id, action, before_json, after_json) VALUES (?, ?, ?, ?, ?, ?)",
		r.changeSetID, table, id, action, b, a,
	)
	return err
}

func toJSON(v any) (any, error) {
	if v == nil {
		return nil, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

// InTx はトランザクション内で変更セットを作成して fn を実行する。fn がエラーを返したらロールバックする。
func InTx(ctx context.Context, db *sql.DB, userID int64, scenarioID *int64, reason string, fn func(tx *sql.Tx, rec *Recorder) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	rec, err := Begin(ctx, tx, userID, scenarioID, reason)
	if err != nil {
		return err
	}
	if err := fn(tx, rec); err != nil {
		return err
	}
	return tx.Commit()
}
