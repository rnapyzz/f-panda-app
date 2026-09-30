// Package report は予実比較・集計の API を提供する。
//
// GET /api/reports/comparison は、複数のシナリオ（と着地見込）の金額を、
// 機能×科目×月（または機能内の施策×科目×月）で集計して返す。
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

// Series は比較する系列（シナリオ、または着地見込）。
type Series struct {
	Key        string `json:"key"`
	Label      string `json:"label"`
	Kind       string `json:"kind"` // scenario / landing
	ScenarioID *int64 `json:"scenario_id,omitempty"`
	// 着地見込の構成
	ActualScenarioID   *int64 `json:"actual_scenario_id,omitempty"`
	ForecastScenarioID *int64 `json:"forecast_scenario_id,omitempty"`
	ActualThrough      string `json:"actual_through,omitempty"`
}

// Row は1つの集計単位（機能または施策）× 科目 × 月の金額。Values は系列の Key → 金額（円、文字列）。
type Row struct {
	FunctionID int64             `json:"function_id"`
	ActivityID *int64            `json:"activity_id,omitempty"`
	SubjectID  int64             `json:"subject_id"`
	Month      string            `json:"month"`
	Values     map[string]string `json:"values"`
}

type comparisonResponse struct {
	FiscalYear int      `json:"fiscal_year"`
	Months     []string `json:"months"`
	Series     []Series `json:"series"`
	Rows       []Row    `json:"rows"`
}

type scenarioInfo struct {
	id         int64
	name       string
	kind       string
	fiscalYear int
}

