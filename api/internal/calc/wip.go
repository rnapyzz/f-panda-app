package calc

import (
	"context"
	"database/sql"
	"errors"
	"math/big"
	"slices"

	"github.com/rnapyzz/f-panda-app/api/internal/audit"
	"github.com/rnapyzz/f-panda-app/api/internal/versions"
)

// 検収月に一括の施策の仕掛品振替（docs/plan.md「2.28」）。
//
// 原価の科目（subjects.is_project_cost）の金額は発生した月に入力する。仕掛品振替の科目（subjects.role = 'wip_transfer'）の
// 金額をアプリが計算し、source = 'wip'（科目への直接入力の行）で保存する。
//   - 計上月より前の月: −（その月の原価の科目の金額の合計）
//   - 計上月: 前月末の仕掛残高（実績を含む振替の累計の符号を反転したもの）
//   - 計上月より後の月: 0（行を作らない）

// SourceWip は仕掛品振替の金額の source。
const SourceWip = "wip"

// WipSettings は仕掛品振替の科目と原価の科目。
type WipSettings struct {
	// TransferSubjectID は仕掛品振替の科目（0 は未指定）
	TransferSubjectID int64
	CostSubjectIDs    []int64
}

// Queryer は *sql.DB と *sql.Tx に共通する検索のメソッド。
type Queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// LoadWipSettings は科目の設定を読む。
func LoadWipSettings(ctx context.Context, q versions.Querier) (WipSettings, error) {
	rows, err := q.QueryContext(ctx, "SELECT id, is_project_cost, role FROM subjects WHERE is_project_cost OR role = 'wip_transfer' ORDER BY id")
	if err != nil {
		return WipSettings{}, err
	}
	defer rows.Close()
	s := WipSettings{CostSubjectIDs: []int64{}}
	for rows.Next() {
		var id int64
		var cost bool
		var role string
		if err := rows.Scan(&id, &cost, &role); err != nil {
			return WipSettings{}, err
		}
		if role == "wip_transfer" {
			s.TransferSubjectID = id
		} else if cost {
			s.CostSubjectIDs = append(s.CostSubjectIDs, id)
		}
	}
	return s, rows.Err()
}

// RecognitionMonth はシナリオ × 施策の計上月（YYYY-MM、未設定は空）を返す。
func RecognitionMonth(ctx context.Context, q Queryer, scenarioID, activityID int64) (string, error) {
	var m string
	err := q.QueryRowContext(ctx,
		"SELECT DATE_FORMAT(recognition_month, '%Y-%m') FROM activity_recognitions WHERE scenario_id = ? AND activity_id = ?",
		scenarioID, activityID).Scan(&m)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return m, err
}

