package server_test

import (
	"fmt"
	"net/http"
	"strconv"
	"testing"
)

// scenarioFixture は、計算式で金額を算出する内訳を持つ施策（formula）と直接入力だけの施策（manual）を持つ。
type scenarioFixture struct {
	*activityFixture
	sales, cost           int64 // 科目
	formulaAct, manualAct int64 // 施策（どちらも担当者は member）
	priceID, volumeID     int64 // formulaAct のドライバー
	salesLine             int64 // formulaAct の売上の内訳（計算式で反映する）
	budget                int64 // 2026年度のシナリオ
}

func newScenarioFixture(t *testing.T) *scenarioFixture {
	t.Helper()
	f := &scenarioFixture{activityFixture: newActivityFixture(t)}
	a := f.admin
	f.sales = a.mustCreate("/api/subjects", map[string]any{"code": "4110", "name": "受託売上", "category": "revenue"})
	f.cost = a.mustCreate("/api/subjects", map[string]any{"code": "8110", "name": "外注費", "category": "expense"})

	f.formulaAct = a.mustCreate("/api/activities", activityBody(f.fn1, "SAAS-1", map[string]any{
		"owner_user_id": f.memberID, "probability": 0.5,
	}))
	base := fmt.Sprintf("/api/activities/%d", f.formulaAct)
	f.priceID = a.mustCreate(base+"/drivers", map[string]any{"code": "unit_price", "name": "単価", "driver_kind": "value"})
	f.volumeID = a.mustCreate(base+"/drivers", map[string]any{"code": "volume", "name": "件数", "driver_kind": "kpi"})
	f.salesLine = a.mustCreate(base+"/lines", map[string]any{
		"subject_id": f.sales, "name": "利用料", "expression": "unit_price * volume * probability", "formula_enabled": true, "reason": "算出式の設定",
	})

	f.manualAct = a.mustCreate("/api/activities", activityBody(f.fn1, "PRJ-1", map[string]any{"owner_user_id": f.memberID}))
	f.budget = a.mustCreate("/api/scenarios", map[string]any{"name": "2026年度 当初予算", "scenario_kind": "budget", "fiscal_year": 2026})
	return f
}

func (f *scenarioFixture) valuesPath(scenarioID, activityID int64) string {
	return fmt.Sprintf("/api/scenarios/%d/activities/%d", scenarioID, activityID)
}

// amountOf はレスポンスから科目・月の金額を取り出す。無ければ ""。
func amountOf(body map[string]any, subjectID int64, month string) (amount string, cell map[string]any) {
	for _, row := range body["amounts"].([]any) {
		r := row.(map[string]any)
		if int64(r["subject_id"].(float64)) == subjectID {
			return cellOf(r["values"], month)
		}
	}
	return "", nil
}

// lineAmountOf は内訳の金額を返す。
func lineAmountOf(body map[string]any, lineID int64, month string) (amount string, cell map[string]any) {
	for _, row := range body["amounts"].([]any) {
		for _, l := range row.(map[string]any)["lines"].([]any) {
			lm := l.(map[string]any)
			if int64(lm["id"].(float64)) == lineID {
				return cellOf(lm["values"], month)
			}
		}
	}
	return "", nil
}

func cellOf(values any, month string) (string, map[string]any) {
	for _, c := range values.([]any) {
		cm := c.(map[string]any)
		if cm["target_month"] == month {
			return numString(cm["amount"]), cm
		}
	}
	return "", nil
}