// comparison は GET /api/reports/comparison。
//
// クエリパラメーター:
//   - scenario_ids: 比較するシナリオ ID（カンマ区切り、最大4つ）。先頭が差異の基準
//   - landing_actual_id / landing_forecast_id / landing_through（YYYY-MM）:
//     着地見込を系列に加える。landing_through までの月は実績、それ以降の月は見込を使う
//   - function_id: 指定すると、その機能の施策ごとに集計する（指定しなければ機能ごと）
//
// すべての系列は同じ年度のシナリオでなければならない。
func (h *Handler) comparison(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	q := r.URL.Query()

	scenarioIDs, err := parseIDs(q.Get("scenario_ids"))
	if err != nil {
		return httpx.BadRequest("scenario_ids はシナリオ ID をカンマ区切りで指定してください")
	}
	landing, err := parseLanding(q.Get("landing_actual_id"), q.Get("landing_forecast_id"), q.Get("landing_through"))
	if err != nil {
		return err
	}
	if len(scenarioIDs) == 0 && landing == nil {
		return httpx.Validation(map[string]string{"scenario_ids": "比較するシナリオを選んでください"})
	}
	if len(scenarioIDs) > maxScenarios {
		return httpx.Validation(map[string]string{"scenario_ids": fmt.Sprintf("比較できるシナリオは%dつまでです", maxScenarios)})
	}
	var functionID *int64
	if s := q.Get("function_id"); s != "" {
		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return httpx.BadRequest("function_id は数値で指定してください")
		}
		functionID = &id
	}

	// 使うシナリオをまとめて読み込み、年度をそろえる
	needed := slices.Clone(scenarioIDs)
	if landing != nil {
		needed = append(needed, landing.actualID, landing.forecastID)
	}
	infos, err := loadScenarios(ctx, h.db, needed)
	if err != nil {
		return err
	}
	fiscalYear := infos[needed[0]].fiscalYear
	for _, id := range needed {
		if infos[id].fiscalYear != fiscalYear {
			return httpx.Validation(map[string]string{"scenario_ids": "同じ年度のシナリオを選んでください"})
		}
	}
	months := calc.FiscalMonths(fiscalYear)

	resp := comparisonResponse{FiscalYear: fiscalYear, Months: months, Rows: []Row{}}
	for i, id := range scenarioIDs {
		id := id
		resp.Series = append(resp.Series, Series{Key: fmt.Sprintf("s%d", i+1), Label: infos[id].name, Kind: "scenario", ScenarioID: &id})
	}
	if landing != nil {
		if infos[landing.actualID].kind != "actual" {
			return httpx.Validation(map[string]string{"landing_actual_id": "着地見込の実績には、種別が実績のシナリオを選んでください"})
		}
		if infos[landing.forecastID].kind == "actual" {
			return httpx.Validation(map[string]string{"landing_forecast_id": "着地見込の見込には、実績以外のシナリオを選んでください"})
		}
		if !slices.Contains(months, landing.through) {
			return httpx.Validation(map[string]string{"landing_through": fmt.Sprintf("実績を使う最後の月は %s〜%s の範囲で指定してください", months[0], months[len(months)-1])})
		}
		resp.Series = append(resp.Series, Series{
			Key:                "landing",
			Label:              fmt.Sprintf("着地見込（%s まで実績）", landing.through),
			Kind:               "landing",
			ActualScenarioID:   &landing.actualID,
			ForecastScenarioID: &landing.forecastID,
			ActualThrough:      landing.through,
		})
	}

	amounts, err := loadAmounts(ctx, h.db, needed, functionID)
	if err != nil {
		return err
	}

	// 系列ごとに値を組み立てる
	rows := map[rowKey]*Row{}
	put := func(k rowKey, series string, v *big.Int) {
		row, ok := rows[k]
		if !ok {
			row = &Row{FunctionID: k.functionID, SubjectID: k.subjectID, Month: k.month, Values: map[string]string{}}
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
	if landing != nil {
		for k, v := range amounts[landing.actualID] {
			if k.month <= landing.through {
				put(k, "landing", v)
			}
		}
		for k, v := range amounts[landing.forecastID] {
			if k.month > landing.through {
				put(k, "landing", v)
			}
		}
	}

	for _, row := range rows {
		resp.Rows = append(resp.Rows, *row)
	}
	sort.Slice(resp.Rows, func(i, j int) bool {
		a, b := resp.Rows[i], resp.Rows[j]
		if a.FunctionID != b.FunctionID {
			return a.FunctionID < b.FunctionID
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

type landingParams struct {
	actualID, forecastID int64
	through              string
}

func parseLanding(actual, forecast, through string) (*landingParams, error) {
	if actual == "" && forecast == "" && through == "" {
		return nil, nil
	}
	if actual == "" || forecast == "" || through == "" {
		return nil, httpx.Validation(map[string]string{"landing": "着地見込には、実績シナリオ・見込シナリオ・実績を使う最後の月をすべて指定してください"})
	}
	a, err1 := strconv.ParseInt(actual, 10, 64)
	f, err2 := strconv.ParseInt(forecast, 10, 64)
	if err1 != nil || err2 != nil {
		return nil, httpx.BadRequest("着地見込のシナリオ ID は数値で指定してください")
	}
	return &landingParams{actualID: a, forecastID: f, through: through}, nil
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
		err := db.QueryRowContext(ctx, "SELECT id, name, scenario_kind, fiscal_year FROM scenarios WHERE id = ?", id).
			Scan(&s.id, &s.name, &s.kind, &s.fiscalYear)
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
	functionID, activityID, subjectID int64
	month                             string
}

// loadAmounts はシナリオごとに、機能（functionID 指定時は施策）×科目×月の合計を返す。
func loadAmounts(ctx context.Context, db *sql.DB, scenarioIDs []int64, functionID *int64) (map[int64]map[rowKey]*big.Int, error) {
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(scenarioIDs)), ",")
	args := make([]any, 0, len(scenarioIDs)+1)
	for _, id := range scenarioIDs {
		args = append(args, id)
	}
	// 機能単位では施策 ID を 0 とし、グループ化にも含めない
	activityCol, groupActivity, where := "0", "", ""
	if functionID != nil {
		activityCol, groupActivity = "a.id", "a.id, "
		where = " AND a.function_id = ?"
		args = append(args, *functionID)
	}
	rows, err := db.QueryContext(ctx, `
		SELECT b.scenario_id, a.function_id, `+activityCol+`, b.subject_id, DATE_FORMAT(b.target_month, '%Y-%m'), CAST(SUM(b.amount) AS CHAR)
		FROM budget_facts b JOIN activities a ON a.id = b.activity_id
		WHERE b.scenario_id IN (`+placeholders+`)`+where+`
		GROUP BY b.scenario_id, a.function_id, `+groupActivity+`b.subject_id, b.target_month`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]map[rowKey]*big.Int{}
	for rows.Next() {
		var scenarioID int64
		var k rowKey
		var amount string
		if err := rows.Scan(&scenarioID, &k.functionID, &k.activityID, &k.subjectID, &k.month, &amount); err != nil {
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
