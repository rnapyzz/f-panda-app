package scenario

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"slices"
	"strings"

	"github.com/rnapyzz/f-panda-app/api/internal/activity"
	"github.com/rnapyzz/f-panda-app/api/internal/audit"
	"github.com/rnapyzz/f-panda-app/api/internal/auth"
	"github.com/rnapyzz/f-panda-app/api/internal/calc"
	"github.com/rnapyzz/f-panda-app/api/internal/dbx"
	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
)

const (
	maxItemsPerRequest = 1000
	maxReasonLen       = 2000
)

// maxAmount は budget_facts.amount（DECIMAL(18,0)）に入る絶対値の上限。
var maxAmount = new(big.Int).Sub(new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil), big.NewInt(1))

// maxDriverValue は driver_values.value（DECIMAL(24,6)）の整数部に入る絶対値の上限。
var maxDriverValue = new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))

// --- 参照 ---

type valueCell struct {
	TargetMonth       string      `json:"target_month"`
	Value             json.Number `json:"value"`
	IsProvisional     bool        `json:"is_provisional"`
	ProvisionalReason string      `json:"provisional_reason"`
}

type driverRow struct {
	ID         int64       `json:"id"`
	Code       string      `json:"code"`
	Name       string      `json:"name"`
	DriverKind string      `json:"driver_kind"`
	Unit       string      `json:"unit"`
	Values     []valueCell `json:"values"`
}

type amountCell struct {
	TargetMonth       string      `json:"target_month"`
	Amount            json.Number `json:"amount"`
	Source            string      `json:"source"`
	IsProvisional     bool        `json:"is_provisional"`
	ProvisionalReason string      `json:"provisional_reason"`
}

// amountRow は科目の金額。Values は科目への直接入力（内訳なし）の金額、Lines は内訳ごとの金額。
// 科目の金額はそれらの合計。
type amountRow struct {
	SubjectID int64        `json:"subject_id"`
	Code      string       `json:"code"`
	Name      string       `json:"name"`
	Category  string       `json:"category"`
	Values    []amountCell `json:"values"`
	Lines     []lineRow    `json:"lines"`
}

// lineRow は内訳の金額。FormulaEnabled なら計算式で算出され、直接入力できない。
type lineRow struct {
	ID             int64        `json:"id"`
	Name           string       `json:"name"`
	Expression     string       `json:"expression"`
	FormulaEnabled bool         `json:"formula_enabled"`
	Values         []amountCell `json:"values"`
}

// valuesView はシナリオ×施策の入力画面用のデータ。
type valuesView struct {
	Scenario Scenario         `json:"scenario"`
	Activity activity.Summary `json:"activity"`
	Months   []string         `json:"months"`
	// ActualMonths は実績の月（決算確定月以前）。金額は実績で、入力できない
	ActualMonths []string    `json:"actual_months"`
	Editable     bool        `json:"editable"`
	Drivers      []driverRow `json:"drivers"`
	Amounts      []amountRow `json:"amounts"`
	Condition    *string     `json:"condition"`
}

// getValues は GET /api/scenarios/{id}/activities/{aid}。
// ドライバー値・金額（計算式がある科目を含む）・想定条件を月別に返す。
func (h *Handler) getValues(w http.ResponseWriter, r *http.Request) error {
	u, err := currentUser(r)
	if err != nil {
		return err
	}
	scenarioID, activityID, err := pathIDs(r)
	if err != nil {
		return err
	}
	view, err := h.loadValues(r.Context(), h.db, u, scenarioID, activityID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, view)
	return nil
}