func TestScenarioManagement(t *testing.T) {
	f := newScenarioFixture(t)

	if status, _ := f.manager1.do("POST", "/api/scenarios", map[string]any{"name": "x", "scenario_kind": "budget", "fiscal_year": 2026}); status != http.StatusForbidden {
		t.Errorf("マネージャーのシナリオ作成: status = %d, want 403", status)
	}
	for name, tc := range map[string]struct {
		body  map[string]any
		field string
	}{
		"名称重複":     {map[string]any{"name": "2026年度 当初予算", "scenario_kind": "budget", "fiscal_year": 2026}, "name"},
		"種別不正":     {map[string]any{"name": "x", "scenario_kind": "plan", "fiscal_year": 2026}, "scenario_kind"},
		"年度不正":     {map[string]any{"name": "x", "scenario_kind": "budget", "fiscal_year": 26}, "fiscal_year"},
		"実績を複製で作成": {map[string]any{"name": "x", "scenario_kind": "actual", "fiscal_year": 2026, "base_scenario_id": f.budget}, "base_scenario_id"},
		"年度違いの複製元": {map[string]any{"name": "x", "scenario_kind": "forecast", "fiscal_year": 2027, "base_scenario_id": f.budget}, "base_scenario_id"},
	} {
		if status, body := f.admin.do("POST", "/api/scenarios", tc.body); status != http.StatusUnprocessableEntity || detail(body, tc.field) == "" {
			t.Errorf("%s: status = %d, body = %v", name, status, body)
		}
	}

	path := fmt.Sprintf("/api/scenarios/%d", f.budget)
	if status, body := f.admin.do("PUT", path, map[string]any{"name": "2026年度 予算"}); status != http.StatusOK || body["name"] != "2026年度 予算" {
		t.Errorf("名称変更: status = %d, body = %v", status, body)
	}

	// ロック / ロック解除
	if status, body := f.admin.do("POST", path+"/lock", nil); status != http.StatusOK || body["is_locked"] != true {
		t.Errorf("ロック: status = %d, body = %v", status, body)
	}
	if status, _ := f.admin.do("POST", path+"/lock", nil); status != http.StatusConflict {
		t.Errorf("二重ロック: status = %d, want 409", status)
	}
	if status, body := f.admin.do("POST", path+"/unlock", nil); status != http.StatusUnprocessableEntity || detail(body, "reason") == "" {
		t.Errorf("理由なしのロック解除: status = %d, body = %v", status, body)
	}
	if status, _ := f.admin.do("POST", path+"/unlock", map[string]any{"reason": "修正漏れの反映"}); status != http.StatusOK {
		t.Errorf("ロック解除: status = %d, want 200", status)
	}
	if status, _ := f.manager1.do("POST", path+"/lock", nil); status != http.StatusForbidden {
		t.Errorf("マネージャーのロック: status = %d, want 403", status)
	}

	f.admin.mustCreate("/api/scenarios", map[string]any{"name": "2027年度 予算", "scenario_kind": "budget", "fiscal_year": 2027})
	items := f.viewer.mustGet("/api/scenarios?fiscal_year=2027")["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["name"] != "2027年度 予算" {
		t.Errorf("年度での絞り込み = %v", items)
	}
}

