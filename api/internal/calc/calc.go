// Package calc は、内訳（activity_lines）の計算式とドライバー値（driver_values）から
// 金額（budget_facts）を算出して保存する。
//
// 再計算するのは、計算式で反映する（formula_enabled）内訳だけ。算出した金額はその内訳の金額として保存する。
// ロック済みのシナリオと実績シナリオは再計算しない（確定した値を変えないため）。
// 計算に必要なドライバー値がそろわない月は金額を持たない（既存の値は削除する）。
package calc

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"

	"github.com/rnapyzz/f-panda-app/api/internal/audit"
	"github.com/rnapyzz/f-panda-app/api/internal/formula"
	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
)

// Fact は budget_facts の1行。監査ログにもこの形で記録する。
type Fact struct {
	ID                int64  `json:"id"`
	ScenarioID        int64  `json:"scenario_id"`
	ActivityID        int64  `json:"activity_id"`
	SubjectID         int64  `json:"subject_id"`
	LineID            *int64 `json:"line_id"`      // 内訳。nil は科目への直接入力
	TargetMonth       string `json:"target_month"` // YYYY-MM
	Amount            string `json:"amount"`
	Source            string `json:"source"`
	IsProvisional     bool   `json:"is_provisional"`
	ProvisionalReason string `json:"provisional_reason"`
}

// Key はシナリオ×施策の中で金額を特定するキー（科目・内訳・月）。LineID が 0 なら科目への直接入力。
type Key struct {
	SubjectID int64
	LineID    int64
	Month     string
}

// KeyOf は金額のキーを返す。
func KeyOf(f Fact) Key {
	k := Key{SubjectID: f.SubjectID, Month: f.TargetMonth}
	if f.LineID != nil {
		k.LineID = *f.LineID
	}
	return k
}

// sameFact は2つの金額が同じ内容か（ポインターの内訳 ID も値で比べる）。
func sameFact(a, b Fact) bool {
	la, lb := a.LineID, b.LineID
	a.LineID, b.LineID = nil, nil
	return a == b && ((la == nil && lb == nil) || (la != nil && lb != nil && *la == *lb))
}

// FiscalMonths は会計年度（4月開始）の12か月を YYYY-MM で返す。
func FiscalMonths(fiscalYear int) []string {
	months := make([]string, 0, 12)
	for i := 0; i < 12; i++ {
		m := 4 + i
		y := fiscalYear
		if m > 12 {
			m -= 12
			y++
		}
		months = append(months, fmt.Sprintf("%04d-%02d", y, m))
	}
	return months
}

// RecalculateActivity は施策の金額を、再計算の対象となるすべてのシナリオで再計算する。
// 計算式・確度・算出方式を変更したときに呼ぶ。
func RecalculateActivity(ctx context.Context, tx *sql.Tx, rec *audit.Recorder, activityID int64) error {
	rows, err := tx.QueryContext(ctx,
		"SELECT id FROM scenarios WHERE NOT is_locked AND scenario_kind <> 'actual' ORDER BY id FOR SHARE")
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		if err := Recalculate(ctx, tx, rec, id, activityID); err != nil {
			return err
		}
	}
	return nil
}

type driverValue struct {
	value       *big.Rat
	provisional bool
}

