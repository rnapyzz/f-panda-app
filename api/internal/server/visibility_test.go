package server_test

import (
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"testing"
)

// TestRestrictedSubjects は閲覧制限のある科目（docs/plan.md「2.17」）を確かめる。
func TestRestrictedSubjects(t *testing.T) {
	f := newScenarioFixture(t)
	a, m := f.admin, f.member
	personnel := a.mustCreate("/api/subjects", map[string]any{"code": "8210", "name": "人件費", "category": "expense", "is_restricted": true})
	a.mustCreate("/api/gl-accounts", map[string]any{"code": "8210", "name": "人件費", "subject_id": personnel})
	if got := a.mustGet(fmt.Sprintf("/api/subjects/%d", personnel))["is_restricted"]; got != true {
		t.Fatalf("閲覧制限 = %v", got)
	}

	put := func(c *client, subjectID int64, amount int, reason string) (int, map[string]any) {
		return c.do("PUT", f.valuesPath(f.budget, f.manualAct)+"/amounts", map[string]any{
			"reason": reason, "amounts": []map[string]any{{"subject_id": subjectID, "target_month": "2026-10", "amount": amount}},
		})
	}
	if status, body := put(a, f.sales, 1000, "売上"); status != http.StatusOK {
		t.Fatalf("売上: %d %v", status, body)
	}
	if status, body := put(a, personnel, 300, "人件費の配賦"); status != http.StatusOK {
		t.Fatalf("人件費: %d %v", status, body)
	}

	// 数値入力: 現場には人件費を出さない。入力・内訳の作成もできない
	subjectsOf := func(c *client) (string, any) {
		v := c.mustGet(f.valuesPath(f.budget, f.manualAct))
		var codes []string
		for _, r := range v["amounts"].([]any) {
			codes = append(codes, r.(map[string]any)["code"].(string))
		}
		return strings.Join(codes, ","), v["restricted_hidden"]
	}
	if codes, hidden := subjectsOf(a); codes != "4110,8210" || hidden != false {
		t.Errorf("FP&A の科目 = %s, restricted_hidden = %v", codes, hidden)
	}
	if codes, hidden := subjectsOf(m); codes != "4110" || hidden != true {
		t.Errorf("現場の科目 = %s, restricted_hidden = %v", codes, hidden)
	}
	if codes, _ := subjectsOf(f.viewer); codes != "4110,8210" {
		t.Errorf("経営陣の科目 = %s", codes)
	}
	if status, _ := put(m, personnel, 1, "r"); status != http.StatusForbidden {
		t.Errorf("現場の人件費の入力: status = %d", status)
	}
	if status, _ := m.do("POST", fmt.Sprintf("/api/activities/%d/lines", f.manualAct), map[string]any{"subject_id": personnel, "name": "山田"}); status != http.StatusForbidden {
		t.Errorf("現場の人件費の内訳: status = %d", status)
	}

	// 予実比較・リスク・ホームは、人件費を合計からも除く
	expenseTotal := func(c *client) string {
		body := c.mustGet(fmt.Sprintf("/api/reports/comparison?scenario_ids=%d", f.budget))
		total := new(big.Int)
		for _, r := range body["rows"].([]any) {
			row := r.(map[string]any)
			if id := int64(row["subject_id"].(float64)); id == f.cost || id == personnel {
				v, _ := new(big.Int).SetString(row["values"].(map[string]any)["s1"].(string), 10)
				total.Add(total, v)
			}
		}
		return total.String()
	}
	if got := expenseTotal(a); got != "300" {
		t.Errorf("FP&A の予実比較の費用 = %s", got)
	}
	if got := expenseTotal(m); got != "0" {
		t.Errorf("現場の予実比較の費用 = %s", got)
	}
	riskOf := func(c *client) string {
		return plOf(riskActivity(c.mustGet(fmt.Sprintf("/api/reports/risk?scenario_id=%d", f.budget)), "PRJ-1"), "full")
	}
	if a, m := riskOf(a), riskOf(m); a != "1000/300" || m != "1000/0" {
		t.Errorf("リスクの満額: FP&A = %s, 現場 = %s", a, m)
	}
	homeOf := func(c *client) string {
		for _, it := range c.mustGet(fmt.Sprintf("/api/scenarios/%d/activity-status?scope=all", f.budget))["items"].([]any) {
			if it := it.(map[string]any); it["code"] == "PRJ-1" {
				return fmt.Sprint(it["current"].(map[string]any)["expense"])
			}
		}
		return ""
	}
	if a, m := homeOf(a), homeOf(m); a != "300" || m != "0" {
		t.Errorf("ホームの費用: FP&A = %s, 現場 = %s", a, m)
	}

	// 計画値の CSV: 現場には出さず、取り込めない
	exportPath := fmt.Sprintf("/api/scenarios/%d/amounts/export", f.budget)
	if !strings.Contains(a.download(exportPath), ",8210,人件費,") || strings.Contains(m.download(exportPath), "8210") {
		t.Errorf("計画値の CSV に人件費が出る（現場）")
	}
	status, body := m.upload(fmt.Sprintf("/api/scenarios/%d/plan-values/import", f.budget), "activity_code,subject_code,line_name,2026-11\nPRJ-1,8210,,1\n", "r")
	if status != http.StatusUnprocessableEntity || !strings.Contains(rowMessages(body), "閲覧制限") {
		t.Errorf("現場の人件費の取込: status = %d, body = %v", status, body)
	}

	// 実績の明細: 現場には人件費の明細も合計も出さない
	a.upload(actualsPath, "target_month,box_code,account_code,amount\n2026-04,PRJ-1,4110,700\n2026-04,PRJ-1,8210,500\n", "4月実績")
	entriesPath := fmt.Sprintf("/api/activities/%d/actual-entries?month=2026-04", f.manualAct)
	if e := a.mustGet(entriesPath); e["fact_total"] != "1200" {
		t.Errorf("FP&A の実績の合計 = %v", e["fact_total"])
	}
	if e := m.mustGet(entriesPath); e["fact_total"] != "700" || len(e["items"].([]any)) != 1 || e["restricted_hidden"] != true {
		t.Errorf("現場の実績 = %v", e)
	}

	// 変更履歴: 現場は編集できる施策だけ。人件費の変更は出さない
	if status, _ := m.do("GET", "/api/change-sets", nil); status != http.StatusForbidden {
		t.Errorf("現場の変更履歴（全体）: status = %d", status)
	}
	if status, _ := f.manager2.do("GET", fmt.Sprintf("/api/change-sets?activity_id=%d", f.manualAct), nil); status != http.StatusForbidden {
		t.Errorf("編集できない施策の変更履歴: status = %d", status)
	}
	reasons := func(c *client) (string, map[string]int64) {
		var out []string
		ids := map[string]int64{}
		for _, it := range c.mustGet(fmt.Sprintf("/api/change-sets?activity_id=%d", f.manualAct))["items"].([]any) {
			cs := it.(map[string]any)
			out = append(out, cs["reason"].(string))
			ids[cs["reason"].(string)] = int64(cs["id"].(float64))
		}
		return strings.Join(out, ","), ids
	}
	got, ids := reasons(a)
	if !strings.Contains(got, "人件費の配賦") {
		t.Errorf("FP&A の変更履歴 = %s", got)
	}
	if got, _ := reasons(m); strings.Contains(got, "人件費") || !strings.Contains(got, "売上") {
		t.Errorf("現場の変更履歴 = %s", got)
	}
	detail := fmt.Sprintf("/api/change-sets/%d", ids["人件費の配賦"])
	if status, _ := m.do("GET", detail+fmt.Sprintf("?activity_id=%d", f.manualAct), nil); status != http.StatusNotFound {
		t.Errorf("現場の人件費の変更の詳細: status = %d", status)
	}
	if status, _ := f.viewer.do("GET", detail, nil); status != http.StatusOK {
		t.Errorf("経営陣の変更履歴の詳細: status = %d", status)
	}
	if status, _ := m.do("GET", fmt.Sprintf("/api/change-sets/%d?activity_id=%d", ids["売上"], f.manualAct), nil); status != http.StatusOK {
		t.Errorf("現場の売上の変更の詳細: status = %d", status)
	}

	// 勘定科目の CSV: is_restricted を出力し、列がなければ変えない
	if !strings.Contains(a.download("/api/subjects/export"), "8210,人件費,expense,,0,true") {
		t.Errorf("勘定科目の出力に閲覧制限がない")
	}
	if status, body := a.upload("/api/subjects/import", "code,name,category,parent_code,sort_order\n8210,人件費,expense,,0\n", "r"); status != http.StatusOK || body["unchanged"] != float64(1) {
		t.Errorf("is_restricted の列がない取込: %d %v", status, body)
	}
}