func TestDriverValuesRecalculateAmounts(t *testing.T) {
	f := newScenarioFixture(t)
	path := f.valuesPath(f.budget, f.formulaAct) + "/driver-values"

	status, body := f.member.do("PUT", path, map[string]any{
		"reason": "初回入力",
		"values": []map[string]any{
			{"driver_id": f.priceID, "target_month": "2026-10", "value": 1000},
			{"driver_id": f.volumeID, "target_month": "2026-10", "value": 3},
			{"driver_id": f.priceID, "target_month": "2026-11", "value": "1000.5"},
			{"driver_id": f.volumeID, "target_month": "2026-11", "value": 3, "is_provisional": true, "provisional_reason": "商談中の件数"},
			{"driver_id": f.priceID, "target_month": "2026-12", "value": 1000}, // 件数がないので金額は出ない
		},
	})
	if status != http.StatusOK {
		t.Fatalf("ドライバー値の登録: status = %d, body = %v", status, body)
	}
	// 1000 * 3 * 0.5 = 1500、1000.5 * 3 * 0.5 = 1500.75 → 1501（四捨五入）
	if got, _ := lineAmountOf(body, f.salesLine, "2026-10"); got != "1500" {
		t.Errorf("2026-10 の金額 = %q, want 1500", got)
	}
	got, cell := lineAmountOf(body, f.salesLine, "2026-11")
	if got != "1501" || cell["source"] != "formula" || cell["is_provisional"] != true {
		t.Errorf("2026-11 = %v, want 1501・formula・仮の値", cell)
	}
	if got, _ := lineAmountOf(body, f.salesLine, "2026-12"); got != "" {
		t.Errorf("2026-12 の金額 = %q, want なし（件数が未入力）", got)
	}
	if body["editable"] != true {
		t.Errorf("editable = %v, want true", body["editable"])
	}

	// 値の削除で金額も消え、値の変更で金額も変わる
	status, body = f.member.do("PUT", path, map[string]any{
		"reason": "見直し",
		"values": []map[string]any{
			{"driver_id": f.volumeID, "target_month": "2026-10", "value": nil},
			{"driver_id": f.volumeID, "target_month": "2026-11", "value": 4},
		},
	})
	if status != http.StatusOK {
		t.Fatalf("更新: status = %d, body = %v", status, body)
	}
	if got, _ := lineAmountOf(body, f.salesLine, "2026-10"); got != "" {
		t.Errorf("件数削除後の 2026-10 = %q, want なし", got)
	}
	if got, cell := lineAmountOf(body, f.salesLine, "2026-11"); got != "2001" || cell["is_provisional"] != false {
		t.Errorf("2026-11 = %v, want 2001・仮の値ではない", cell)
	}

	// 確度を変えると金額も再計算される（1000.5 * 4 * 0.8 = 3201.6 → 3202）
	update := activityBody(f.fn1, "SAAS-1", map[string]any{"owner_user_id": f.memberID, "probability": 0.8, "reason": "受注確度の上方修正"})
	if status, body := f.member.do("PUT", fmt.Sprintf("/api/activities/%d", f.formulaAct), update); status != http.StatusOK {
		t.Fatalf("確度の変更: status = %d, body = %v", status, body)
	}
	if got, _ := lineAmountOf(f.member.mustGet(f.valuesPath(f.budget, f.formulaAct)), f.salesLine, "2026-11"); got != "3202" {
		t.Errorf("確度変更後の 2026-11 = %q, want 3202", got)
	}

	// 変更は変更セット（シナリオ付き）と監査ログに残る
	var n int
	if err := f.env.QueryRow(`
		SELECT COUNT(*) FROM audit_logs a JOIN change_sets c ON c.id = a.change_set_id
		WHERE a.table_name = 'budget_facts' AND c.scenario_id = ? AND c.reason = '見直し'`, f.budget).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("「見直し」での金額の監査ログ = %d件, want 2（削除1・更新1）", n)
	}
}

func TestDriverValueValidation(t *testing.T) {
	f := newScenarioFixture(t)
	path := f.valuesPath(f.budget, f.formulaAct) + "/driver-values"

	otherAct := f.admin.mustCreate("/api/activities", activityBody(f.fn1, "OTHER", nil))
	otherDriver := f.admin.mustCreate(fmt.Sprintf("/api/activities/%d/drivers", otherAct), map[string]any{"code": "x", "name": "x", "driver_kind": "kpi"})

	tests := []struct {
		name  string
		value map[string]any
		field string
	}{
		{"年度外の月", map[string]any{"driver_id": f.priceID, "target_month": "2027-04", "value": 1}, "values[0].target_month"},
		{"月の形式", map[string]any{"driver_id": f.priceID, "target_month": "2026-10-01", "value": 1}, "values[0].target_month"},
		{"他施策のドライバー", map[string]any{"driver_id": otherDriver, "target_month": "2026-10", "value": 1}, "values[0].driver_id"},
		{"小数7桁", map[string]any{"driver_id": f.priceID, "target_month": "2026-10", "value": "0.1234567"}, "values[0].value"},
		{"仮の値に理由なし", map[string]any{"driver_id": f.priceID, "target_month": "2026-10", "value": 1, "is_provisional": true}, "values[0].provisional_reason"},
	}
	for _, tt := range tests {
		status, body := f.member.do("PUT", path, map[string]any{"reason": "r", "values": []map[string]any{tt.value}})
		if status != http.StatusUnprocessableEntity || detail(body, tt.field) == "" {
			t.Errorf("%s: status = %d, body = %v", tt.name, status, body)
		}
	}
	if status, body := f.member.do("PUT", path, map[string]any{"values": []map[string]any{{"driver_id": f.priceID, "target_month": "2026-10", "value": 1}}}); status != http.StatusUnprocessableEntity || detail(body, "reason") == "" {
		t.Errorf("理由なし: status = %d, body = %v", status, body)
	}
	dup := []map[string]any{{"driver_id": f.priceID, "target_month": "2026-10", "value": 1}, {"driver_id": f.priceID, "target_month": "2026-10", "value": 2}}
	if status, body := f.member.do("PUT", path, map[string]any{"reason": "r", "values": dup}); status != http.StatusUnprocessableEntity || detail(body, "values[1]") == "" {
		t.Errorf("重複: status = %d, body = %v", status, body)
	}
}

