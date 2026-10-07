package scenario

// 計画値の CSV（金額の直接入力・ドライバー値）の出力と取込（docs/plan.md「6.3」）。

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"slices"
	"strconv"

	"github.com/rnapyzz/f-panda-app/api/internal/activity"
	"github.com/rnapyzz/f-panda-app/api/internal/audit"
	"github.com/rnapyzz/f-panda-app/api/internal/auth"
	"github.com/rnapyzz/f-panda-app/api/internal/calc"
	"github.com/rnapyzz/f-panda-app/api/internal/csvio"
	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
)

const (
	maxPlanCSVBytes = 5 << 20 // 5MB
	maxPlanCSVRows  = 5_000
	// deleteMark は「値を消す」セルの書き方（空欄は「変えない」）
	deleteMark = "-"
)

var (
	amountKeyColumns = []string{"activity_code", "subject_code", "line_name"}
	amountRefColumns = []string{"activity_name", "subject_name", "line_type"}
	driverKeyColumns = []string{"activity_code", "driver_code"}
	driverRefColumns = []string{"activity_name", "driver_name", "unit"}
)

// --- 出力 ---

// exportFilter はシナリオと、ユニットでの絞り込み（unit_id）の条件を読む。
func (h *Handler) exportFilter(r *http.Request) (Scenario, string, []any, error) {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return Scenario{}, "", nil, err
	}
	s, err := findScenario(r.Context(), h.db, id, "")
	if err != nil {
		return Scenario{}, "", nil, err
	}
	if v := r.URL.Query().Get("unit_id"); v != "" {
		unitID, err := strconv.ParseInt(v, 10, 64)
		if err != nil || unitID <= 0 {
			return Scenario{}, "", nil, httpx.BadRequest("unit_id が正しくありません")
		}
		return s, " AND a.unit_id = ?", []any{unitID}, nil
	}
	return s, "", nil, nil
}

type exportActivity struct {
	id         int64
	code, name string
}