// Recalculate はシナリオ×施策の、計算式で反映する内訳の金額を再計算し、差分を budget_facts に反映して監査ログに記録する。
func Recalculate(ctx context.Context, tx *sql.Tx, rec *audit.Recorder, scenarioID, activityID int64) error {
	var fiscalYear int
	var locked bool
	var kind string
	err := tx.QueryRowContext(ctx, "SELECT fiscal_year, is_locked, scenario_kind FROM scenarios WHERE id = ?", scenarioID).
		Scan(&fiscalYear, &locked, &kind)
	if err != nil {
		return err
	}
	if locked || kind == "actual" {
		return nil
	}

	lines, err := loadComputedLines(ctx, tx, activityID)
	if err != nil || len(lines) == 0 {
		return err
	}
	var probability sql.NullString
	if err := tx.QueryRowContext(ctx, "SELECT probability FROM activities WHERE id = ?", activityID).Scan(&probability); err != nil {
		return err
	}
	values, err := loadDriverValues(ctx, tx, scenarioID, activityID)
	if err != nil {
		return err
	}

	want := map[Key]Fact{}
	lineIDs := map[int64]bool{}
	for _, l := range lines {
		lineIDs[l.id] = true
		for _, month := range FiscalMonths(fiscalYear) {
			vars := map[string]*big.Rat{}
			var provisional []string
			complete := true
			for _, id := range l.expr.Idents() {
				if id == "probability" {
					if !probability.Valid {
						complete = false
						break
					}
					p, _ := new(big.Rat).SetString(probability.String)
					vars[id] = p
					continue
				}
				v, ok := values[month][id]
				if !ok {
					complete = false
					break
				}
				vars[id] = v.value
				if v.provisional {
					provisional = append(provisional, id)
				}
			}
			if !complete {
				continue
			}
			result, err := l.expr.Eval(vars)
			if errors.Is(err, formula.ErrDivisionByZero) {
				return httpx.Validation(map[string]string{
					"values": fmt.Sprintf("%s の内訳「%s」の計算で 0 による割り算が発生します", month, l.name),
				})
			}
			if err != nil {
				return err
			}
			lineID := l.id
			f := Fact{
				ScenarioID:  scenarioID,
				ActivityID:  activityID,
				SubjectID:   l.subjectID,
				LineID:      &lineID,
				TargetMonth: month,
				Amount:      formula.RoundHalfUp(result).String(),
				Source:      "formula",
			}
			if len(provisional) > 0 {
				sort.Strings(provisional)
				f.IsProvisional = true
				f.ProvisionalReason = "仮の値のドライバーを含む: " + strings.Join(provisional, ", ")
			}
			want[KeyOf(f)] = f
		}
	}

	all, err := LoadFacts(ctx, tx, scenarioID, activityID)
	if err != nil {
		return err
	}
	existing := map[Key]Fact{}
	for k, f := range all {
		if lineIDs[k.LineID] {
			existing[k] = f
		}
	}
	return apply(ctx, tx, rec, existing, want)
}

// apply は existing を want に合わせて insert / update / delete する。
func apply(ctx context.Context, tx *sql.Tx, rec *audit.Recorder, existing, want map[Key]Fact) error {
	keys := make([]Key, 0, len(existing)+len(want))
	for k := range existing {
		keys = append(keys, k)
	}
	for k := range want {
		if _, ok := existing[k]; !ok {
			keys = append(keys, k)
		}
	}
	// 監査ログの順序を安定させる
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.SubjectID != b.SubjectID {
			return a.SubjectID < b.SubjectID
		}
		if a.LineID != b.LineID {
			return a.LineID < b.LineID
		}
		return a.Month < b.Month
	})

	for _, k := range keys {
		before, had := existing[k]
		after, has := want[k]
		switch {
		case had && !has:
			if _, err := tx.ExecContext(ctx, "DELETE FROM budget_facts WHERE id = ?", before.ID); err != nil {
				return err
			}
			if err := rec.Delete(ctx, "budget_facts", before.ID, before); err != nil {
				return err
			}
		case !had && has:
			id, err := InsertFact(ctx, tx, after)
			if err != nil {
				return err
			}
			after.ID = id
			if err := rec.Insert(ctx, "budget_facts", id, after); err != nil {
				return err
			}
		case had && has:
			after.ID = before.ID
			if sameFact(before, after) {
				continue
			}
			if err := UpdateFact(ctx, tx, after); err != nil {
				return err
			}
			if err := rec.Update(ctx, "budget_facts", before.ID, before, after); err != nil {
				return err
			}
		}
	}
	return nil
}

