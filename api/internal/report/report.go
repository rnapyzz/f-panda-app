// Package report は予実比較・集計の API を提供する。
//
// GET /api/reports/comparison は、複数のシナリオ（と実績）の金額を、
// ユニット×科目×月（またはユニット内の施策×科目×月）で集計して返す。
// 階層（セグメント・組織）での集計や利益の計算は画面側で行う。
package report

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/rnapyzz/f-panda-app/api/internal/calc"
	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
)

const maxScenarios = 4

// Handler は予実比較 API のハンドラー。
type Handler struct {
	db *sql.DB
}

// NewHandler は Handler を作る。
func NewHandler(db *sql.DB) *Handler {
	return &Handler{db: db}
}

// Register はルートを登録する。参照はログインユーザー全員。
func (h *Handler) Register(mux *http.ServeMux, requireAuth func(http.Handler) http.Handler) {
	mux.Handle("GET /api/reports/comparison", requireAuth(httpx.Handle(h.comparison)))
	mux.Handle("GET /api/reports/risk", requireAuth(httpx.Handle(h.risk)))
}

// Series は比較する系列（シナリオ、または実績）。
type Series struct {
	Key        string `json:"key"`
	Label      string `json:"label"`
	Kind       string `json:"kind"` // scenario / actual
	ScenarioID *int64 `json:"scenario_id,omitempty"`
	// ActualThrough はシナリオの決算確定月（YYYY-MM）。この月以前は実績
	ActualThrough *string `json:"actual_through,omitempty"`
}

// Row は1つの集計単位（ユニットまたは施策）× 科目 × 月の金額。Values は系列の Key → 金額（円、文字列）。
// UnitID が nil の行は未割当の実績（docs/plan.md「2.12」）。ユニットを指定しないときだけ返す。
type Row struct {
	UnitID     *int64            `json:"unit_id"`
	ActivityID *int64            `json:"activity_id,omitempty"`
	SubjectID  int64             `json:"subject_id"`
	Month      string            `json:"month"`
	Values     map[string]string `json:"values"`
}

type comparisonResponse struct {
	FiscalYear int      `json:"fiscal_year"`
	Measure    string   `json:"measure"`
	Months     []string `json:"months"`
	Series     []Series `json:"series"`
	Rows       []Row    `json:"rows"`
}

type scenarioInfo struct {
	id            int64
	name          string
	actualThrough *string
	fiscalYear    int
}