func TestDivisionByZeroRollsBack(t *testing.T) {
	f := newScenarioFixture(t)
	base := fmt.Sprintf("/api/activities/%d", f.formulaAct)
	f.admin.mustCreate(base+"/lines", map[string]any{"subject_id": f.cost, "name": "外注費", "expression": "unit_price / volume", "formula_enabled": true, "reason": "r"})
	status, body := f.member.do("PUT", f.valuesPath(f.budget, f.formulaAct)+"/driver-values", map[string]any{
		"reason": "r",
		"values": []map[string]any{
			{"driver_id": f.priceID, "target_month": "2026-10", "value": 1000},
			{"driver_id": f.volumeID, "target_month": "2026-10", "value": 0},
		},
	})
	if status != http.StatusUnprocessableEntity || detail(body, "values") == "" {
		t.Fatalf("0除算: status = %d, body = %v", status, body)
	}
	// ロールバックされ、ドライバー値も保存されていない
	for _, d := range f.member.mustGet(f.valuesPath(f.budget, f.formulaAct))["drivers"].([]any) {
		if n := len(d.(map[string]any)["values"].([]any)); n != 0 {
			t.Errorf("0除算の後にドライバー値が %d 件残っている", n)
		}
	}
}

func TestFormulaChangeRecalculatesOnlyUnlockedScenarios(t *testing.T) {
	f := newScenarioFixture(t)
	f.member.do("PUT", f.valuesPath(f.budget, f.formulaAct)+"/driver-values", map[string]any{
		"reason": "r",
		"values": []map[string]any{
			{"driver_id": f.priceID, "target_month": "2026-10", "value": 1000},
			{"driver_id": f.volumeID, "target_month": "2026-10", "value": 2},
		},
	})
	// 予算を複製して見込を作り、予算はロックする
	forecast := f.admin.mustCreate("/api/scenarios", map[string]any{"name": "2026-10時点見込", "scenario_kind": "forecast", "fiscal_year": 2026, "base_scenario_id": f.budget})
	if status, _ := f.admin.do("POST", fmt.Sprintf("/api/scenarios/%d/lock", f.budget), nil); status != http.StatusOK {
		t.Fatalf("ロック: status = %d", status)
	}

	// 計算式を変更（確度を掛けない）
	if status, body := f.member.do("PUT", fmt.Sprintf("/api/activities/%d/lines/%d", f.formulaAct, f.salesLine), map[string]any{"name": "利用料", "expression": "unit_price * volume", "formula_enabled": true, "reason": "確度は別管理にする"}); status != http.StatusOK {
		t.Fatalf("計算式の変更: status = %d, body = %v", status, body)
	}
	if got, _ := lineAmountOf(f.viewer.mustGet(f.valuesPath(f.budget, f.formulaAct)), f.salesLine, "2026-10"); got != "1000" {
		t.Errorf("ロック済み予算の金額 = %q, want 1000（変わらない）", got)
	}
	if got, _ := lineAmountOf(f.viewer.mustGet(f.valuesPath(forecast, f.formulaAct)), f.salesLine, "2026-10"); got != "2000" {
		t.Errorf("見込の金額 = %q, want 2000（再計算される）", got)
	}
}

func TestScenarioCopy(t *testing.T) {
	f := newScenarioFixture(t)
	f.member.do("PUT", f.valuesPath(f.budget, f.formulaAct)+"/driver-values", map[string]any{
		"reason": "r", "values": []map[string]any{{"driver_id": f.priceID, "target_month": "2026-10", "value": 1000}},
	})
	f.member.do("PUT", f.valuesPath(f.budget, f.manualAct)+"/amounts", map[string]any{
		"reason": "r", "amounts": []map[string]any{{"subject_id": f.cost, "target_month": "2026-10", "amount": -500000}},
	})
	f.member.do("PUT", f.valuesPath(f.budget, f.manualAct)+"/condition", map[string]any{"description": "A社の継続受注が前提"})

	copyID := f.admin.mustCreate("/api/scenarios", map[string]any{"name": "複製", "scenario_kind": "forecast", "fiscal_year": 2026, "base_scenario_id": f.budget})
	body := f.viewer.mustGet(f.valuesPath(copyID, f.manualAct))
	if got, _ := amountOf(body, f.cost, "2026-10"); got != "-500000" {
		t.Errorf("複製された金額 = %q, want -500000", got)
	}
	if c, _ := body["condition"].(string); c != "A社の継続受注が前提" {
		t.Errorf("複製された想定条件 = %v", body["condition"])
	}
	drivers := f.viewer.mustGet(f.valuesPath(copyID, f.formulaAct))["drivers"].([]any)
	if v := drivers[0].(map[string]any)["values"].([]any); len(v) != 1 || numString(v[0].(map[string]any)["value"]) != "1000" {
		t.Errorf("複製されたドライバー値 = %v", v)
	}
}