// queryer は *sql.DB と *sql.Tx の共通部分。
type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// loadValues は数値入力画面のデータを読み込む。q はトランザクション（試算）でもよい。
func (h *Handler) loadValues(ctx context.Context, q queryer, u auth.User, scenarioID, activityID int64) (valuesView, error) {
	s, err := findScenario(ctx, q, scenarioID, "")
	if err != nil {
		return valuesView{}, err
	}
	a, err := activity.Load(ctx, q, u, activityID)
	if err != nil {
		return valuesView{}, err
	}
	v := valuesView{
		Scenario:     s,
		Activity:     a,
		Months:       calc.FiscalMonths(s.FiscalYear),
		ActualMonths: s.ActualMonths(),
		Editable:     a.CanEdit && !s.IsLocked && canEditScenario(u, s),
		Drivers:      []driverRow{},
		Amounts:      []amountRow{},
	}

	// ドライバーと値
	rows, err := q.QueryContext(ctx, `
		SELECT d.id, d.code, d.name, d.driver_kind, COALESCE(d.unit, ''),
		       DATE_FORMAT(v.target_month, '%Y-%m'), v.value, v.is_provisional, COALESCE(v.provisional_reason, '')
		FROM activity_drivers d
		LEFT JOIN driver_values v ON v.activity_driver_id = d.id AND v.scenario_id = ?
		WHERE d.activity_id = ?
		ORDER BY d.sort_order, d.id, v.target_month`, scenarioID, activityID)
	if err != nil {
		return valuesView{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var d driverRow
		var month, value sql.NullString
		var provisional sql.NullBool
		var reason string
		if err := rows.Scan(&d.ID, &d.Code, &d.Name, &d.DriverKind, &d.Unit, &month, &value, &provisional, &reason); err != nil {
			return valuesView{}, err
		}
		if n := len(v.Drivers); n == 0 || v.Drivers[n-1].ID != d.ID {
			d.Values = []valueCell{}
			v.Drivers = append(v.Drivers, d)
		}
		if month.Valid {
			last := &v.Drivers[len(v.Drivers)-1]
			last.Values = append(last.Values, valueCell{
				TargetMonth: month.String, Value: trimDecimal(value.String),
				IsProvisional: provisional.Bool, ProvisionalReason: reason,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return valuesView{}, err
	}

	// 金額: 内訳がある科目と、金額がある科目
	if v.Amounts, err = loadAmounts(ctx, q, scenarioID, activityID); err != nil {
		return valuesView{}, err
	}

	var cond string
	err = q.QueryRowContext(ctx, "SELECT description FROM scenario_conditions WHERE scenario_id = ? AND activity_id = ?", scenarioID, activityID).Scan(&cond)
	switch {
	case err == nil:
		v.Condition = &cond
	case !errors.Is(err, sql.ErrNoRows):
		return valuesView{}, err
	}
	return v, nil
}

// canEditScenario は、ユーザーがシナリオの数値を入力できるか（ロックと施策の権限は別に判定する）。
// 作成中のシナリオは施策の編集権限があれば入力でき、それ以外のシナリオは FP&A のみが入力できる。
func canEditScenario(u auth.User, s Scenario) bool {
	return s.IsActive || u.Role == auth.RoleFPAAdmin
}

// loadAmounts は科目ごとの金額（科目への直接入力と内訳）を返す。
// 決算確定月以前の月は実績（scenario_amounts ビュー、source は actual）で、科目への直接入力として返す。
func loadAmounts(ctx context.Context, q queryer, scenarioID, activityID int64) ([]amountRow, error) {
	rows := map[int64]*amountRow{}
	lineOf := map[int64]*lineRow{}
	get := func(subjectID int64) *amountRow {
		if r, ok := rows[subjectID]; ok {
			return r
		}
		r := &amountRow{SubjectID: subjectID, Values: []amountCell{}, Lines: []lineRow{}}
		rows[subjectID] = r
		return r
	}

	lrows, err := q.QueryContext(ctx, `
		SELECT id, subject_id, name, COALESCE(expression, ''), formula_enabled
		FROM activity_lines WHERE activity_id = ? ORDER BY subject_id, sort_order, id`, activityID)
	if err != nil {
		return nil, err
	}
	for lrows.Next() {
		var l lineRow
		var subjectID int64
		if err := lrows.Scan(&l.ID, &subjectID, &l.Name, &l.Expression, &l.FormulaEnabled); err != nil {
			lrows.Close()
			return nil, err
		}
		l.Values = []amountCell{}
		r := get(subjectID)
		r.Lines = append(r.Lines, l)
	}
	lrows.Close()
	if err := lrows.Err(); err != nil {
		return nil, err
	}
	for _, r := range rows {
		for i := range r.Lines {
			lineOf[r.Lines[i].ID] = &r.Lines[i]
		}
	}

	frows, err := q.QueryContext(ctx, `
		SELECT subject_id, line_id, DATE_FORMAT(target_month, '%Y-%m'), amount, source, is_provisional, COALESCE(provisional_reason, '')
		FROM scenario_amounts WHERE scenario_id = ? AND activity_id = ? ORDER BY target_month`, scenarioID, activityID)
	if err != nil {
		return nil, err
	}
	for frows.Next() {
		var subjectID int64
		var lineID sql.NullInt64
		var c amountCell
		var amount string
		if err := frows.Scan(&subjectID, &lineID, &c.TargetMonth, &amount, &c.Source, &c.IsProvisional, &c.ProvisionalReason); err != nil {
			frows.Close()
			return nil, err
		}
		c.Amount = json.Number(amount)
		if l, ok := lineOf[lineID.Int64]; lineID.Valid && ok {
			l.Values = append(l.Values, c)
		} else {
			r := get(subjectID)
			r.Values = append(r.Values, c)
		}
	}
	frows.Close()
	if err := frows.Err(); err != nil {
		return nil, err
	}

	// 科目の情報を付けて、区分（収益 → 費用）・表示順・コードの順に並べる
	srows, err := q.QueryContext(ctx, "SELECT id, code, name, category FROM subjects ORDER BY category, sort_order, code")
	if err != nil {
		return nil, err
	}
	defer srows.Close()
	out := []amountRow{}
	for srows.Next() {
		var id int64
		var code, name, category string
		if err := srows.Scan(&id, &code, &name, &category); err != nil {
			return nil, err
		}
		if r, ok := rows[id]; ok {
			r.Code, r.Name, r.Category = code, name, category
			out = append(out, *r)
		}
	}
	return out, srows.Err()
}

// --- 更新の共通処理 ---

// editTx はシナリオと施策を行ロックし、数値を編集できることを確認してから fn を実行する。
func (h *Handler) editTx(r *http.Request, scenarioID, activityID int64, reason string,
	fn func(ctx context.Context, tx *sql.Tx, rec *audit.Recorder, s Scenario, a activity.Summary) error) error {
	u, err := currentUser(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	return audit.InTx(ctx, h.db, u.ID, &scenarioID, reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		s, err := findScenario(ctx, tx, scenarioID, " FOR SHARE")
		if err != nil {
			return err
		}
		if s.IsLocked {
			return httpx.Conflict("ロックされたシナリオは変更できません")
		}
		if !canEditScenario(u, s) {
			return httpx.Conflict("作成中ではないシナリオの数値は FP&A のみが入力できます")
		}
		a, err := activity.LockForEdit(ctx, tx, u, activityID)
		if err != nil {
			return err
		}
		return fn(ctx, tx, rec, s, a)
	})
}

func (h *Handler) respondValues(w http.ResponseWriter, r *http.Request, scenarioID, activityID int64) error {
	u, err := currentUser(r)
	if err != nil {
		return err
	}
	view, err := h.loadValues(r.Context(), h.db, u, scenarioID, activityID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, view)
	return nil
}

func pathIDs(r *http.Request) (scenarioID, activityID int64, err error) {
	if scenarioID, err = httpx.PathID(r, "id"); err != nil {
		return 0, 0, err
	}
	if activityID, err = httpx.PathID(r, "aid"); err != nil {
		return 0, 0, err
	}
	return scenarioID, activityID, nil
}

// cellInput は1つの月の値に共通する入力項目。
type cellInput struct {
	TargetMonth       string `json:"target_month"`
	IsProvisional     bool   `json:"is_provisional"`
	ProvisionalReason string `json:"provisional_reason"`
}

// validateCell は月と「仮の値」の入力を検証する。入力できるのは計画値の月（決算確定月より後）だけ。仮の値には理由が必須。
func validateCell(v httpx.Validator, prefix string, c *cellInput, s Scenario) {
	c.TargetMonth = strings.TrimSpace(c.TargetMonth)
	months := calc.FiscalMonths(s.FiscalYear)
	switch {
	case !slices.Contains(months, c.TargetMonth):
		v.Add(prefix+".target_month", fmt.Sprintf("対象月は %s〜%s の YYYY-MM で指定してください", months[0], months[len(months)-1]))
	case !slices.Contains(s.PlanMonths(), c.TargetMonth):
		v.Add(prefix+".target_month", fmt.Sprintf("%s は決算確定月（%s）以前の実績の月のため入力できません", c.TargetMonth, derefStr(s.ActualThrough)))
	}
	c.ProvisionalReason = v.OptionalText(prefix+".provisional_reason", "仮の値の理由", c.ProvisionalReason, maxReasonLen)
	if c.IsProvisional && c.ProvisionalReason == "" {
		v.Add(prefix+".provisional_reason", "仮の値には理由を入力してください")
	}
	if !c.IsProvisional {
		c.ProvisionalReason = ""
	}
}

// --- ドライバー値 ---

type driverValueInput struct {
	DriverID int64        `json:"driver_id"`
	Value    *json.Number `json:"value"` // null で削除
	cellInput
}

type driverValuesRequest struct {
	Values []driverValueInput `json:"values"`
	Reason string             `json:"reason"`
}

// driverValueRecord は監査ログに記録するドライバー値。
type driverValueRecord struct {
	ID                int64  `json:"id"`
	ScenarioID        int64  `json:"scenario_id"`
	ActivityDriverID  int64  `json:"activity_driver_id"`
	TargetMonth       string `json:"target_month"`
	Value             string `json:"value"`
	IsProvisional     bool   `json:"is_provisional"`
	ProvisionalReason string `json:"provisional_reason"`
}

// putDriverValues は PUT /api/scenarios/{id}/activities/{aid}/driver-values（?dry_run=true で試算）。
// 指定した（ドライバー, 月）の値を登録・更新・削除（value: null）し、計算式の金額を再計算する。変更理由が必須。
func (h *Handler) putDriverValues(w http.ResponseWriter, r *http.Request) error {
	scenarioID, activityID, err := pathIDs(r)
	if err != nil {
		return err
	}
	var req driverValuesRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	// dry_run=true は試算: 保存したときと同じ検証・再計算を行い、結果を返してロールバックする（変更理由は不要）
	dryRun := r.URL.Query().Get("dry_run") == "true"
	if dryRun {
		req.Reason = "試算"
	}
	if err := checkBatch(len(req.Values), req.Reason); err != nil {
		return err
	}
	u, err := currentUser(r)
	if err != nil {
		return err
	}

	var preview valuesView
	err = h.editTx(r, scenarioID, activityID, req.Reason, func(ctx context.Context, tx *sql.Tx, rec *audit.Recorder, s Scenario, a activity.Summary) error {
		drivers := map[int64]bool{}
		rows, err := tx.QueryContext(ctx, "SELECT id FROM activity_drivers WHERE activity_id = ?", activityID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			drivers[id] = true
		}
		rows.Close()

		v := httpx.Validator{}
		seen := map[string]bool{}
		parsed := make([]string, len(req.Values))
		for i := range req.Values {
			in := &req.Values[i]
			prefix := fmt.Sprintf("values[%d]", i)
			if !drivers[in.DriverID] {
				v.Add(prefix+".driver_id", "この施策のドライバーを指定してください")
			}
			validateCell(v, prefix, &in.cellInput, s)
			key := fmt.Sprintf("%d/%s", in.DriverID, in.TargetMonth)
			if seen[key] {
				v.Add(prefix, "同じドライバー・月が重複しています")
			}
			seen[key] = true
			if in.Value != nil {
				parsed[i] = parseDriverValue(v, prefix+".value", *in.Value)
			}
		}
		if err := v.Err(); err != nil {
			return err
		}

		for i, in := range req.Values {
			var before driverValueRecord
			err := tx.QueryRowContext(ctx, `
				SELECT id, value, is_provisional, COALESCE(provisional_reason, '')
				FROM driver_values WHERE activity_driver_id = ? AND scenario_id = ? AND target_month = ? FOR UPDATE`,
				in.DriverID, scenarioID, in.TargetMonth+"-01",
			).Scan(&before.ID, &before.Value, &before.IsProvisional, &before.ProvisionalReason)
			found := err == nil
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			before.ScenarioID, before.ActivityDriverID, before.TargetMonth = scenarioID, in.DriverID, in.TargetMonth
			before.Value = string(trimDecimal(before.Value))

			if in.Value == nil {
				if found {
					if _, err := tx.ExecContext(ctx, "DELETE FROM driver_values WHERE id = ?", before.ID); err != nil {
						return err
					}
					if err := rec.Delete(ctx, "driver_values", before.ID, before); err != nil {
						return err
					}
				}
				continue
			}

			after := driverValueRecord{
				ScenarioID: scenarioID, ActivityDriverID: in.DriverID, TargetMonth: in.TargetMonth,
				Value: string(trimDecimal(parsed[i])), IsProvisional: in.IsProvisional, ProvisionalReason: in.ProvisionalReason,
			}
			if found {
				after.ID = before.ID
				if before == after {
					continue
				}
				if _, err := tx.ExecContext(ctx,
					"UPDATE driver_values SET value = ?, is_provisional = ?, provisional_reason = ? WHERE id = ?",
					parsed[i], in.IsProvisional, dbx.NullString(in.ProvisionalReason), before.ID,
				); err != nil {
					return err
				}
				if err := rec.Update(ctx, "driver_values", before.ID, before, after); err != nil {
					return err
				}
				continue
			}
			res, err := tx.ExecContext(ctx, `
				INSERT INTO driver_values (activity_driver_id, scenario_id, target_month, value, is_provisional, provisional_reason)
				VALUES (?, ?, ?, ?, ?, ?)`,
				in.DriverID, scenarioID, in.TargetMonth+"-01", parsed[i], in.IsProvisional, dbx.NullString(in.ProvisionalReason),
			)
			if err != nil {
				return err
			}
			after.ID, _ = res.LastInsertId()
			if err := rec.Insert(ctx, "driver_values", after.ID, after); err != nil {
				return err
			}
		}
		if err := calc.Recalculate(ctx, tx, rec, scenarioID, activityID); err != nil {
			return err
		}
		if !dryRun {
			return nil
		}
		if preview, err = h.loadValues(ctx, tx, u, scenarioID, activityID); err != nil {
			return err
		}
		return errDryRun // 試算の結果を読んでからロールバックする
	})
	if dryRun && errors.Is(err, errDryRun) {
		httpx.WriteJSON(w, http.StatusOK, preview)
		return nil
	}
	if err != nil {
		return err
	}
	return h.respondValues(w, r, scenarioID, activityID)
}

// parseDriverValue はドライバー値を検証し、DB に保存する文字列（小数点以下6桁）を返す。
func parseDriverValue(v httpx.Validator, field string, n json.Number) string {
	x, ok := new(big.Rat).SetString(string(n))
	switch {
	case !ok:
		v.Add(field, "数値を入力してください")
	case !new(big.Rat).Mul(x, big.NewRat(1_000_000, 1)).IsInt():
		v.Add(field, "小数点以下は6桁までで入力してください")
	case new(big.Rat).Abs(x).Cmp(maxDriverValue) >= 0:
		v.Add(field, "値が大きすぎます")
	default:
		return x.FloatString(6)
	}
	return ""
}

// --- 金額（直接入力） ---

type amountInput struct {
	SubjectID int64        `json:"subject_id"`
	LineID    *int64       `json:"line_id"` // 内訳。null は科目への直接入力
	Amount    *json.Number `json:"amount"`  // null で削除
	cellInput
}

type amountsRequest struct {
	Amounts []amountInput `json:"amounts"`
	Reason  string        `json:"reason"`
}

// putAmounts は PUT /api/scenarios/{id}/activities/{aid}/amounts。
// 金額（円）を直接入力する。line_id で内訳を指定する（null は科目への直接入力）。
// 計算式で反映する内訳は入力できない。変更理由が必須。
func (h *Handler) putAmounts(w http.ResponseWriter, r *http.Request) error {
	scenarioID, activityID, err := pathIDs(r)
	if err != nil {
		return err
	}
	var req amountsRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	if err := checkBatch(len(req.Amounts), req.Reason); err != nil {
		return err
	}

	err = h.editTx(r, scenarioID, activityID, req.Reason, func(ctx context.Context, tx *sql.Tx, rec *audit.Recorder, s Scenario, a activity.Summary) error {
		lines, err := calc.Lines(ctx, tx, activityID)
		if err != nil {
			return err
		}

		v := httpx.Validator{}
		seen := map[string]bool{}
		amounts := make([]string, len(req.Amounts))
		for i := range req.Amounts {
			in := &req.Amounts[i]
			prefix := fmt.Sprintf("amounts[%d]", i)
			switch ok, err := dbx.Exists(ctx, tx, "subjects", in.SubjectID); {
			case err != nil:
				return err
			case !ok:
				v.Add(prefix+".subject_id", "科目が見つかりません")
			}
			if in.LineID != nil {
				switch l, ok := lines[*in.LineID]; {
				case !ok || l.SubjectID != in.SubjectID:
					v.Add(prefix+".line_id", "この施策・科目の内訳を指定してください")
				case l.Computed:
					v.Add(prefix+".line_id", "内訳「"+l.Name+"」は計算式で反映するため直接入力できません")
				}
			}
			validateCell(v, prefix, &in.cellInput, s)
			key := fmt.Sprintf("%d/%d/%s", in.SubjectID, deref(in.LineID), in.TargetMonth)
			if seen[key] {
				v.Add(prefix, "同じ科目・内訳・月が重複しています")
			}
			seen[key] = true
			if in.Amount != nil {
				amounts[i] = parseAmount(v, prefix+".amount", *in.Amount)
			}
		}
		if err := v.Err(); err != nil {
			return err
		}

		existing, err := calc.LoadFacts(ctx, tx, scenarioID, activityID)
		if err != nil {
			return err
		}
		for i, in := range req.Amounts {
			before, found := existing[calc.Key{SubjectID: in.SubjectID, LineID: deref(in.LineID), Month: in.TargetMonth}]
			if in.Amount == nil {
				if found {
					if _, err := tx.ExecContext(ctx, "DELETE FROM budget_facts WHERE id = ?", before.ID); err != nil {
						return err
					}
					if err := rec.Delete(ctx, "budget_facts", before.ID, before); err != nil {
						return err
					}
				}
				continue
			}
			after := calc.Fact{
				ScenarioID: scenarioID, ActivityID: activityID, SubjectID: in.SubjectID, LineID: in.LineID, TargetMonth: in.TargetMonth,
				Amount: amounts[i], Source: "manual", IsProvisional: in.IsProvisional, ProvisionalReason: in.ProvisionalReason,
			}
			if found {
				after.ID = before.ID
				if before.Amount == after.Amount && before.Source == after.Source &&
					before.IsProvisional == after.IsProvisional && before.ProvisionalReason == after.ProvisionalReason {
					continue
				}
				if err := calc.UpdateFact(ctx, tx, after); err != nil {
					return err
				}
				if err := rec.Update(ctx, "budget_facts", before.ID, before, after); err != nil {
					return err
				}
				continue
			}
			if after.ID, err = calc.InsertFact(ctx, tx, after); err != nil {
				return err
			}
			if err := rec.Insert(ctx, "budget_facts", after.ID, after); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return h.respondValues(w, r, scenarioID, activityID)
}

func deref(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

// parseAmount は金額（円、整数）を検証する。
func parseAmount(v httpx.Validator, field string, n json.Number) string {
	x, ok := new(big.Int).SetString(string(n), 10)
	switch {
	case !ok:
		v.Add(field, "金額は円単位の整数で入力してください")
	case new(big.Int).Abs(x).Cmp(maxAmount) > 0:
		v.Add(field, "金額が大きすぎます")
	default:
		return x.String()
	}
	return ""
}

// --- 想定条件 ---

// putCondition は PUT /api/scenarios/{id}/activities/{aid}/condition。
// シナリオにおける施策の想定内容と発生条件（楽観/悲観の根拠など）を登録する。空文字で削除する。
func (h *Handler) putCondition(w http.ResponseWriter, r *http.Request) error {
	scenarioID, activityID, err := pathIDs(r)
	if err != nil {
		return err
	}
	var req struct {
		Description string `json:"description"`
		Reason      string `json:"reason"`
	}
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := httpx.Validator{}
	desc := v.OptionalText("description", "想定条件", req.Description, 5000)
	if err := v.Err(); err != nil {
		return err
	}

	type conditionRecord struct {
		ID          int64  `json:"id"`
		ScenarioID  int64  `json:"scenario_id"`
		ActivityID  int64  `json:"activity_id"`
		Description string `json:"description"`
	}
	err = h.editTx(r, scenarioID, activityID, req.Reason, func(ctx context.Context, tx *sql.Tx, rec *audit.Recorder, s Scenario, a activity.Summary) error {
		before := conditionRecord{ScenarioID: scenarioID, ActivityID: activityID}
		err := tx.QueryRowContext(ctx,
			"SELECT id, description FROM scenario_conditions WHERE scenario_id = ? AND activity_id = ? FOR UPDATE",
			scenarioID, activityID,
		).Scan(&before.ID, &before.Description)
		found := err == nil
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		after := before
		after.Description = desc

		switch {
		case desc == "" && found:
			if _, err := tx.ExecContext(ctx, "DELETE FROM scenario_conditions WHERE id = ?", before.ID); err != nil {
				return err
			}
			return rec.Delete(ctx, "scenario_conditions", before.ID, before)
		case desc == "":
			return nil
		case found:
			if before.Description == desc {
				return nil
			}
			if _, err := tx.ExecContext(ctx, "UPDATE scenario_conditions SET description = ? WHERE id = ?", desc, before.ID); err != nil {
				return err
			}
			return rec.Update(ctx, "scenario_conditions", before.ID, before, after)
		default:
			res, err := tx.ExecContext(ctx,
				"INSERT INTO scenario_conditions (scenario_id, activity_id, description) VALUES (?, ?, ?)",
				scenarioID, activityID, desc)
			if err != nil {
				return err
			}
			after.ID, _ = res.LastInsertId()
			return rec.Insert(ctx, "scenario_conditions", after.ID, after)
		}
	})
	if err != nil {
		return err
	}
	return h.respondValues(w, r, scenarioID, activityID)
}

// checkBatch は一括更新の件数と変更理由を確認する。
func checkBatch(n int, reason string) error {
	if n == 0 {
		return httpx.Validation(map[string]string{"values": "更新する値を指定してください"})
	}
	if n > maxItemsPerRequest {
		return httpx.Validation(map[string]string{"values": fmt.Sprintf("一度に更新できるのは%d件までです", maxItemsPerRequest)})
	}
	return activity.RequireReason(reason)
}

// trimDecimal は DECIMAL の文字列から末尾の不要な 0 を取り除く（例: "12.500000" → "12.5"）。
func trimDecimal(s string) json.Number {
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	if s == "" || s == "-" {
		s = "0"
	}
	return json.Number(s)
}