// InsertFact は budget_facts に1行追加する。
func InsertFact(ctx context.Context, tx *sql.Tx, f Fact) (int64, error) {
	res, err := tx.ExecContext(ctx, `
		INSERT INTO budget_facts (scenario_id, activity_id, subject_id, line_id, target_month, amount, source, is_provisional, provisional_reason)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		f.ScenarioID, f.ActivityID, f.SubjectID, nullInt64(f.LineID), f.TargetMonth+"-01", f.Amount, f.Source, f.IsProvisional, nullString(f.ProvisionalReason),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateFact は budget_facts の1行を更新する（科目・内訳・月は変えない）。
func UpdateFact(ctx context.Context, tx *sql.Tx, f Fact) error {
	_, err := tx.ExecContext(ctx,
		"UPDATE budget_facts SET amount = ?, source = ?, is_provisional = ?, provisional_reason = ? WHERE id = ?",
		f.Amount, f.Source, f.IsProvisional, nullString(f.ProvisionalReason), f.ID,
	)
	return err
}

// LoadFacts はシナリオ×施策の金額を（科目・内訳・月）ごとに行ロック付きで返す。
func LoadFacts(ctx context.Context, tx *sql.Tx, scenarioID, activityID int64) (map[Key]Fact, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, subject_id, line_id, DATE_FORMAT(target_month, '%Y-%m'), amount, source, is_provisional, COALESCE(provisional_reason, '')
		FROM budget_facts WHERE scenario_id = ? AND activity_id = ? FOR UPDATE`,
		scenarioID, activityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[Key]Fact{}
	for rows.Next() {
		f := Fact{ScenarioID: scenarioID, ActivityID: activityID}
		var lineID sql.NullInt64
		if err := rows.Scan(&f.ID, &f.SubjectID, &lineID, &f.TargetMonth, &f.Amount, &f.Source, &f.IsProvisional, &f.ProvisionalReason); err != nil {
			return nil, err
		}
		if lineID.Valid {
			v := lineID.Int64
			f.LineID = &v
		}
		out[KeyOf(f)] = f
	}
	return out, rows.Err()
}

// LineInfo は内訳の要約。
type LineInfo struct {
	SubjectID int64
	Name      string
	// Computed は計算式で反映する内訳か（直接入力できない）
	Computed bool
}

// Lines は施策の内訳を返す。
func Lines(ctx context.Context, tx *sql.Tx, activityID int64) (map[int64]LineInfo, error) {
	rows, err := tx.QueryContext(ctx, "SELECT id, subject_id, name, formula_enabled FROM activity_lines WHERE activity_id = ?", activityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]LineInfo{}
	for rows.Next() {
		var id int64
		var l LineInfo
		if err := rows.Scan(&id, &l.SubjectID, &l.Name, &l.Computed); err != nil {
			return nil, err
		}
		out[id] = l
	}
	return out, rows.Err()
}

type computedLine struct {
	id, subjectID int64
	name          string
	expr          *formula.Expr
}

// loadComputedLines は計算式で反映する内訳を返す。
func loadComputedLines(ctx context.Context, tx *sql.Tx, activityID int64) ([]computedLine, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, subject_id, name, expression FROM activity_lines
		WHERE activity_id = ? AND formula_enabled AND expression IS NOT NULL ORDER BY id`, activityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []computedLine
	for rows.Next() {
		var l computedLine
		var src string
		if err := rows.Scan(&l.id, &l.subjectID, &l.name, &src); err != nil {
			return nil, err
		}
		e, err := formula.Parse(src)
		if err != nil {
			return nil, fmt.Errorf("activity %d line %d: %w", activityID, l.id, err)
		}
		l.expr = e
		out = append(out, l)
	}
	return out, rows.Err()
}

// loadDriverValues は月 → ドライバー code → 値 を返す。
func loadDriverValues(ctx context.Context, tx *sql.Tx, scenarioID, activityID int64) (map[string]map[string]driverValue, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT d.code, DATE_FORMAT(v.target_month, '%Y-%m'), v.value, v.is_provisional
		FROM driver_values v JOIN activity_drivers d ON d.id = v.activity_driver_id
		WHERE v.scenario_id = ? AND d.activity_id = ?`,
		scenarioID, activityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]map[string]driverValue{}
	for rows.Next() {
		var code, month, value string
		var provisional bool
		if err := rows.Scan(&code, &month, &value, &provisional); err != nil {
			return nil, err
		}
		v, ok := new(big.Rat).SetString(value)
		if !ok {
			return nil, fmt.Errorf("invalid driver value %q", value)
		}
		if out[month] == nil {
			out[month] = map[string]driverValue{}
		}
		out[month][code] = driverValue{value: v, provisional: provisional}
	}
	return out, rows.Err()
}

func nullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

func nullInt64(p *int64) sql.NullInt64 {
	if p == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *p, Valid: true}
}