func TestManualAmounts(t *testing.T) {
	f := newScenarioFixture(t)
	path := f.valuesPath(f.budget, f.manualAct) + "/amounts"

	status, body := f.member.do("PUT", path, map[string]any{
		"reason": "初回入力",
		"amounts": []map[string]any{
			{"subject_id": f.sales, "target_month": "2026-04", "amount": 12000000},
			{"subject_id": f.cost, "target_month": "2026-04", "amount": -8000000, "is_provisional": true, "provisional_reason": "見積待ち"},
		},
	})
	if status != http.StatusOK {
		t.Fatalf("金額の登録: status = %d, body = %v", status, body)
	}
	if got, cell := amountOf(body, f.cost, "2026-04"); got != "-8000000" || cell["source"] != "manual" || cell["provisional_reason"] != "見積待ち" {
		t.Errorf("外注費 = %v", cell)
	}

	for name, tc := range map[string]struct {
		amount map[string]any
		field  string
	}{
		"小数":      {map[string]any{"subject_id": f.sales, "target_month": "2026-05", "amount": 100.5}, "amounts[0].amount"},
		"桁あふれ":    {map[string]any{"subject_id": f.sales, "target_month": "2026-05", "amount": "1000000000000000000"}, "amounts[0].amount"},
		"存在しない科目": {map[string]any{"subject_id": 99999, "target_month": "2026-05", "amount": 1}, "amounts[0].subject_id"},
	} {
		if status, body := f.member.do("PUT", path, map[string]any{"reason": "r", "amounts": []map[string]any{tc.amount}}); status != http.StatusUnprocessableEntity || detail(body, tc.field) == "" {
			t.Errorf("%s: status = %d, body = %v", name, status, body)
		}
	}

	// 計算式で反映する内訳には直接入力できないが、同じ科目への直接入力はできる
	fpath := f.valuesPath(f.budget, f.formulaAct) + "/amounts"
	if status, body := f.member.do("PUT", fpath, map[string]any{"reason": "r", "amounts": []map[string]any{{"subject_id": f.sales, "line_id": f.salesLine, "target_month": "2026-05", "amount": 1}}}); status != http.StatusUnprocessableEntity || detail(body, "amounts[0].line_id") == "" {
		t.Errorf("計算式の内訳への直接入力: status = %d, body = %v", status, body)
	}
	if status, body := f.member.do("PUT", fpath, map[string]any{"reason": "r", "amounts": []map[string]any{{"subject_id": f.cost, "line_id": f.salesLine, "target_month": "2026-05", "amount": 1}}}); status != http.StatusUnprocessableEntity || detail(body, "amounts[0].line_id") == "" {
		t.Errorf("別科目の内訳の指定: status = %d, body = %v", status, body)
	}
	if status, body := f.member.do("PUT", fpath, map[string]any{"reason": "r", "amounts": []map[string]any{{"subject_id": f.sales, "target_month": "2026-05", "amount": 1}}}); status != http.StatusOK {
		t.Errorf("計算式の内訳がある科目への直接入力: status = %d, body = %v", status, body)
	}

	// null で削除
	status, body = f.member.do("PUT", path, map[string]any{"reason": "取消", "amounts": []map[string]any{{"subject_id": f.sales, "target_month": "2026-04", "amount": nil}}})
	if got, _ := amountOf(body, f.sales, "2026-04"); status != http.StatusOK || got != "" {
		t.Errorf("削除: status = %d, 残った金額 = %q", status, got)
	}
}

