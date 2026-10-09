package actual

import (
	"context"
	"database/sql"
	"fmt"
	"math/big"
	"sort"
	"strings"

	"github.com/rnapyzz/f-panda-app/api/internal/audit"
	"github.com/rnapyzz/f-panda-app/api/internal/dbx"
	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
)

// 実績（actual_facts）は、明細（actual_entries）を施策 × 科目 × 月で合計したもの。施策が NULL の行は未割当。
// 明細を変えたら syncFacts で合計を合わせる。合計は差分だけを更新し、行ごとの監査ログを残す。

// factKey は実績の行のキー。activityID の 0 は未割当。
// factBatch は合計（actual_facts）をまとめて書く件数。
const factBatch = 1000

// rowsOf は「(?, ?), (?, ?)」の形の VALUES を作る。
func rowsOf(n, cols int) string {
	row := "(" + placeholders(cols) + ")"
	return strings.TrimSuffix(strings.Repeat(row+",", n), ",")
}

type factKey struct {
	activityID, subjectID int64
	month                 string
}

// actualFact は実績（actual_facts）の1行。監査ログにも使う。
type actualFact struct {
	ID          int64  `json:"id"`
	ActivityID  *int64 `json:"activity_id"`
	SubjectID   int64  `json:"subject_id"`
	TargetMonth string `json:"target_month"`
	Amount      string `json:"amount"`
}

// factCounts は合計の更新件数。
type factCounts struct {
	Facts     int `json:"facts"` // 更新後の件数
	Inserted  int `json:"inserted"`
	Updated   int `json:"updated"`
	Deleted   int `json:"deleted"`
	Unchanged int `json:"unchanged"`
}