// RecalculateWipAll は、検収月に一括のすべての施策の振替を、ロックされていないすべてのシナリオで再計算する。
// 科目の設定（原価の科目・仕掛品振替の科目）を変えたときに呼ぶ。
func RecalculateWipAll(ctx context.Context, tx *sql.Tx, rec *audit.Recorder) error {
	// 以前の振替（科目の指定を外した場合を含む）も消せるよう、振替の金額がある施策も対象にする
	rows, err := tx.QueryContext(ctx, `
		SELECT s.id, a.id FROM scenarios s
		JOIN activities a ON a.recognition = 'on_delivery'
		  OR a.id IN (SELECT activity_id FROM budget_facts WHERE scenario_id = s.id AND source = 'wip')
		WHERE NOT s.is_locked ORDER BY s.id, a.id`)
	if err != nil {
		return err
	}
	type pair struct{ scenarioID, activityID int64 }
	var pairs []pair
	for rows.Next() {
		var p pair
		if err := rows.Scan(&p.scenarioID, &p.activityID); err != nil {
			rows.Close()
			return err
		}
		pairs = append(pairs, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, p := range pairs {
		if err := RecalculateWip(ctx, tx, rec, p.scenarioID, p.activityID); err != nil {
			return err
		}
	}
	return nil
}

// RecalculateWip はシナリオ × 施策の仕掛品振替を計算し直し、差分を budget_facts に反映して監査ログに記録する。
// 原価の金額・計上月・決算確定月・計上の仕方・科目の設定を変えたときに呼ぶ（Recalculate からも呼ぶ）。
func RecalculateWip(ctx context.Context, tx *sql.Tx, rec *audit.Recorder, scenarioID, activityID int64) error {
	var fiscalYear int
	var locked bool
	var through sql.NullString
	err := tx.QueryRowContext(ctx, "SELECT fiscal_year, is_locked, DATE_FORMAT(actual_through, '%Y-%m') FROM scenarios WHERE id = ?", scenarioID).
		Scan(&fiscalYear, &locked, &through)
	if err != nil {
		return err
	}
	if locked {
		return nil
	}
	var recognition string
	if err := tx.QueryRowContext(ctx, "SELECT recognition FROM activities WHERE id = ?", activityID).Scan(&recognition); err != nil {
		return err
	}
	months := append(PlanMonths(fiscalYear, through.String), BeyondMonths(fiscalYear)...)

	settings, err := LoadWipSettings(ctx, tx)
	if err != nil {
		return err
	}
	all, err := LoadFacts(ctx, tx, scenarioID, activityID)
	if err != nil {
		return err
	}
	enabled := recognition == "on_delivery" && settings.TransferSubjectID != 0
	existing := map[Key]Fact{}
	for k, f := range all {
		if !slices.Contains(months, k.Month) {
			continue
		}
		// 振替の行と、検収月に一括の施策の振替の科目への直接入力（振替で置き換える）
		if f.Source == SourceWip || (enabled && k.SubjectID == settings.TransferSubjectID && k.LineID == 0) {
			existing[k] = f
		}
	}
	want := map[Key]Fact{}
	if enabled {
		recMonth, err := RecognitionMonth(ctx, tx, scenarioID, activityID)
		if err != nil {
			return err
		}
		if recMonth != "" && recMonth >= months[0] {
			cost := map[string]*big.Int{}
			for k, f := range all {
				if !slices.Contains(settings.CostSubjectIDs, k.SubjectID) {
					continue
				}
				x, ok := new(big.Int).SetString(f.Amount, 10)
				if !ok {
					continue
				}
				if cost[k.Month] == nil {
					cost[k.Month] = new(big.Int)
				}
				cost[k.Month].Add(cost[k.Month], x)
			}
			balance, err := OpeningWip(ctx, tx, scenarioID, activityID, fiscalYear, through.String, months[0], settings.TransferSubjectID)
			if err != nil {
				return err
			}
			for _, m := range months {
				amount := new(big.Int)
				switch {
				case m < recMonth:
					if c := cost[m]; c != nil {
						amount.Neg(c)
						balance.Add(balance, c)
					}
				case m == recMonth:
					amount.Set(balance)
					balance.SetInt64(0)
				}
				if amount.Sign() == 0 {
					continue
				}
				f := Fact{
					ScenarioID: scenarioID, ActivityID: activityID, SubjectID: settings.TransferSubjectID,
					TargetMonth: m, Amount: amount.String(), Source: SourceWip,
				}
				want[KeyOf(f)] = f
			}
		}
	}
	return apply(ctx, tx, rec, existing, want)
}

// OpeningWip は、月 first の前月末の仕掛残高を返す。
// 実績の月は実績の振替（actual_facts。過去の年度を含む）を使う。決算確定月のない計画の版（翌期の計画など）では、
// 今回の見込の決算確定月より後で first より前の月に、今回の見込の振替を使う。
func OpeningWip(ctx context.Context, tx Queryer, scenarioID, activityID int64, fiscalYear int, through, first string, transferSubjectID int64) (*big.Int, error) {
	boundary := through
	var gapScenario int64
	if through == "" {
		actives, err := versions.Load(ctx, tx)
		if err != nil {
			return nil, err
		}
		if c := actives.Current; c != nil && c.ID != scenarioID && c.FiscalYear < fiscalYear && c.ActualThrough != nil {
			boundary, gapScenario = *c.ActualThrough, c.ID
		}
	}
	var actual, gap sql.NullString
	query := "SELECT SUM(amount) FROM actual_facts WHERE activity_id = ? AND subject_id = ? AND target_month < ?"
	args := []any{activityID, transferSubjectID, first + "-01"}
	if boundary != "" {
		query += " AND target_month <= ?"
		args = append(args, boundary+"-01")
	}
	if err := tx.QueryRowContext(ctx, query, args...).Scan(&actual); err != nil {
		return nil, err
	}
	if gapScenario != 0 {
		err := tx.QueryRowContext(ctx, `
			SELECT SUM(amount) FROM budget_facts
			WHERE scenario_id = ? AND activity_id = ? AND subject_id = ? AND target_month > ? AND target_month < ?`,
			gapScenario, activityID, transferSubjectID, boundary+"-01", first+"-01").Scan(&gap)
		if err != nil {
			return nil, err
		}
	}
	sum := new(big.Int)
	for _, s := range []sql.NullString{actual, gap} {
		if !s.Valid {
			continue
		}
		x, ok := new(big.Int).SetString(s.String, 10)
		if !ok {
			return nil, errors.New("invalid wip amount " + s.String)
		}
		sum.Add(sum, x)
	}
	// 仕掛残高は振替の累計の符号を反転したもの
	return sum.Neg(sum), nil
}