func TestValueEditingRestrictions(t *testing.T) {
	f := newScenarioFixture(t)
	values := []map[string]any{{"subject_id": f.sales, "target_month": "2026-04", "amount": 1}}

	// 担当外・閲覧者は編集できない。閲覧者は参照できるが editable は false
	for name, c := range map[string]*client{"担当外": f.member2, "閲覧者": f.viewer} {
		if status, _ := c.do("PUT", f.valuesPath(f.budget, f.manualAct)+"/amounts", map[string]any{"reason": "r", "amounts": values}); status != http.StatusForbidden {
			t.Errorf("%s の金額入力: status = %d, want 403", name, status)
		}
	}
	if got := f.viewer.mustGet(f.valuesPath(f.budget, f.manualAct))["editable"]; got != false {
		t.Errorf("閲覧者の editable = %v, want false", got)
	}

	// ロック済み・実績シナリオは編集できない
	actual := f.admin.mustCreate("/api/scenarios", map[string]any{"name": "2026年度 実績", "scenario_kind": "actual", "fiscal_year": 2026})
	f.admin.do("POST", fmt.Sprintf("/api/scenarios/%d/lock", f.budget), nil)
	for name, sid := range map[string]int64{"ロック済み": f.budget, "実績": actual} {
		if status, _ := f.member.do("PUT", f.valuesPath(sid, f.manualAct)+"/amounts", map[string]any{"reason": "r", "amounts": values}); status != http.StatusConflict {
			t.Errorf("%s シナリオへの入力: status = %d, want 409", name, status)
		}
		if got := f.member.mustGet(f.valuesPath(sid, f.manualAct))["editable"]; got != false {
			t.Errorf("%s シナリオの editable = %v, want false", name, got)
		}
	}
}

func TestScenarioCondition(t *testing.T) {
	f := newScenarioFixture(t)
	path := f.valuesPath(f.budget, f.manualAct) + "/condition"

	status, body := f.member.do("PUT", path, map[string]any{"description": "楽観: 追加発注2件を見込む"})
	if status != http.StatusOK || body["condition"] != "楽観: 追加発注2件を見込む" {
		t.Errorf("登録: status = %d, body = %v", status, body)
	}
	status, body = f.member.do("PUT", path, map[string]any{"description": ""})
	if status != http.StatusOK || body["condition"] != nil {
		t.Errorf("削除: status = %d, condition = %v", status, body["condition"])
	}
}