// syncFacts は、指定月の実績を明細の合計に合わせる。
func syncFacts(ctx context.Context, tx *sql.Tx, rec *audit.Recorder, months []string, c *factCounts) error {
	if len(months) == 0 {
		return nil
	}
	existing := map[factKey]actualFact{}
	rows, err := tx.QueryContext(ctx, `
		SELECT id, activity_id, subject_id, DATE_FORMAT(target_month, '%Y-%m'), CAST(amount AS CHAR)
		FROM actual_facts WHERE target_month IN (`+placeholders(len(months))+`) FOR UPDATE`, monthArgs(months)...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var f actualFact
		var activity sql.NullInt64
		if err := rows.Scan(&f.ID, &activity, &f.SubjectID, &f.TargetMonth, &f.Amount); err != nil {
			rows.Close()
			return err
		}
		f.ActivityID = dbx.PtrInt64(activity)
		existing[factKey{activity.Int64, f.SubjectID, f.TargetMonth}] = f
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	want := map[factKey]string{}
	rows, err = tx.QueryContext(ctx, `
		SELECT COALESCE(activity_id, 0), subject_id, DATE_FORMAT(target_month, '%Y-%m'), CAST(SUM(amount) AS CHAR)
		FROM actual_entries WHERE target_month IN (`+placeholders(len(months))+`)
		GROUP BY activity_id, subject_id, target_month`, monthArgs(months)...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var k factKey
		var amount string
		if err := rows.Scan(&k.activityID, &k.subjectID, &k.month, &amount); err != nil {
			rows.Close()
			return err
		}
		v, _ := new(big.Int).SetString(amount, 10)
		if v == nil || new(big.Int).Abs(v).Cmp(maxAmount) > 0 {
			rows.Close()
			return httpx.Validation(map[string]string{"file": fmt.Sprintf("%s の合計の金額が大きすぎます", k.month)})
		}
		want[k] = amount
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	c.Facts += len(want)

	keys := make([]factKey, 0, len(existing)+len(want))
	for k := range existing {
		keys = append(keys, k)
	}
	for k := range want {
		if _, ok := existing[k]; !ok {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.month != b.month {
			return a.month < b.month
		}
		if a.activityID != b.activityID {
			return a.activityID < b.activityID
		}
		return a.subjectID < b.subjectID
	})

	// 変更をまとめて書く（件数の多い取込で、1件ずつ SQL を送らないため。I-15）
	var deletes []actualFact
	var inserts []actualFact
	var updates [][2]actualFact
	for _, k := range keys {
		before, had := existing[k]
		amount, has := want[k]
		switch {
		case had && !has:
			deletes = append(deletes, before)
		case !had && has:
			after := actualFact{SubjectID: k.subjectID, TargetMonth: k.month, Amount: amount}
			if k.activityID != 0 {
				id := k.activityID
				after.ActivityID = &id
			}
			inserts = append(inserts, after)
		case before.Amount == amount:
			c.Unchanged++
		default:
			after := before
			after.Amount = amount
			updates = append(updates, [2]actualFact{before, after})
		}
	}
	var changes []audit.Change
	for start := 0; start < len(deletes); start += factBatch {
		batch := deletes[start:min(start+factBatch, len(deletes))]
		args := make([]any, len(batch))
		for i, f := range batch {
			args[i] = f.ID
			changes = append(changes, audit.Change{Table: "actual_facts", ID: f.ID, Action: "delete", Before: f})
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM actual_facts WHERE id IN ("+placeholders(len(batch))+")", args...); err != nil {
			return err
		}
	}
	for start := 0; start < len(updates); start += factBatch {
		batch := updates[start:min(start+factBatch, len(updates))]
		// 主キーが重なる行を入れ、金額だけを更新する（1件ずつの UPDATE の代わり）
		args := make([]any, 0, len(batch)*5)
		for _, u := range batch {
			args = append(args, u[1].ID, dbx.NullInt64(u[1].ActivityID), u[1].SubjectID, u[1].TargetMonth+"-01", u[1].Amount)
			changes = append(changes, audit.Change{Table: "actual_facts", ID: u[1].ID, Action: "update", Before: u[0], After: u[1]})
		}
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO actual_facts (id, activity_id, subject_id, target_month, amount) VALUES "+rowsOf(len(batch), 5)+
				" AS new ON DUPLICATE KEY UPDATE amount = new.amount", args...); err != nil {
			return err
		}
	}
	for start := 0; start < len(inserts); start += factBatch {
		batch := inserts[start:min(start+factBatch, len(inserts))]
		args := make([]any, 0, len(batch)*4)
		for _, f := range batch {
			args = append(args, dbx.NullInt64(f.ActivityID), f.SubjectID, f.TargetMonth+"-01", f.Amount)
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO actual_facts (activity_id, subject_id, target_month, amount) VALUES "+rowsOf(len(batch), 4), args...); err != nil {
			return err
		}
	}
	if len(inserts) > 0 {
		// 追加した行の ID は、キー（施策・科目・月）で読み直す（監査ログに残すため）
		ids := map[factKey]int64{}
		rows, err := tx.QueryContext(ctx, `
			SELECT id, activity_key, subject_id, DATE_FORMAT(target_month, '%Y-%m')
			FROM actual_facts WHERE target_month IN (`+placeholders(len(months))+`)`, monthArgs(months)...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id int64
			var k factKey
			if err := rows.Scan(&id, &k.activityID, &k.subjectID, &k.month); err != nil {
				rows.Close()
				return err
			}
			ids[k] = id
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, f := range inserts {
			k := factKey{0, f.SubjectID, f.TargetMonth}
			if f.ActivityID != nil {
				k.activityID = *f.ActivityID
			}
			f.ID = ids[k]
			changes = append(changes, audit.Change{Table: "actual_facts", ID: f.ID, Action: "insert", After: f})
		}
	}
	c.Deleted += len(deletes)
	c.Inserted += len(inserts)
	c.Updated += len(updates)
	return rec.Many(ctx, changes)
}

// monthTotals は月ごとの実績の合計（会計システムとの突合用）。未割当を含む。
type monthTotals struct {
	Month              string `json:"month"`
	Revenue            string `json:"revenue"`
	Expense            string `json:"expense"`
	UnallocatedRevenue string `json:"unallocated_revenue"`
	UnallocatedExpense string `json:"unallocated_expense"`
}

func monthlyTotals(ctx context.Context, tx *sql.Tx, months []string) ([]monthTotals, error) {
	out := []monthTotals{}
	for _, m := range months {
		t := monthTotals{Month: m}
		err := tx.QueryRowContext(ctx, `
			SELECT CAST(COALESCE(SUM(CASE WHEN s.category = 'revenue' THEN f.amount END), 0) AS CHAR),
			       CAST(COALESCE(SUM(CASE WHEN s.category = 'expense' THEN f.amount END), 0) AS CHAR),
			       CAST(COALESCE(SUM(CASE WHEN s.category = 'revenue' AND f.activity_id IS NULL THEN f.amount END), 0) AS CHAR),
			       CAST(COALESCE(SUM(CASE WHEN s.category = 'expense' AND f.activity_id IS NULL THEN f.amount END), 0) AS CHAR)
			FROM actual_facts f JOIN subjects s ON s.id = f.subject_id
			WHERE f.target_month = ?`, m+"-01",
		).Scan(&t.Revenue, &t.Expense, &t.UnallocatedRevenue, &t.UnallocatedExpense)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// entryMonths は明細がある月を返す（指定した月のうち）。
func entryMonths(ctx context.Context, tx *sql.Tx, months []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(months) == 0 {
		return out, nil
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT DISTINCT DATE_FORMAT(target_month, '%Y-%m') FROM actual_entries
		WHERE target_month IN (`+placeholders(len(months))+`)`, monthArgs(months)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			return nil, err
		}
		out[m] = true
	}
	return out, rows.Err()
}
