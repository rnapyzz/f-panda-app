package report

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rnapyzz/f-panda-app/api/internal/auth"
	"github.com/rnapyzz/f-panda-app/api/internal/calc"
	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
	"github.com/rnapyzz/f-panda-app/api/internal/target"
	"github.com/rnapyzz/f-panda-app/api/internal/visibility"
	"github.com/rnapyzz/f-panda-app/api/internal/xlsx"
)

var packSheets = []string{"pl", "units", "notes"}

// pack は GET /api/reports/pack.xlsx（報告資料、docs/plan.md「2.23」）。
//
// クエリパラメーター:
//   - scenario_ids・include_actual・measure: 予実比較と同じ
//   - grain: month / quarter / half / year（全社 P/L の列の期間）
//   - segment_id / organization_id / unit_id: 範囲（どれか1つ。指定しなければ全社で、未割当の実績を含む）
//   - sheets: 出すシート（pl,units,notes のカンマ区切り。既定はすべて）
//   - activities=true: ユニット別 P/L に施策を含める
func (h *Handler) pack(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	q := r.URL.Query()
	u, ok := auth.UserFrom(ctx)
	if !ok {
		return httpx.Unauthorized("ログインしてください")
	}

	scenarioIDs, err := parseIDs(q.Get("scenario_ids"))
	if err != nil {
		return httpx.BadRequest("scenario_ids はシナリオ ID をカンマ区切りで指定してください")
	}
	if len(scenarioIDs) == 0 {
		return httpx.Validation(map[string]string{"scenario_ids": "シナリオを選んでください"})
	}
	if len(scenarioIDs) > maxScenarios {
		return httpx.Validation(map[string]string{"scenario_ids": fmt.Sprintf("選べるシナリオは%dつまでです", maxScenarios)})
	}
	in := &packInput{measure: q.Get("measure"), grain: q.Get("grain"), withActivities: q.Get("activities") == "true", userName: u.Name, now: time.Now().In(jst())}
	if in.measure == "" {
		in.measure = "full"
	}
	if !measures[in.measure] {
		return httpx.BadRequest("measure は full / weighted / optimistic / pessimistic のいずれかを指定してください")
	}
	if in.grain == "" {
		in.grain = "month"
	}
	if grainLabels[in.grain] == "" {
		return httpx.BadRequest("grain は month / quarter / half / year のいずれかを指定してください")
	}
	if s := q.Get("sheets"); s == "" {
		in.sheets = packSheets
	} else {
		for _, p := range strings.Split(s, ",") {
			p = strings.TrimSpace(p)
			if !contains(packSheets, p) {
				return httpx.BadRequest("sheets は pl / units / notes をカンマ区切りで指定してください")
			}
			if !contains(in.sheets, p) {
				in.sheets = append(in.sheets, p)
			}
		}
	}
	scope, err := parseScope(q)
	if err != nil {
		return err
	}

	infos, err := loadScenarios(ctx, h.db, scenarioIDs)
	if err != nil {
		return err
	}
	in.fiscalYear = infos[scenarioIDs[0]].fiscalYear
	for _, id := range scenarioIDs {
		if infos[id].fiscalYear != in.fiscalYear {
			return httpx.Validation(map[string]string{"scenario_ids": "同じ年度のシナリオを選んでください"})
		}
		in.series = append(in.series, packSeries{label: infos[id].name, scenarioID: id, actualThrough: infos[id].actualThrough})
	}
	includeActual := q.Get("include_actual") == "true"
	if includeActual {
		in.series = append(in.series, packSeries{label: "実績"})
	}
	in.months = calc.FiscalMonths(in.fiscalYear)
	if in.restrictedHidden, err = visibility.Hidden(ctx, h.db, u); err != nil {
		return err
	}

	// マスタと範囲
	if err := loadPackMasters(ctx, h.db, u, in); err != nil {
		return err
	}
	inScope, err := applyScope(ctx, h.db, in, scope)
	if err != nil {
		return err
	}

	// 金額（予実比較と同じ読み込み）。範囲のユニットに絞る
	byActivity := in.withActivities && contains(in.sheets, "units")
	var unitID *int64
	if scope.kind == "unit" {
		unitID = &scope.id
	}
	amounts, err := loadAmounts(ctx, h.db, u, scenarioIDs, unitID, byActivity, in.measure)
	if err != nil {
		return err
	}
	in.values = make([]map[rowKey]*big.Int, len(in.series))
	keep := func(dst map[rowKey]*big.Int, src map[rowKey]*big.Int) {
		for k, v := range src {
			if inScope[k.unitID] {
				dst[k] = v
			}
		}
	}
	for i, id := range scenarioIDs {
		in.values[i] = map[rowKey]*big.Int{}
		keep(in.values[i], amounts[id])
	}
	if includeActual {
		actuals, err := loadActuals(ctx, h.db, u, in.months, unitID, byActivity)
		if err != nil {
			return err
		}
		in.values[len(in.series)-1] = map[rowKey]*big.Int{}
		keep(in.values[len(in.series)-1], actuals)
	}
	if in.allUnits {
		unallocated, err := loadUnallocated(ctx, h.db, u, scenarioIDs, in.months, includeActual)
		if err != nil {
			return err
		}
		for i, id := range scenarioIDs {
			keep(in.values[i], unallocated[id])
		}
		if includeActual {
			keep(in.values[len(in.series)-1], unallocated[0])
		}
	}
	if byActivity {
		if in.activities, err = loadPackActivities(ctx, h.db); err != nil {
			return err
		}
	}
	if contains(in.sheets, "notes") {
		if in.notes, err = loadPackNotes(ctx, h.db, u, in.fiscalYear, inScope); err != nil {
			return err
		}
	}

	var buf bytes.Buffer
	if err := xlsx.Write(&buf, buildPack(in)); err != nil {
		return err
	}
	date := in.now.Format("20060102")
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="report_%d_%s.xlsx"; filename*=UTF-8''%s`,
		in.fiscalYear, date, url.PathEscape(fmt.Sprintf("報告資料_%d_%s.xlsx", in.fiscalYear, date))))
	w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
	_, err = w.Write(buf.Bytes())
	return err
}

func jst() *time.Location {
	loc, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		return time.FixedZone("Asia/Tokyo", 9*60*60)
	}
	return loc
}

// packScope は範囲。kind が空なら全社。
type packScope struct {
	kind string // segment / organization / unit
	id   int64
}

func parseScope(q url.Values) (packScope, error) {
	var scope packScope
	for _, kind := range []string{"segment", "organization", "unit"} {
		s := q.Get(kind + "_id")
		if s == "" {
			continue
		}
		if scope.kind != "" {
			return scope, httpx.BadRequest("範囲は segment_id / organization_id / unit_id のどれか1つで指定してください")
		}
		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil || id <= 0 {
			return scope, httpx.BadRequest(kind + "_id は数値で指定してください")
		}
		scope = packScope{kind, id}
	}
	return scope, nil
}

// loadPackMasters は科目体系（見られる科目だけ）・ユニット・セグメントを読む。
func loadPackMasters(ctx context.Context, db *sql.DB, u auth.User, in *packInput) error {
	rows, err := db.QueryContext(ctx, "SELECT id, parent_id, name, category FROM subjects WHERE TRUE"+visibility.SubjectFilter(u, "id")+" ORDER BY sort_order, code")
	if err != nil {
		return err
	}
	type subjectRow struct {
		node   *subjectNode
		parent sql.NullInt64
	}
	var list []subjectRow
	byID := map[int64]*subjectNode{}
	for rows.Next() {
		var s subjectRow
		s.node = &subjectNode{}
		if err := rows.Scan(&s.node.id, &s.parent, &s.node.name, &s.node.category); err != nil {
			rows.Close()
			return err
		}
		list = append(list, s)
		byID[s.node.id] = s.node
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, s := range list {
		// 親が見られない科目は、ルートとして出す
		if p, ok := byID[s.parent.Int64]; s.parent.Valid && ok {
			p.children = append(p.children, s.node)
		} else {
			in.subjects = append(in.subjects, s.node)
		}
	}

	if in.segments, err = loadTree(ctx, db, "segments"); err != nil {
		return err
	}
	rows, err = db.QueryContext(ctx, "SELECT id, code, name, segment_id, is_archived FROM units ORDER BY code")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var un packUnit
		if err := rows.Scan(&un.id, &un.code, &un.name, &un.segmentID, &un.archived); err != nil {
			return err
		}
		in.units = append(in.units, un)
	}
	return rows.Err()
}

// loadTree はセグメントか組織の階層を読む（並び順）。
func loadTree(ctx context.Context, db *sql.DB, table string) ([]packNode, error) {
	rows, err := db.QueryContext(ctx, "SELECT id, COALESCE(parent_id, 0), name FROM "+table+" ORDER BY sort_order, id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []packNode
	for rows.Next() {
		var n packNode
		if err := rows.Scan(&n.id, &n.parentID, &n.name); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// applyScope は範囲のユニットに絞り（in.units）、範囲の名前を付ける。返り値は範囲のユニット ID（全社なら未割当の 0 を含む）。
func applyScope(ctx context.Context, db *sql.DB, in *packInput, scope packScope) (map[int64]bool, error) {
	inScope := map[int64]bool{}
	if scope.kind == "" {
		in.allUnits, in.scopeLabel = true, "全社"
		inScope[0] = true
		for _, un := range in.units {
			inScope[un.id] = true
		}
		return inScope, nil
	}
	var name string
	var match func(packUnit) bool
	switch scope.kind {
	case "unit":
		match = func(un packUnit) bool { return un.id == scope.id }
		for _, un := range in.units {
			if un.id == scope.id {
				name = un.name
			}
		}
		in.scopeLabel = "ユニット: " + name
	default:
		nodes := in.segments
		label, column := "セグメント", "segment_id"
		if scope.kind == "organization" {
			var err error
			if nodes, err = loadTree(ctx, db, "organizations"); err != nil {
				return nil, err
			}
			label, column = "組織", "organization_id"
		}
		under := map[int64]bool{scope.id: true}
		for changed := true; changed; {
			changed = false
			for _, n := range nodes {
				if under[n.parentID] && !under[n.id] {
					under[n.id], changed = true, true
				}
			}
		}
		for _, n := range nodes {
			if n.id == scope.id {
				name = n.name
			}
		}
		in.scopeLabel = label + ": " + name
		if scope.kind == "segment" {
			match = func(un packUnit) bool { return under[un.segmentID] }
		} else {
			// ユニットの組織は units から読む（packUnit は持たない）
			ids, err := unitsWhere(ctx, db, column, under)
			if err != nil {
				return nil, err
			}
			match = func(un packUnit) bool { return ids[un.id] }
		}
	}
	if name == "" {
		return nil, httpx.Validation(map[string]string{scope.kind + "_id": "範囲が見つかりません"})
	}
	var units []packUnit
	for _, un := range in.units {
		if match(un) {
			units = append(units, un)
			inScope[un.id] = true
		}
	}
	in.units = units
	return inScope, nil
}

func unitsWhere(ctx context.Context, db *sql.DB, column string, nodeIDs map[int64]bool) (map[int64]bool, error) {
	rows, err := db.QueryContext(ctx, "SELECT id, "+column+" FROM units")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id, node int64
		if err := rows.Scan(&id, &node); err != nil {
			return nil, err
		}
		if nodeIDs[node] {
			out[id] = true
		}
	}
	return out, rows.Err()
}

func loadPackActivities(ctx context.Context, db *sql.DB) ([]packActivity, error) {
	rows, err := db.QueryContext(ctx, "SELECT id, unit_id, code, name FROM activities ORDER BY code")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []packActivity
	for rows.Next() {
		var a packActivity
		if err := rows.Scan(&a.id, &a.unitID, &a.code, &a.name); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// loadPackNotes は変動の説明のシートのデータを読む。今回の見込は年度の作成中のシナリオ、
// 目標は修正計画（なければ期初計画、なければ最初に作ったシナリオ。ホームと同じ）、前回の見込はシナリオの前回見込。
// 対象の施策はホームと同じ（更新の対象）で、範囲のユニットに絞る。
func loadPackNotes(ctx context.Context, db *sql.DB, u auth.User, fiscalYear int, inScope map[int64]bool) (*packNotes, error) {
	notes := &packNotes{}
	var currentID int64
	var previousID sql.NullInt64
	err := db.QueryRowContext(ctx, "SELECT id, name, previous_scenario_id FROM scenarios WHERE is_active AND fiscal_year = ?", fiscalYear).
		Scan(&currentID, &notes.scenario, &previousID)
	if errors.Is(err, sql.ErrNoRows) {
		return notes, nil
	}
	if err != nil {
		return nil, err
	}
	var baseID int64
	err = db.QueryRowContext(ctx, `SELECT id, name FROM scenarios WHERE fiscal_year = ?
		ORDER BY CASE plan_role WHEN 'revised' THEN 0 WHEN 'initial' THEN 1 ELSE 2 END, id LIMIT 1`, fiscalYear).Scan(&baseID, &notes.base)
	if err != nil {
		return nil, err
	}
	ids := []int64{currentID, baseID}
	if previousID.Valid {
		if err := db.QueryRowContext(ctx, "SELECT name FROM scenarios WHERE id = ?", previousID.Int64).Scan(&notes.previous); err != nil {
			return nil, err
		}
		ids = append(ids, previousID.Int64)
	}
	profits, err := annualProfits(ctx, db, u, ids)
	if err != nil {
		return nil, err
	}
	profitOf := func(scenarioID, activityID int64) *big.Int {
		if v := profits[scenarioID][activityID]; v != nil {
			return v
		}
		return new(big.Int)
	}

	rows, err := db.QueryContext(ctx, `
		SELECT a.id, a.unit_id, a.code, a.name, un.name, COALESCE(ow.name, ''),
		       COALESCE(n.explanation, ''), COALESCE(n.causes, ''), n.last_edited_at IS NOT NULL, n.completed_at IS NOT NULL
		FROM activities a
		JOIN units un ON un.id = a.unit_id
		LEFT JOIN users ow ON ow.id = a.owner_user_id
		LEFT JOIN activity_scenario_notes n ON n.activity_id = a.id AND n.scenario_id = ?
		WHERE `+target.Condition("a"), currentID, currentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var r noteRow
		var id, unitID int64
		var causes string
		var edited, completed bool
		if err := rows.Scan(&id, &unitID, &r.code, &r.name, &r.unit, &r.owner, &r.explanation, &causes, &edited, &completed); err != nil {
			return nil, err
		}
		if !inScope[unitID] {
			continue
		}
		switch {
		case completed:
			r.status = "completed"
		case edited:
			r.status = "in_progress"
		default:
			r.status = "not_started"
		}
		if causes != "" {
			r.causes = strings.Split(causes, ",")
		}
		r.current = profitOf(currentID, id)
		r.baseDiff = new(big.Int).Sub(r.current, profitOf(baseID, id))
		if previousID.Valid {
			r.previousDiff = new(big.Int).Sub(r.current, profitOf(previousID.Int64, id))
		}
		notes.rows = append(notes.rows, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sortNotes(notes.rows)
	return notes, nil
}

// annualProfits はシナリオごとの、施策の年間の利益（満額、決算確定月以前は実績）を返す。見られない科目は除く。
func annualProfits(ctx context.Context, db *sql.DB, u auth.User, scenarioIDs []int64) (map[int64]map[int64]*big.Int, error) {
	args := make([]any, len(scenarioIDs))
	for i, id := range scenarioIDs {
		args[i] = id
	}
	rows, err := db.QueryContext(ctx, `
		SELECT b.scenario_id, b.activity_id, CAST(SUM(CASE WHEN s.category = 'revenue' THEN b.amount ELSE -b.amount END) AS CHAR)
		FROM scenario_amounts b JOIN subjects s ON s.id = b.subject_id
		WHERE b.scenario_id IN (`+strings.TrimSuffix(strings.Repeat("?,", len(scenarioIDs)), ",")+`)`+visibility.SubjectFilter(u, "b.subject_id")+`
		GROUP BY b.scenario_id, b.activity_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]map[int64]*big.Int{}
	for rows.Next() {
		var sid, aid int64
		var amount string
		if err := rows.Scan(&sid, &aid, &amount); err != nil {
			return nil, err
		}
		v, ok := new(big.Int).SetString(amount, 10)
		if !ok {
			return nil, fmt.Errorf("invalid amount %q", amount)
		}
		if out[sid] == nil {
			out[sid] = map[int64]*big.Int{}
		}
		out[sid][aid] = v
	}
	return out, rows.Err()
}