// numString は JSON の数値を指数表記にせず文字列にする。
func numString(v any) string {
	if f, ok := v.(float64); ok {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	return fmt.Sprint(v)
}

func TestAmountLines(t *testing.T) {
	f := newScenarioFixture(t)
	base := fmt.Sprintf("/api/activities/%d", f.formulaAct)
	vpath := f.valuesPath(f.budget, f.formulaAct)
	lpath := fmt.Sprintf("%s/lines/%d", base, f.salesLine)
	setupLine := f.member.mustCreate(base+"/lines", map[string]any{"subject_id": f.sales, "name": "初期導入費"})

	// 同じ科目に、計算式の内訳・直接入力の内訳・科目への直接入力が並ぶ
	f.member.do("PUT", vpath+"/driver-values", map[string]any{"reason": "r", "values": []map[string]any{
		{"driver_id": f.priceID, "target_month": "2026-10", "value": 1000},
		{"driver_id": f.volumeID, "target_month": "2026-10", "value": 3},
	}})
	status, body := f.member.do("PUT", vpath+"/amounts", map[string]any{"reason": "r", "amounts": []map[string]any{
		{"subject_id": f.sales, "line_id": setupLine, "target_month": "2026-10", "amount": 200000},
		{"subject_id": f.sales, "target_month": "2026-10", "amount": 1000},
	}})
	if status != http.StatusOK {
		t.Fatalf("金額の入力: status = %d, body = %v", status, body)
	}
	for name, got := range map[string]string{
		"1500":   first(lineAmountOf(body, f.salesLine, "2026-10")),
		"200000": first(lineAmountOf(body, setupLine, "2026-10")),
		"1000":   first(amountOf(body, f.sales, "2026-10")),
	} {
		if got != name {
			t.Errorf("金額 = %q, want %s", got, name)
		}
	}
	// 再計算しても、ほかの内訳・直接入力の金額は消えない
	f.member.do("PUT", vpath+"/driver-values", map[string]any{"reason": "r", "values": []map[string]any{{"driver_id": f.volumeID, "target_month": "2026-10", "value": 4}}})
	body = f.member.mustGet(vpath)
	if a, b, c := first(lineAmountOf(body, f.salesLine, "2026-10")), first(lineAmountOf(body, setupLine, "2026-10")), first(amountOf(body, f.sales, "2026-10")); a != "2000" || b != "200000" || c != "1000" {
		t.Errorf("再計算後 = %s / %s / %s, want 2000 / 200000 / 1000", a, b, c)
	}

	// 反映をやめると金額は残り、直接入力できるようになる
	if status, body := f.member.do("PUT", lpath, map[string]any{"name": "利用料", "expression": "unit_price * volume * probability", "formula_enabled": false, "reason": "値引き交渉中のため手入力"}); status != http.StatusOK {
		t.Fatalf("反映をやめる: status = %d, body = %v", status, body)
	}
	status, body = f.member.do("PUT", vpath+"/amounts", map[string]any{"reason": "値引き", "amounts": []map[string]any{{"subject_id": f.sales, "line_id": f.salesLine, "target_month": "2026-10", "amount": 1800}}})
	if got, cell := lineAmountOf(body, f.salesLine, "2026-10"); status != http.StatusOK || got != "1800" || cell["source"] != "manual" {
		t.Errorf("反映しない内訳への入力: status = %d, cell = %v", status, cell)
	}
	// 反映に戻すと再計算される
	if status, body := f.member.do("PUT", lpath, map[string]any{"name": "利用料", "expression": "unit_price * volume * probability", "formula_enabled": true, "reason": "式に戻す"}); status != http.StatusOK {
		t.Fatalf("反映に戻す: status = %d, body = %v", status, body)
	}
	if got, cell := lineAmountOf(f.member.mustGet(vpath), f.salesLine, "2026-10"); got != "2000" || cell["source"] != "formula" {
		t.Errorf("反映に戻した後 = %v, want 2000・formula", cell)
	}

	// 複製したシナリオにも内訳の金額が引き継がれる
	forecast := f.admin.mustCreate("/api/scenarios", map[string]any{"name": "見込", "scenario_kind": "forecast", "fiscal_year": 2026, "base_scenario_id": f.budget})
	body = f.viewer.mustGet(f.valuesPath(forecast, f.formulaAct))
	if a, b := first(lineAmountOf(body, f.salesLine, "2026-10")), first(lineAmountOf(body, setupLine, "2026-10")); a != "2000" || b != "200000" {
		t.Errorf("複製後 = %s / %s, want 2000 / 200000", a, b)
	}

	// ロック済みシナリオに金額がある内訳は削除できない。ロックがなければ金額ごと削除できる
	f.admin.do("POST", fmt.Sprintf("/api/scenarios/%d/lock", f.budget), nil)
	if status, _ := f.member.do("DELETE", fmt.Sprintf("%s/lines/%d", base, setupLine), map[string]any{"reason": "r"}); status != http.StatusConflict {
		t.Errorf("ロック済みの金額がある内訳の削除: status = %d, want 409", status)
	}
	f.admin.do("POST", fmt.Sprintf("/api/scenarios/%d/unlock", f.budget), map[string]any{"reason": "r"})
	if status, body := f.member.do("DELETE", fmt.Sprintf("%s/lines/%d", base, setupLine), map[string]any{"reason": "内訳の整理"}); status != http.StatusNoContent {
		t.Fatalf("内訳の削除: status = %d, body = %v", status, body)
	}
	if got := first(amountOf(f.viewer.mustGet(f.valuesPath(forecast, f.formulaAct)), f.sales, "2026-10")); got != "1000" {
		t.Errorf("内訳削除後の科目への直接入力 = %q, want 1000", got)
	}
	var n int
	if err := f.env.QueryRow("SELECT COUNT(*) FROM budget_facts WHERE line_id = ?", setupLine).Scan(&n); err != nil || n != 0 {
		t.Errorf("削除した内訳の金額が %d 件残っている (err = %v)", n, err)
	}
}

func first(s string, _ map[string]any) string { return s }