// comparison は GET /api/reports/comparison。
//
// クエリパラメーター:
//   - scenario_ids: 比較するシナリオ ID（カンマ区切り、最大4つ）。先頭が差異の基準
//   - include_actual=true: 実績データ（取込済みの月）を系列に加える
//   - unit_id: 指定すると、そのユニットの施策ごとに集計する（指定しなければユニットごと）
//
// シナリオの金額は、決算確定月以前の月は実績、それより後の月は計画値。
// すべてのシナリオは同じ年度でなければならない。
func (h *Handler) comparison(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	q := r.URL.Query()

	scenarioIDs, err := parseIDs(q.Get("scenario_ids"))
	if err != nil {
		return httpx.BadRequest("scenario_ids はシナリオ ID をカンマ区切りで指定してください")
	}
	if len(scenarioIDs) == 0 {
		return httpx.Validation(map[string]string{"scenario_ids": "比較するシナリオを選んでください"})
	}
	if len(scenarioIDs) > maxScenarios {
		return httpx.Validation(map[string]string{"scenario_ids": fmt.Sprintf("比較できるシナリオは%dつまでです", maxScenarios)})
	}
	includeActual := q.Get("include_actual") == "true"
	measure := q.Get("measure")
	if measure == "" {
		measure = "full"
	}
	if !measures[measure] {
		return httpx.BadRequest("measure は full / weighted / optimistic / pessimistic のいずれかを指定してください")
	}
	var unitID *int64
	if s := q.Get("unit_id"); s != "" {
		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return httpx.BadRequest("unit_id は数値で指定してください")
		}
		unitID = &id
	}

	// 使うシナリオをまとめて読み込み、年度をそろえる
	infos, err := loadScenarios(ctx, h.db, scenarioIDs)
	if err != nil {
		return err
	}
	fiscalYear := infos[scenarioIDs[0]].fiscalYear
	for _, id := range scenarioIDs {
		if infos[id].fiscalYear != fiscalYear {
			return httpx.Validation(map[string]string{"scenario_ids": "同じ年度のシナリオを選んでください"})
		}
	}
	months := calc.FiscalMonths(fiscalYear)

	resp := comparisonResponse{FiscalYear: fiscalYear, Measure: measure, Months: months, Rows: []Row{}}
	for i, id := range scenarioIDs {
		id := id
		resp.Series = append(resp.Series, Series{Key: fmt.Sprintf("s%d", i+1), Label: infos[id].name, Kind: "scenario", ScenarioID: &id, ActualThrough: infos[id].actualThrough})
	}
	if includeActual {
		resp.Series = append(resp.Series, Series{Key: "actual", Label: "実績", Kind: "actual"})
	}

	amounts, err := loadAmounts(ctx, h.db, scenarioIDs, unitID, measure)
	if err != nil {
		return err
	}

	// 系列ごとに値を組み立てる
	rows := map[rowKey]*Row{}
	put := func(k rowKey, series string, v *big.Int) {
		row, ok := rows[k]
		if !ok {
			row = &Row{SubjectID: k.subjectID, Month: k.month, Values: map[string]string{}}
			if k.unitID != 0 {
				u := k.unitID
				row.UnitID = &u
			}
			if k.activityID != 0 {
				a := k.activityID
				row.ActivityID = &a
			}
			rows[k] = row
		}
		row.Values[series] = v.String()
	}
	for i, id := range scenarioIDs {
		for k, v := range amounts[id] {
			put(k, fmt.Sprintf("s%d", i+1), v)
		}
	}
	if includeActual {
		actuals, err := loadActuals(ctx, h.db, months, unitID)
		if err != nil {
			return err
		}
		for k, v := range actuals {
			put(k, "actual", v)
		}
	}
	// 全社の合計を会計と一致させるため、ユニットを指定しないときは未割当の実績も返す（ユニット ID 0 → null）
	if unitID == nil {
		unallocated, err := loadUnallocated(ctx, h.db, scenarioIDs, months, includeActual)
		if err != nil {
			return err
		}
		for i, id := range scenarioIDs {
			for k, v := range unallocated[id] {
				put(k, fmt.Sprintf("s%d", i+1), v)
			}
		}
		for k, v := range unallocated[0] {
			put(k, "actual", v)
		}
	}

	for _, row := range rows {
		resp.Rows = append(resp.Rows, *row)
	}
	sort.Slice(resp.Rows, func(i, j int) bool {
		a, b := resp.Rows[i], resp.Rows[j]
		if au, bu := derefOr0(a.UnitID), derefOr0(b.UnitID); au != bu {
			return au < bu
		}
		if aid, bid := derefOr0(a.ActivityID), derefOr0(b.ActivityID); aid != bid {
			return aid < bid
		}
		if a.SubjectID != b.SubjectID {
			return a.SubjectID < b.SubjectID
		}
		return a.Month < b.Month
	})
	httpx.WriteJSON(w, http.StatusOK, resp)
	return nil
}

func parseIDs(s string) ([]int64, error) {
	var ids []int64
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		id, err := strconv.ParseInt(p, 10, 64)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("invalid id %q", p)
		}
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func loadScenarios(ctx context.Context, db *sql.DB, ids []int64) (map[int64]scenarioInfo, error) {
	out := map[int64]scenarioInfo{}
	for _, id := range ids {
		if _, ok := out[id]; ok {
			continue
		}
		var s scenarioInfo
		var through sql.NullString
		err := db.QueryRowContext(ctx, "SELECT id, name, DATE_FORMAT(actual_through, '%Y-%m'), fiscal_year FROM scenarios WHERE id = ?", id).
			Scan(&s.id, &s.name, &through, &s.fiscalYear)
		if through.Valid {
			s.actualThrough = &through.String
		}
		if errors.Is(err, sql.ErrNoRows) {
			return nil, httpx.Validation(map[string]string{"scenario_ids": fmt.Sprintf("シナリオ %d が見つかりません", id)})
		}
		if err != nil {
			return nil, err
		}
		out[id] = s
	}
	return out, nil
}

type rowKey struct {
	unitID, activityID, subjectID int64
	month                         string
}

