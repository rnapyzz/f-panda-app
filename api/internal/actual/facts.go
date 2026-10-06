package actual

import (
	"context"
	"database/sql"
	"fmt"
	"math/big"
	"sort"

	"github.com/rnapyzz/f-panda-app/api/internal/audit"
	"github.com/rnapyzz/f-panda-app/api/internal/dbx"
	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
)

// 実績（actual_facts）は、明細（actual_entries）を施策 × 科目 × 月で合計したもの。施策が NULL の行は未割当。
// 明細を変えたら syncFacts で合計を合わせる。合計は差分だけを更新し、行ごとの監査ログを残す。

// factKey は実績の行のキー。activityID の 0 は未割当。
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

	for _, k := range keys {
		before, had := existing[k]
		amount, has := want[k]
		switch {
		case had && !has:
			if _, err := tx.ExecContext(ctx, "DELETE FROM actual_facts WHERE id = ?", before.ID); err != nil {
				return err
			}
			if err := rec.Delete(ctx, "actual_facts", before.ID, before); err != nil {
				return err
			}
			c.Deleted++
		case !had && has:
			after := actualFact{SubjectID: k.subjectID, TargetMonth: k.month, Amount: amount}
			if k.activityID != 0 {
				id := k.activityID
				after.ActivityID = &id
			}
			res, err := tx.ExecContext(ctx,
				"INSERT INTO actual_facts (activity_id, subject_id, target_month, amount) VALUES (?, ?, ?, ?)",
				dbx.NullInt64(after.ActivityID), after.SubjectID, after.TargetMonth+"-01", after.Amount)
			if err != nil {
				return err
			}
			after.ID, _ = res.LastInsertId()
			if err := rec.Insert(ctx, "actual_facts", after.ID, after); err != nil {
				return err
			}
			c.Inserted++
		default:
			if before.Amount == amount {
				c.Unchanged++
				continue
			}
			after := before
			after.Amount = amount
			if _, err := tx.ExecContext(ctx, "UPDATE actual_facts SET amount = ? WHERE id = ?", after.Amount, after.ID); err != nil {
				return err
			}
			if err := rec.Update(ctx, "actual_facts", before.ID, before, after); err != nil {
				return err
			}
			c.Updated++
		}
	}
	return nil
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