func exportActivities(ctx context.Context, q queryer, cond string, args []any) ([]exportActivity, error) {
	rows, err := q.QueryContext(ctx, "SELECT a.id, a.code, a.name FROM activities a WHERE 1 = 1"+cond+" ORDER BY a.code", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []exportActivity
	for rows.Next() {
		var a exportActivity
		if err := rows.Scan(&a.id, &a.code, &a.name); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// exportAmounts は GET /api/scenarios/{id}/amounts/export。
// 1行が施策 × 科目 × 内訳（内訳名が空は科目への直接入力）で、月を横に並べる。実績の月は実績を出す。
func (h *Handler) exportAmounts(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	s, cond, args, err := h.exportFilter(r)
	if err != nil {
		return err
	}
	months := calc.FiscalMonths(s.FiscalYear)
	acts, err := exportActivities(ctx, h.db, cond, args)
	if err != nil {
		return err
	}

	type subject struct{ id, code, name string }
	var subjects []subject
	srows, err := h.db.QueryContext(ctx, "SELECT id, code, name FROM subjects ORDER BY category, sort_order, code")
	if err != nil {
		return err
	}
	for srows.Next() {
		var sub subject
		if err := srows.Scan(&sub.id, &sub.code, &sub.name); err != nil {
			srows.Close()
			return err
		}
		subjects = append(subjects, sub)
	}
	srows.Close()
	if err := srows.Err(); err != nil {
		return err
	}

	type line struct {
		id      int64
		name    string
		formula bool
	}
	// 施策 → 科目 → 内訳（表示順）
	lines := map[int64]map[string][]line{}
	lineIDs := map[int64]bool{}
	lrows, err := h.db.QueryContext(ctx, `
		SELECT l.id, l.activity_id, l.subject_id, l.name, l.formula_enabled
		FROM activity_lines l JOIN activities a ON a.id = l.activity_id
		WHERE 1 = 1`+cond+` ORDER BY l.sort_order, l.id`, args...)
	if err != nil {
		return err
	}
	for lrows.Next() {
		var l line
		var activityID int64
		var subjectID string
		if err := lrows.Scan(&l.id, &activityID, &subjectID, &l.name, &l.formula); err != nil {
			lrows.Close()
			return err
		}
		if lines[activityID] == nil {
			lines[activityID] = map[string][]line{}
		}
		lines[activityID][subjectID] = append(lines[activityID][subjectID], l)
		lineIDs[l.id] = true
	}
	lrows.Close()
	if err := lrows.Err(); err != nil {
		return err
	}

	// 金額: (施策, 科目, 内訳（0 は直接入力）) → 月 → 金額
	type key struct {
		activityID int64
		subjectID  string
		lineID     int64
	}
	values := map[key]map[string]*big.Rat{}
	vrows, err := h.db.QueryContext(ctx, `
		SELECT v.activity_id, v.subject_id, v.line_id, DATE_FORMAT(v.target_month, '%Y-%m'), v.amount
		FROM scenario_amounts v JOIN activities a ON a.id = v.activity_id
		WHERE v.scenario_id = ?`+cond, append([]any{s.ID}, args...)...)
	if err != nil {
		return err
	}
	for vrows.Next() {
		var k key
		var lineID sql.NullInt64
		var month, amount string
		if err := vrows.Scan(&k.activityID, &k.subjectID, &lineID, &month, &amount); err != nil {
			vrows.Close()
			return err
		}
		if lineID.Valid && lineIDs[lineID.Int64] {
			k.lineID = lineID.Int64
		}
		if values[k] == nil {
			values[k] = map[string]*big.Rat{}
		}
		x, ok := new(big.Rat).SetString(amount)
		if !ok {
			return errors.New("金額を読めません: " + amount)
		}
		if cur := values[k][month]; cur != nil {
			x.Add(x, cur)
		}
		values[k][month] = x
	}
	vrows.Close()
	if err := vrows.Err(); err != nil {
		return err
	}

	cells := func(m map[string]*big.Rat) []string {
		out := make([]string, len(months))
		for i, month := range months {
			if v := m[month]; v != nil {
				out[i] = v.RatString()
			}
		}
		return out
	}
	header := append(append([]string{}, "activity_code", "activity_name", "subject_code", "subject_name", "line_name", "line_type"), months...)
	var out [][]string
	for _, a := range acts {
		for _, sub := range subjects {
			if direct := values[key{a.id, sub.id, 0}]; len(direct) > 0 {
				out = append(out, append([]string{a.code, a.name, sub.code, sub.name, "", "manual"}, cells(direct)...))
			}
			for _, l := range lines[a.id][sub.id] {
				typ := "manual"
				if l.formula {
					typ = "formula"
				}
				out = append(out, append([]string{a.code, a.name, sub.code, sub.name, l.name, typ}, cells(values[key{a.id, sub.id, l.id}])...))
			}
		}
	}
	return csvio.WriteCSV(w, "amounts", header, out)
}

// exportDriverValues は GET /api/scenarios/{id}/driver-values/export。1行が施策 × ドライバーで、月を横に並べる。
func (h *Handler) exportDriverValues(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	s, cond, args, err := h.exportFilter(r)
	if err != nil {
		return err
	}
	months := calc.FiscalMonths(s.FiscalYear)

	values := map[int64]map[string]string{}
	vrows, err := h.db.QueryContext(ctx, `
		SELECT v.activity_driver_id, DATE_FORMAT(v.target_month, '%Y-%m'), v.value
		FROM driver_values v
		JOIN activity_drivers d ON d.id = v.activity_driver_id
		JOIN activities a ON a.id = d.activity_id
		WHERE v.scenario_id = ?`+cond, append([]any{s.ID}, args...)...)
	if err != nil {
		return err
	}
	for vrows.Next() {
		var driverID int64
		var month, value string
		if err := vrows.Scan(&driverID, &month, &value); err != nil {
			vrows.Close()
			return err
		}
		if values[driverID] == nil {
			values[driverID] = map[string]string{}
		}
		values[driverID][month] = string(trimDecimal(value))
	}
	vrows.Close()
	if err := vrows.Err(); err != nil {
		return err
	}

	rows, err := h.db.QueryContext(ctx, `
		SELECT d.id, a.code, a.name, d.code, d.name, COALESCE(d.unit, '')
		FROM activity_drivers d JOIN activities a ON a.id = d.activity_id
		WHERE 1 = 1`+cond+` ORDER BY a.code, d.sort_order, d.id`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	header := append(append([]string{}, "activity_code", "activity_name", "driver_code", "driver_name", "unit"), months...)
	var out [][]string
	for rows.Next() {
		var driverID int64
		rec := make([]string, 5, 5+len(months))
		if err := rows.Scan(&driverID, &rec[0], &rec[1], &rec[2], &rec[3], &rec[4]); err != nil {
			return err
		}
		for _, m := range months {
			rec = append(rec, values[driverID][m])
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return csvio.WriteCSV(w, "driver-values", header, out)
}

// --- 取込 ---

type planImportActivity struct {
	ActivityID int64  `json:"activity_id"`
	Code       string `json:"code"`
	Name       string `json:"name"`
	Changed    int    `json:"changed"`
}

type planImportWarning struct {
	// Kind は actual_month（実績の月の値）か formula_line（計算式で反映する内訳の値）
	Kind  string `json:"kind"`
	Count int    `json:"count"`
}

// planImportResult は取込の結果。件数はセルの数。
type planImportResult struct {
	DryRun     bool                 `json:"dry_run"`
	Kind       string               `json:"kind"` // amounts / driver_values
	Rows       int                  `json:"rows"`
	Inserted   int                  `json:"inserted"`
	Updated    int                  `json:"updated"`
	Deleted    int                  `json:"deleted"`
	Unchanged  int                  `json:"unchanged"`
	Activities []planImportActivity `json:"activities"`
	Warnings   []planImportWarning  `json:"warnings"`
}

// cellOp は取り込む1セルの変更。Value が nil なら削除。
type cellOp struct {
	subjectID int64
	lineID    *int64 // 金額の内訳（nil は科目への直接入力）
	driverID  int64  // ドライバー値
	month     string
	value     *string
}

// planImport は取込の途中の状態（行の検証と、施策ごとの変更）。
type planImport struct {
	ctx      context.Context
	tx       *sql.Tx
	u        auth.User
	s        Scenario
	months   []string
	errs     *csvio.RowErrors
	result   *planImportResult
	acts     map[string]int64 // 施策コード → ID
	summary  map[int64]activity.Summary
	ops      map[int64][]cellOp
	warnings map[string]int
}

// importPlanValues は POST /api/scenarios/{id}/plan-values/import（?dry_run=true で確認のみ）。
// 金額かドライバー値の CSV（ヘッダーで判定）を取り込む。権限は数値の入力と同じで、施策ごとに判定する。
func (h *Handler) importPlanValues(w http.ResponseWriter, r *http.Request) error {
	u, err := currentUser(r)
	if err != nil {
		return err
	}
	scenarioID, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	up, err := csvio.ReadUpload(w, r)
	if err != nil {
		return err
	}
	if len(up.Data) > maxPlanCSVBytes {
		return &httpx.Error{Status: http.StatusRequestEntityTooLarge, Code: "too_large", Message: "ファイルは 5MB 以下にしてください"}
	}
	header, err := csvio.Header(up.Data)
	if err != nil {
		return err
	}
	kind := ""
	switch {
	case slices.Contains(header, "driver_code"):
		kind = "driver_values"
	case slices.Contains(header, "subject_code"):
		kind = "amounts"
	default:
		return httpx.BadRequest("金額の CSV（subject_code の列）か、ドライバー値の CSV（driver_code の列）を指定してください")
	}
	ctx := r.Context()
	s, err := findScenario(ctx, h.db, scenarioID, "")
	if err != nil {
		return err
	}
	months := calc.FiscalMonths(s.FiscalYear)
	keys, refs := amountKeyColumns, amountRefColumns
	if kind == "driver_values" {
		keys, refs = driverKeyColumns, driverRefColumns
	}
	rows, err := csvio.ParseWithOptional(up.Data, keys, append(append([]string{}, refs...), months...))
	if err != nil {
		return err
	}
	if len(rows) > maxPlanCSVRows {
		return httpx.BadRequest("データ行は 5,000 行までです")
	}

	result := planImportResult{DryRun: up.DryRun, Kind: kind, Rows: len(rows), Activities: []planImportActivity{}, Warnings: []planImportWarning{}}
	err = audit.InTx(ctx, h.db, u.ID, &scenarioID, up.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		s, err := findScenario(ctx, tx, scenarioID, " FOR SHARE")
		if err != nil {
			return err
		}
		if s.IsLocked {
			return httpx.Conflict("ロックされたシナリオには取り込めません")
		}
		if !canEditScenario(u, s) {
			return httpx.Conflict("作成中ではないシナリオの数値は FP&A のみが入力できます")
		}
		p := &planImport{
			ctx: ctx, tx: tx, u: u, s: s, months: months, errs: &csvio.RowErrors{}, result: &result,
			summary: map[int64]activity.Summary{}, ops: map[int64][]cellOp{}, warnings: map[string]int{},
		}
		if p.acts, err = codeIndex(ctx, tx, "SELECT code, id FROM activities"); err != nil {
			return err
		}
		if kind == "amounts" {
			err = p.planAmounts(rows)
		} else {
			err = p.planDriverValues(rows)
		}
		if err != nil {
			return err
		}
		if err := p.errs.Err(); err != nil {
			return err
		}
		for _, k := range []string{"actual_month", "formula_line"} {
			if n := p.warnings[k]; n > 0 {
				result.Warnings = append(result.Warnings, planImportWarning{Kind: k, Count: n})
			}
		}

		ids := make([]int64, 0, len(p.ops))
		for id := range p.ops {
			ids = append(ids, id)
		}
		slices.Sort(ids) // 行ロックの順番をそろえる
		for _, id := range ids {
			a, err := activity.LockForEdit(ctx, tx, u, id)
			if err != nil {
				return err
			}
			if kind == "amounts" {
				err = p.applyAmounts(rec, id)
			} else {
				err = p.applyDriverValues(rec, id)
			}
			if err != nil {
				return err
			}
			if err := markEdited(ctx, tx, rec, scenarioID, id); err != nil {
				return err
			}
			result.Activities = append(result.Activities, planImportActivity{ActivityID: id, Code: a.Code, Name: a.Name, Changed: len(p.ops[id])})
		}
		slices.SortFunc(result.Activities, func(a, b planImportActivity) int {
			switch {
			case a.Code < b.Code:
				return -1
			case a.Code > b.Code:
				return 1
			}
			return 0
		})
		if up.DryRun {
			return csvio.ErrDryRun
		}
		return nil
	})
	if err != nil && !errors.Is(err, csvio.ErrDryRun) {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, result)
	return nil
}

// codeIndex はコード → ID の対応を返す。
func codeIndex(ctx context.Context, tx *sql.Tx, query string, args ...any) (map[string]int64, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var code string
		var id int64
		if err := rows.Scan(&code, &id); err != nil {
			return nil, err
		}
		out[code] = id
	}
	return out, rows.Err()
}

// activityOf は行の施策を返す。見つからなければ行のエラーにする。
func (p *planImport) activityOf(row csvio.Row) (activity.Summary, bool, error) {
	code := row.Get("activity_code")
	id, ok := p.acts[code]
	if !ok {
		p.errs.Add(row.Line, "施策コード「%s」が見つかりません", code)
		return activity.Summary{}, false, nil
	}
	if a, ok := p.summary[id]; ok {
		return a, true, nil
	}
	a, err := activity.Load(p.ctx, p.tx, p.u, id)
	if err != nil {
		return activity.Summary{}, false, err
	}
	p.summary[id] = a
	return a, true, nil
}

// readCells は行の月のセルを読む。空欄は「変えない」で返さず、"-" は削除（nil）。形式の誤りは行のエラーにする。
func (p *planImport) readCells(row csvio.Row, parse func(httpx.Validator, string, json.Number) string) map[string]*string {
	out := map[string]*string{}
	for _, m := range p.months {
		raw := row.Get(m)
		switch raw {
		case "":
			continue
		case deleteMark:
			out[m] = nil
			continue
		}
		v := httpx.Validator{}
		value := parse(v, m, json.Number(raw))
		if msg, ok := v[m]; ok {
			p.errs.Add(row.Line, "%s 列: %s（%s）", m, msg, raw)
			continue
		}
		out[m] = &value
	}
	return out
}

// changedCell は、セルの値が今の値から変わるかを返す（数値として比べる）。
func changedCell(cur string, has bool, value *string) bool {
	if value == nil {
		return has
	}
	if !has {
		return true
	}
	a, okA := new(big.Rat).SetString(cur)
	b, okB := new(big.Rat).SetString(*value)
	return !okA || !okB || a.Cmp(b) != 0
}

// collect は行の変更を集める。変わらないセルは「変更なし」、実績の月や計算式の内訳は取り込まずに注意として数える。
func (p *planImport) collect(row csvio.Row, a activity.Summary, cells map[string]*string, current func(month string) (string, bool), formula bool, op func(month string, value *string) cellOp) {
	actual := p.s.ActualMonths()
	var ops []cellOp
	for _, m := range p.months {
		value, ok := cells[m]
		if !ok {
			continue
		}
		cur, has := current(m)
		switch {
		case !changedCell(cur, has, value):
			p.result.Unchanged++
		case slices.Contains(actual, m):
			p.warnings["actual_month"]++
		case formula:
			p.warnings["formula_line"]++
		default:
			ops = append(ops, op(m, value))
		}
	}
	if len(ops) == 0 {
		return
	}
	if !a.CanEdit {
		p.errs.Add(row.Line, "施策「%s」の数値を入力する権限がありません", a.Code)
		return
	}
	p.ops[a.ID] = append(p.ops[a.ID], ops...)
}

// --- 金額 ---

type lineKey struct {
	activityID, subjectID int64
	name                  string
}

type amountKey struct {
	activityID, subjectID, lineID int64
	month                         string
}

func (p *planImport) planAmounts(rows []csvio.Row) error {
	subjects, err := codeIndex(p.ctx, p.tx, "SELECT code, id FROM subjects")
	if err != nil {
		return err
	}
	type lineInfo struct {
		id      int64
		formula bool
	}
	lines := map[lineKey]lineInfo{}
	lrows, err := p.tx.QueryContext(p.ctx, "SELECT id, activity_id, subject_id, name, formula_enabled FROM activity_lines")
	if err != nil {
		return err
	}
	for lrows.Next() {
		var k lineKey
		var l lineInfo
		if err := lrows.Scan(&l.id, &k.activityID, &k.subjectID, &k.name, &l.formula); err != nil {
			lrows.Close()
			return err
		}
		lines[k] = l
	}
	lrows.Close()
	if err := lrows.Err(); err != nil {
		return err
	}

	// 今の値（実績の月は実績、それ以外は計画値）
	current := map[amountKey]string{}
	vrows, err := p.tx.QueryContext(p.ctx, `
		SELECT activity_id, subject_id, COALESCE(line_id, 0), DATE_FORMAT(target_month, '%Y-%m'), amount
		FROM scenario_amounts WHERE scenario_id = ?`, p.s.ID)
	if err != nil {
		return err
	}
	for vrows.Next() {
		var k amountKey
		var amount string
		if err := vrows.Scan(&k.activityID, &k.subjectID, &k.lineID, &k.month, &amount); err != nil {
			vrows.Close()
			return err
		}
		if cur, ok := current[k]; ok {
			a, _ := new(big.Rat).SetString(cur)
			b, _ := new(big.Rat).SetString(amount)
			amount = new(big.Rat).Add(a, b).RatString()
		}
		current[k] = amount
	}
	vrows.Close()
	if err := vrows.Err(); err != nil {
		return err
	}

	seen := map[[3]int64]int{}
	for _, row := range rows {
		a, ok, err := p.activityOf(row)
		if err != nil {
			return err
		}
		subjectCode := row.Get("subject_code")
		subjectID, subjectOK := subjects[subjectCode]
		if !subjectOK {
			p.errs.Add(row.Line, "科目コード「%s」が見つかりません", subjectCode)
		}
		var line lineInfo
		lineName := row.Get("line_name")
		if ok && subjectOK && lineName != "" {
			var found bool
			if line, found = lines[lineKey{a.ID, subjectID, lineName}]; !found {
				p.errs.Add(row.Line, "施策「%s」の科目「%s」に内訳「%s」がありません。内訳は施策の画面で作ってください", a.Code, subjectCode, lineName)
				continue
			}
		}
		cells := p.readCells(row, parseAmount)
		if !ok || !subjectOK {
			continue
		}
		k := [3]int64{a.ID, subjectID, line.id}
		if first, dup := seen[k]; dup {
			p.errs.Add(row.Line, "%d 行目と同じ施策・科目・内訳の行です", first)
			continue
		}
		seen[k] = row.Line
		var lineID *int64
		if line.id != 0 {
			id := line.id
			lineID = &id
		}
		p.collect(row, a, cells,
			func(m string) (string, bool) {
				v, has := current[amountKey{a.ID, subjectID, line.id, m}]
				return v, has
			},
			line.formula,
			func(m string, value *string) cellOp {
				return cellOp{subjectID: subjectID, lineID: lineID, month: m, value: value}
			})
	}
	return nil
}

// applyAmounts は施策の金額を書き込む（画面の直接入力と同じく、source は manual、仮の値の印は外す）。
func (p *planImport) applyAmounts(rec *audit.Recorder, activityID int64) error {
	existing, err := calc.LoadFacts(p.ctx, p.tx, p.s.ID, activityID)
	if err != nil {
		return err
	}
	for _, op := range p.ops[activityID] {
		before, found := existing[calc.Key{SubjectID: op.subjectID, LineID: deref(op.lineID), Month: op.month}]
		if op.value == nil {
			if !found {
				continue
			}
			if _, err := p.tx.ExecContext(p.ctx, "DELETE FROM budget_facts WHERE id = ?", before.ID); err != nil {
				return err
			}
			if err := rec.Delete(p.ctx, "budget_facts", before.ID, before); err != nil {
				return err
			}
			p.result.Deleted++
			continue
		}
		after := calc.Fact{
			ScenarioID: p.s.ID, ActivityID: activityID, SubjectID: op.subjectID, LineID: op.lineID, TargetMonth: op.month,
			Amount: *op.value, Source: "manual",
		}
		if found {
			after.ID = before.ID
			if err := calc.UpdateFact(p.ctx, p.tx, after); err != nil {
				return err
			}
			if err := rec.Update(p.ctx, "budget_facts", before.ID, before, after); err != nil {
				return err
			}
			p.result.Updated++
			continue
		}
		if after.ID, err = calc.InsertFact(p.ctx, p.tx, after); err != nil {
			return err
		}
		if err := rec.Insert(p.ctx, "budget_facts", after.ID, after); err != nil {
			return err
		}
		p.result.Inserted++
	}
	return nil
}

// --- ドライバー値 ---

type driverKey struct {
	activityID int64
	code       string
}

func (p *planImport) planDriverValues(rows []csvio.Row) error {
	drivers := map[driverKey]int64{}
	drows, err := p.tx.QueryContext(p.ctx, "SELECT id, activity_id, code FROM activity_drivers")
	if err != nil {
		return err
	}
	for drows.Next() {
		var k driverKey
		var id int64
		if err := drows.Scan(&id, &k.activityID, &k.code); err != nil {
			drows.Close()
			return err
		}
		drivers[k] = id
	}
	drows.Close()
	if err := drows.Err(); err != nil {
		return err
	}

	type valueKey struct {
		driverID int64
		month    string
	}
	current := map[valueKey]string{}
	vrows, err := p.tx.QueryContext(p.ctx,
		"SELECT activity_driver_id, DATE_FORMAT(target_month, '%Y-%m'), value FROM driver_values WHERE scenario_id = ?", p.s.ID)
	if err != nil {
		return err
	}
	for vrows.Next() {
		var k valueKey
		var value string
		if err := vrows.Scan(&k.driverID, &k.month, &value); err != nil {
			vrows.Close()
			return err
		}
		current[k] = value
	}
	vrows.Close()
	if err := vrows.Err(); err != nil {
		return err
	}

	seen := map[int64]int{}
	for _, row := range rows {
		a, ok, err := p.activityOf(row)
		if err != nil {
			return err
		}
		var driverID int64
		code := row.Get("driver_code")
		if ok {
			if driverID = drivers[driverKey{a.ID, code}]; driverID == 0 {
				p.errs.Add(row.Line, "施策「%s」にドライバー「%s」がありません。ドライバーは施策の画面で作ってください", a.Code, code)
				continue
			}
		}
		cells := p.readCells(row, parseDriverValue)
		if !ok {
			continue
		}
		if first, dup := seen[driverID]; dup {
			p.errs.Add(row.Line, "%d 行目と同じ施策・ドライバーの行です", first)
			continue
		}
		seen[driverID] = row.Line
		p.collect(row, a, cells,
			func(m string) (string, bool) {
				v, has := current[valueKey{driverID, m}]
				return v, has
			},
			false,
			func(m string, value *string) cellOp {
				return cellOp{driverID: driverID, month: m, value: value}
			})
	}
	return nil
}

// applyDriverValues は施策のドライバー値を書き込み、計算式の金額を計算し直す。仮の値の印は外す。
func (p *planImport) applyDriverValues(rec *audit.Recorder, activityID int64) error {
	for _, op := range p.ops[activityID] {
		before := driverValueRecord{ScenarioID: p.s.ID, ActivityDriverID: op.driverID, TargetMonth: op.month}
		err := p.tx.QueryRowContext(p.ctx, `
			SELECT id, value, is_provisional, COALESCE(provisional_reason, '')
			FROM driver_values WHERE activity_driver_id = ? AND scenario_id = ? AND target_month = ? FOR UPDATE`,
			op.driverID, p.s.ID, op.month+"-01",
		).Scan(&before.ID, &before.Value, &before.IsProvisional, &before.ProvisionalReason)
		found := err == nil
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		before.Value = string(trimDecimal(before.Value))

		if op.value == nil {
			if !found {
				continue
			}
			if _, err := p.tx.ExecContext(p.ctx, "DELETE FROM driver_values WHERE id = ?", before.ID); err != nil {
				return err
			}
			if err := rec.Delete(p.ctx, "driver_values", before.ID, before); err != nil {
				return err
			}
			p.result.Deleted++
			continue
		}
		after := driverValueRecord{ScenarioID: p.s.ID, ActivityDriverID: op.driverID, TargetMonth: op.month, Value: string(trimDecimal(*op.value))}
		if found {
			after.ID = before.ID
			if _, err := p.tx.ExecContext(p.ctx,
				"UPDATE driver_values SET value = ?, is_provisional = FALSE, provisional_reason = NULL WHERE id = ?", *op.value, before.ID); err != nil {
				return err
			}
			if err := rec.Update(p.ctx, "driver_values", before.ID, before, after); err != nil {
				return err
			}
			p.result.Updated++
			continue
		}
		res, err := p.tx.ExecContext(p.ctx,
			"INSERT INTO driver_values (activity_driver_id, scenario_id, target_month, value, is_provisional) VALUES (?, ?, ?, ?, FALSE)",
			op.driverID, p.s.ID, op.month+"-01", *op.value)
		if err != nil {
			return err
		}
		after.ID, _ = res.LastInsertId()
		if err := rec.Insert(p.ctx, "driver_values", after.ID, after); err != nil {
			return err
		}
		p.result.Inserted++
	}
	return calc.Recalculate(p.ctx, p.tx, rec, p.s.ID, activityID)
}