// loadAmounts はシナリオごとに、ユニット（unitID 指定時は施策）×科目×月の合計を返す。
// 金額は scenario_amounts ビュー（決算確定月以前は実績、それより後は計画値）から読む。
// measure は指標（full 満額 / weighted 加重見込 / optimistic 楽観 / pessimistic 悲観）。
func loadAmounts(ctx context.Context, db *sql.DB, scenarioIDs []int64, unitID *int64, measure string) (map[int64]map[rowKey]*big.Int, error) {
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(scenarioIDs)), ",")
	args := make([]any, 0, len(scenarioIDs)+1)
	for _, id := range scenarioIDs {
		args = append(args, id)
	}
	// ユニット単位では施策 ID を 0 とし、グループ化にも含めない
	activityCol, groupActivity, where := "0", "", ""
	if unitID != nil {
		activityCol, groupActivity = "a.id", "a.id, "
		where = " AND a.unit_id = ?"
		args = append(args, *unitID)
	}
	rows, err := db.QueryContext(ctx, `
		SELECT b.scenario_id, a.unit_id, `+activityCol+`, b.subject_id, DATE_FORMAT(b.target_month, '%Y-%m'), `+measureSum(measure)+`
		FROM scenario_amounts b JOIN activities a ON a.id = b.activity_id`+measureJoins+`
		WHERE b.scenario_id IN (`+placeholders+`)`+where+`
		GROUP BY b.scenario_id, a.unit_id, `+groupActivity+`b.subject_id, b.target_month`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]map[rowKey]*big.Int{}
	for rows.Next() {
		var scenarioID int64
		var k rowKey
		var amount string
		if err := rows.Scan(&scenarioID, &k.unitID, &k.activityID, &k.subjectID, &k.month, &amount); err != nil {
			return nil, err
		}
		v, ok := new(big.Int).SetString(amount, 10)
		if !ok {
			return nil, fmt.Errorf("invalid amount %q", amount)
		}
		if out[scenarioID] == nil {
			out[scenarioID] = map[rowKey]*big.Int{}
		}
		out[scenarioID][k] = v
	}
	return out, rows.Err()
}

// loadActuals は実績データの、ユニット（unitID 指定時は施策）×科目×月の合計を返す。
func loadActuals(ctx context.Context, db *sql.DB, months []string, unitID *int64) (map[rowKey]*big.Int, error) {
	args := []any{months[0] + "-01", months[len(months)-1] + "-01"}
	activityCol, groupActivity, where := "0", "", ""
	if unitID != nil {
		activityCol, groupActivity = "a.id", "a.id, "
		where = " AND a.unit_id = ?"
		args = append(args, *unitID)
	}
	rows, err := db.QueryContext(ctx, `
		SELECT a.unit_id, `+activityCol+`, f.subject_id, DATE_FORMAT(f.target_month, '%Y-%m'), CAST(SUM(f.amount) AS CHAR)
		FROM actual_facts f JOIN activities a ON a.id = f.activity_id
		WHERE f.target_month BETWEEN ? AND ?`+where+`
		GROUP BY a.unit_id, `+groupActivity+`f.subject_id, f.target_month`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[rowKey]*big.Int{}
	for rows.Next() {
		var k rowKey
		var amount string
		if err := rows.Scan(&k.unitID, &k.activityID, &k.subjectID, &k.month, &amount); err != nil {
			return nil, err
		}
		v, ok := new(big.Int).SetString(amount, 10)
		if !ok {
			return nil, fmt.Errorf("invalid amount %q", amount)
		}
		out[k] = v
	}
	return out, rows.Err()
}

// loadUnallocated は未割当の実績の、科目 × 月の合計を返す（ユニット ID・施策 ID は 0）。
// シナリオは決算確定月以前の月（scenario_unallocated ビュー）、キー 0 は実績データ（includeActual のとき）。
func loadUnallocated(ctx context.Context, db *sql.DB, scenarioIDs []int64, months []string, includeActual bool) (map[int64]map[rowKey]*big.Int, error) {
	args := make([]any, 0, len(scenarioIDs)+2)
	for _, id := range scenarioIDs {
		args = append(args, id)
	}
	query := `
		SELECT scenario_id, subject_id, DATE_FORMAT(target_month, '%Y-%m'), CAST(SUM(amount) AS CHAR)
		FROM scenario_unallocated WHERE scenario_id IN (` + strings.TrimSuffix(strings.Repeat("?,", len(scenarioIDs)), ",") + `)
		GROUP BY scenario_id, subject_id, target_month`
	if includeActual {
		query += `
		UNION ALL
		SELECT 0, subject_id, DATE_FORMAT(target_month, '%Y-%m'), CAST(SUM(amount) AS CHAR)
		FROM actual_facts WHERE activity_id IS NULL AND target_month BETWEEN ? AND ?
		GROUP BY subject_id, target_month`
		args = append(args, months[0]+"-01", months[len(months)-1]+"-01")
	}
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]map[rowKey]*big.Int{}
	for rows.Next() {
		var scenarioID int64
		var k rowKey
		var amount string
		if err := rows.Scan(&scenarioID, &k.subjectID, &k.month, &amount); err != nil {
			return nil, err
		}
		v, ok := new(big.Int).SetString(amount, 10)
		if !ok {
			return nil, fmt.Errorf("invalid amount %q", amount)
		}
		if out[scenarioID] == nil {
			out[scenarioID] = map[rowKey]*big.Int{}
		}
		out[scenarioID][k] = v
	}
	return out, rows.Err()
}

func derefOr0(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}
