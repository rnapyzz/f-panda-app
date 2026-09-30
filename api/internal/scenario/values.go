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

type amountRow struct {
	SubjectID  int64        `json:"subject_id"`
	Code       string       `json:"code"`
	Name       string       `json:"name"`
	Category   string       `json:"category"`
	HasFormula bool         `json:"has_formula"`
	Values     []amountCell `json:"values"`
}

// valuesView はシナリオ×施策の入力画面用のデータ。
type valuesView struct {
	Scenario  Scenario         `json:"scenario"`
	Activity  activity.Summary `json:"activity"`
	Months    []string         `json:"months"`
	Editable  bool             `json:"editable"`
	Drivers   []driverRow      `json:"drivers"`
	Amounts   []amountRow      `json:"amounts"`
	Condition *string          `json:"condition"`
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
	view, err := h.loadValues(r.Context(), u, scenarioID, activityID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, view)
	return nil
}

func (h *Handler) loadValues(ctx context.Context, u auth.User, scenarioID, activityID int64) (valuesView, error) {
	s, err := findScenario(ctx, h.db, scenarioID, "")
	if err != nil {
		return valuesView{}, err
	}
	a, err := activity.Load(ctx, h.db, u, activityID)
	if err != nil {
		return valuesView{}, err
	}
	v := valuesView{
		Scenario: s,
		Activity: a,
		Months:   calc.FiscalMonths(s.FiscalYear),
		Editable: a.CanEdit && !s.IsLocked && s.ScenarioKind != "actual",
		Drivers:  []driverRow{},
		Amounts:  []amountRow{},
	}

	// ドライバーと値
	rows, err := h.db.QueryContext(ctx, `
		SELECT d.id, d.code, d.name, d.driver_kind, COALESCE(d.unit, ''),
		       DATE_FORMAT(v.target_month, '%Y-%m'), v.value, v.is_provisional, COALESCE(v.provisional_reason, '')
		FROM activity_drivers d
		LEFT JOIN driver_values v ON v.activity_driver_id = d.id AND v.scenario_id = ?
		WHERE d.activity_id = ?
		ORDER BY d.id, v.target_month`, scenarioID, activityID)
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

	// 金額: 金額がある科目と、計算式がある科目
	rows2, err := h.db.QueryContext(ctx, `
		SELECT s.id, s.code, s.name, s.category, f.id IS NOT NULL,
		       DATE_FORMAT(b.target_month, '%Y-%m'), b.amount, b.source, b.is_provisional, COALESCE(b.provisional_reason, '')
		FROM subjects s
		LEFT JOIN activity_formulas f ON f.subject_id = s.id AND f.activity_id = ?
		LEFT JOIN budget_facts b ON b.subject_id = s.id AND b.activity_id = ? AND b.scenario_id = ?
		WHERE f.id IS NOT NULL OR b.id IS NOT NULL
		ORDER BY s.category DESC, s.sort_order, s.code, b.target_month`, activityID, activityID, scenarioID)
	if err != nil {
		return valuesView{}, err
	}
	defer rows2.Close()
	for rows2.Next() {
		var a amountRow
		var month, amount, source sql.NullString
		var provisional sql.NullBool
		var reason string
		if err := rows2.Scan(&a.SubjectID, &a.Code, &a.Name, &a.Category, &a.HasFormula, &month, &amount, &source, &provisional, &reason); err != nil {
			return valuesView{}, err
		}
		if n := len(v.Amounts); n == 0 || v.Amounts[n-1].SubjectID != a.SubjectID {
			a.Values = []amountCell{}
			v.Amounts = append(v.Amounts, a)
		}
		if month.Valid {
			last := &v.Amounts[len(v.Amounts)-1]
			last.Values = append(last.Values, amountCell{
				TargetMonth: month.String, Amount: json.Number(amount.String), Source: source.String,
				IsProvisional: provisional.Bool, ProvisionalReason: reason,
			})
		}
	}
	if err := rows2.Err(); err != nil {
		return valuesView{}, err
	}

	var cond string
	err = h.db.QueryRowContext(ctx, "SELECT description FROM scenario_conditions WHERE scenario_id = ? AND activity_id = ?", scenarioID, activityID).Scan(&cond)
	switch {
	case err == nil:
		v.Condition = &cond
	case !errors.Is(err, sql.ErrNoRows):
		return valuesView{}, err
	}
	return v, nil
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
		if s.ScenarioKind == "actual" {
			return httpx.Conflict("実績シナリオの数値は取込でのみ登録できます")
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
	view, err := h.loadValues(r.Context(), u, scenarioID, activityID)
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

// validateCell は月と「仮の値」の入力を検証する。仮の値には理由が必須。
func validateCell(v httpx.Validator, prefix string, c *cellInput, months []string) {
	c.TargetMonth = strings.TrimSpace(c.TargetMonth)
	if !slices.Contains(months, c.TargetMonth) {
		v.Add(prefix+".target_month", fmt.Sprintf("対象月は %s〜%s の YYYY-MM で指定してください", months[0], months[len(months)-1]))
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

// putDriverValues は PUT /api/scenarios/{id}/activities/{aid}/driver-values。
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
	if err := checkBatch(len(req.Values), req.Reason); err != nil {
		return err
	}

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

		months := calc.FiscalMonths(s.FiscalYear)
		v := httpx.Validator{}
		seen := map[string]bool{}
		parsed := make([]string, len(req.Values))
		for i := range req.Values {
			in := &req.Values[i]
			prefix := fmt.Sprintf("values[%d]", i)
			if !drivers[in.DriverID] {
				v.Add(prefix+".driver_id", "この施策のドライバーを指定してください")
			}
			validateCell(v, prefix, &in.cellInput, months)
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
		return calc.Recalculate(ctx, tx, rec, scenarioID, activityID)
	})
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
	Amount    *json.Number `json:"amount"` // null で削除
	cellInput
}

type amountsRequest struct {
	Amounts []amountInput `json:"amounts"`
	Reason  string        `json:"reason"`
}

// putAmounts は PUT /api/scenarios/{id}/activities/{aid}/amounts。
// 金額（円）を直接入力する。計算式で算出する科目（算出方式が formula で計算式がある科目）は入力できない。変更理由が必須。
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
		formulaSubjects := map[int64]bool{}
		if a.CalcMode == "formula" {
			var err error
			if formulaSubjects, err = calc.FormulaSubjects(ctx, tx, activityID); err != nil {
				return err
			}
		}

		months := calc.FiscalMonths(s.FiscalYear)
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
			case formulaSubjects[in.SubjectID]:
				v.Add(prefix+".subject_id", "計算式で算出する科目のため直接入力できません")
			}
			validateCell(v, prefix, &in.cellInput, months)
			key := fmt.Sprintf("%d/%s", in.SubjectID, in.TargetMonth)
			if seen[key] {
				v.Add(prefix, "同じ科目・月が重複しています")
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
			before, found := existing[in.SubjectID][in.TargetMonth]
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
				ScenarioID: scenarioID, ActivityID: activityID, SubjectID: in.SubjectID, TargetMonth: in.TargetMonth,
				Amount: amounts[i], Source: "manual", IsProvisional: in.IsProvisional, ProvisionalReason: in.ProvisionalReason,
			}
			if found {
				after.ID = before.ID
				if before == after {
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
