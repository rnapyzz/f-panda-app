package server_test

import (
	"fmt"
	"net/http"
	"testing"
)

// wipOf はレスポンスから仕掛品振替（科目への直接入力）の金額を返す。
func wipOf(body map[string]any, wipSubject int64, month string) string {
	got, _ := amountOf(body, wipSubject, month)
	return got
}

// TestProjectWip は、検収月に一括の施策の仕掛品振替（docs/plan.md「2.28」）を確かめる。
func TestProjectWip(t *testing.T) {
	f := newScenarioFixture(t)
	a := f.admin
	project := activityBody(f.fn1, "PJ-WIP", map[string]any{
		"owner_user_id": f.memberID, "activity_type": "project", "start_date": "2026-04-01", "end_date": "2026-10-31",
		"recognition": "on_delivery",
	})

	// 仕掛品振替の科目がないと、検収月に一括にはできない
	if status, body := a.do("POST", "/api/activities", project); status != http.StatusUnprocessableEntity || detail(body, "recognition") == "" {
		t.Fatalf("振替の科目なし: %d %v", status, body)
	}

	// 科目の設定: 人件費・外注費を原価の科目に、仕掛品振替の科目を1つ指定する
	labor := a.mustCreate("/api/subjects", map[string]any{"code": "8100", "name": "人件費", "category": "expense", "is_project_cost": true})
	wip := a.mustCreate("/api/subjects", map[string]any{"code": "8900", "name": "仕掛品振替", "category": "expense", "role": "wip_transfer"})
	if status, body := a.do("PUT", fmt.Sprintf("/api/subjects/%d", f.cost), map[string]any{"code": "8110", "name": "外注費", "category": "expense", "is_project_cost": true}); status != http.StatusOK || body["is_project_cost"] != true || body["role"] != "normal" {
		t.Fatalf("外注費を原価の科目に: %d %v", status, body)
	}
	for name, tc := range map[string]struct {
		body  map[string]any
		field string
	}{
		"振替の科目は1つ": {map[string]any{"code": "8901", "name": "x", "category": "expense", "role": "wip_transfer"}, "role"},
		"振替の科目は費用": {map[string]any{"code": "4901", "name": "x", "category": "revenue", "role": "wip_transfer"}, "role"},
		"原価の科目は費用": {map[string]any{"code": "4902", "name": "x", "category": "revenue", "is_project_cost": true}, "is_project_cost"},
		"役割の値が不正":  {map[string]any{"code": "8902", "name": "x", "category": "expense", "role": "other"}, "role"},
	} {
		if status, body := a.do("POST", "/api/subjects", tc.body); status != http.StatusUnprocessableEntity || detail(body, tc.field) == "" {
			t.Errorf("%s: %d %v", name, status, body)
		}
	}

	pj := a.mustCreate("/api/activities", project)
	if got := a.mustGet(fmt.Sprintf("/api/activities/%d", pj))["recognition"]; got != "on_delivery" {
		t.Errorf("計上の仕方 = %v", got)
	}
	path := f.valuesPath(f.budget, pj)

	// 4〜10月に人件費 100 万・外注費 50 万、9月に売上 1,200 万
	var amounts []map[string]any
	for _, m := range []string{"2026-04", "2026-05", "2026-06", "2026-07", "2026-08", "2026-09", "2026-10"} {
		amounts = append(amounts,
			map[string]any{"subject_id": labor, "target_month": m, "amount": 1000000},
			map[string]any{"subject_id": f.cost, "target_month": m, "amount": 500000})
	}
	amounts = append(amounts, map[string]any{"subject_id": f.sales, "target_month": "2026-09", "amount": 12000000})
	status, body := f.member.do("PUT", path+"/amounts", map[string]any{"reason": "r", "amounts": amounts})
	if status != http.StatusOK {
		t.Fatalf("金額: %d %v", status, body)
	}
	// 計上月が未設定なら振替は作らない
	if got := wipOf(body, wip, "2026-04"); got != "" {
		t.Errorf("計上月なしの振替 = %q", got)
	}
	w := body["wip"].(map[string]any)
	if w["recognition_month"] != nil || int64(w["transfer_subject_id"].(float64)) != wip || len(w["cost_subject_ids"].([]any)) != 2 {
		t.Errorf("仕掛の情報 = %v", w)
	}

	// 計上月を9月にすると、4〜8月は ▲150 万、9月は 750 万を戻す。10月は振替なし
	recognize := func(c *client, month any) (int, map[string]any) {
		return c.do("PUT", path+"/recognition", map[string]any{"recognition_month": month})
	}
	if status, body = recognize(f.member, "2026-09"); status != http.StatusOK {
		t.Fatalf("計上月: %d %v", status, body)
	}
	for m, want := range map[string]string{"2026-04": "-1500000", "2026-08": "-1500000", "2026-09": "7500000", "2026-10": ""} {
		if got := wipOf(body, wip, m); got != want {
			t.Errorf("計上月9月の %s の振替 = %q, want %q", m, got, want)
		}
	}
	if _, cell := amountOf(body, wip, "2026-09"); cell["source"] != "wip" {
		t.Errorf("振替の source = %v", cell["source"])
	}
	if w := body["wip"].(map[string]any); w["recognition_month"] != "2026-09" || numString(w["opening"]) != "0" {
		t.Errorf("仕掛の情報 = %v", w)
	}
	var logs int
	if err := f.env.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE table_name = 'budget_facts' AND JSON_UNQUOTE(after_json->'$.source') = 'wip'`).Scan(&logs); err != nil || logs != 6 {
		t.Errorf("振替の監査ログ = %d (%v), want 6", logs, err)
	}

	// 振替の科目は直接入力できない。計上月は入力できる月の範囲だけ
	if status, body := f.member.do("PUT", path+"/amounts", map[string]any{"reason": "r", "amounts": []map[string]any{{"subject_id": wip, "target_month": "2026-05", "amount": 1}}}); status != http.StatusUnprocessableEntity {
		t.Errorf("振替の直接入力: %d %v", status, body)
	}
	if status, body := recognize(f.member, "2028-01"); status != http.StatusUnprocessableEntity || detail(body, "recognition_month") == "" {
		t.Errorf("範囲外の計上月: %d %v", status, body)
	}

	// 計上月を10月にずらす・原価を変えると計算し直す
	if status, body = recognize(f.member, "2026-10"); status != http.StatusOK {
		t.Fatalf("計上月を10月に: %d %v", status, body)
	}
	if got := wipOf(body, wip, "2026-09"); got != "-1500000" {
		t.Errorf("10月計上の9月の振替 = %q", got)
	}
	if got := wipOf(body, wip, "2026-10"); got != "9000000" {
		t.Errorf("10月計上の10月の振替 = %q, want 9000000", got)
	}
	status, body = f.member.do("PUT", path+"/amounts", map[string]any{"reason": "r", "amounts": []map[string]any{{"subject_id": labor, "target_month": "2026-05", "amount": 2000000}}})
	if status != http.StatusOK || wipOf(body, wip, "2026-05") != "-2500000" || wipOf(body, wip, "2026-10") != "10000000" {
		t.Errorf("原価を変えた後の振替: 5月 %q 10月 %q", wipOf(body, wip, "2026-05"), wipOf(body, wip, "2026-10"))
	}

	// 実績の月: 会計の仕掛品振替を取り込み、4月を実績の月にする。計上月の戻しは実績の振替を含めた残高
	a.mustCreate("/api/gl-accounts", map[string]any{"code": "8100", "name": "人件費", "subject_id": labor})
	a.mustCreate("/api/gl-accounts", map[string]any{"code": "8900", "name": "期末仕掛品", "subject_id": wip})
	csv := "target_month,account_code,box_code,amount\n" +
		"2026-04,8100,PJ-WIP,1000000\n2026-04,8110,PJ-WIP,600000\n2026-04,8900,PJ-WIP,-1600000\n"
	if status, body := a.upload(actualsPath, csv, "4月実績"); status != http.StatusOK {
		t.Fatalf("実績取込: %d %v", status, body)
	}
	if status, body := a.do("PUT", fmt.Sprintf("/api/scenarios/%d", f.budget), map[string]any{"name": "2026年度 当初予算", "plan_role": "initial", "actual_through": "2026-04", "reason": "4月決算確定"}); status != http.StatusOK {
		t.Fatalf("決算確定月: %d %v", status, body)
	}
	body = a.mustGet(path)
	if got := wipOf(body, wip, "2026-04"); got != "-1600000" {
		t.Errorf("4月（実績）の振替 = %q", got)
	}
	// 160 万（実績）+ 250 万（5月）+ 150 万 × 4（6〜9月）
	if got := wipOf(body, wip, "2026-10"); got != "10100000" {
		t.Errorf("実績を含めた10月の振替 = %q, want 10100000", got)
	}
	// 実績を取り込み直すと、計上月の戻しも計算し直す
	csv = "target_month,account_code,box_code,amount\n" +
		"2026-04,8100,PJ-WIP,1100000\n2026-04,8110,PJ-WIP,600000\n2026-04,8900,PJ-WIP,-1700000\n"
	if status, body := a.upload(actualsPath, csv, "4月実績の修正"); status != http.StatusOK {
		t.Fatalf("実績の取り込み直し: %d %v", status, body)
	}
	if got := wipOf(a.mustGet(path), wip, "2026-10"); got != "10200000" {
		t.Errorf("実績の修正後の10月の振替 = %q, want 10200000", got)
	}

	// 年度をまたぐ案件: 1月着手・6月検収。翌期の計画は、今回の見込の1〜3月の仕掛を引き継いで戻す
	span := a.mustCreate("/api/activities", activityBody(f.fn1, "PJ-SPAN", map[string]any{
		"owner_user_id": f.memberID, "activity_type": "project", "start_date": "2027-01-01", "end_date": "2027-06-30",
		"recognition": "on_delivery",
	}))
	spanPath := f.valuesPath(f.budget, span)
	amounts = nil
	for _, m := range []string{"2027-01", "2027-02", "2027-03", "2027-04", "2027-05", "2027-06"} {
		amounts = append(amounts, map[string]any{"subject_id": labor, "target_month": m, "amount": 100})
	}
	if status, body := f.member.do("PUT", spanPath+"/amounts", map[string]any{"reason": "r", "amounts": amounts}); status != http.StatusOK {
		t.Fatalf("年度をまたぐ案件の金額: %d %v", status, body)
	}
	status, body = f.member.do("PUT", spanPath+"/recognition", map[string]any{"recognition_month": "2027-06"})
	if status != http.StatusOK || wipOf(body, wip, "2027-03") != "-100" || wipOf(body, wip, "2027-06") != "500" {
		t.Fatalf("今回の見込の振替: %d 3月 %q 6月 %q", status, wipOf(body, wip, "2027-03"), wipOf(body, wip, "2027-06"))
	}
	status, body = a.do("POST", "/api/scenarios/start-fiscal-year", map[string]any{"fiscal_year": 2027, "name": "2027年度 期初計画", "source": "current"})
	if status != http.StatusCreated {
		t.Fatalf("新年度: %d %v", status, body)
	}
	plan := int64(body["scenario"].(map[string]any)["id"].(float64))
	planView := a.mustGet(f.valuesPath(plan, span))
	w = planView["wip"].(map[string]any)
	if w["recognition_month"] != "2027-06" || numString(w["opening"]) != "300" {
		t.Errorf("翌期の計画の仕掛の情報 = %v", w)
	}
	if got := wipOf(planView, wip, "2027-06"); got != "500" {
		t.Errorf("翌期の計画の6月の振替 = %q, want 500", got)
	}

	// 計上の仕方を「発生した月に計上」に戻すと、計画値の月の振替を消す。計上月は検収月に一括の施策だけ
	update := activityBody(f.fn1, "PJ-WIP", map[string]any{
		"owner_user_id": f.memberID, "activity_type": "project", "start_date": "2026-04-01", "end_date": "2026-10-31",
		"recognition": "as_incurred",
	})
	if status, body := a.do("PUT", fmt.Sprintf("/api/activities/%d", pj), update); status != http.StatusUnprocessableEntity || detail(body, "reason") == "" {
		t.Errorf("理由なしで計上の仕方を変更: %d %v", status, body)
	}
	update["reason"] = "検収基準をやめる"
	if status, body := a.do("PUT", fmt.Sprintf("/api/activities/%d", pj), update); status != http.StatusOK {
		t.Fatalf("計上の仕方を戻す: %d %v", status, body)
	}
	body = a.mustGet(path)
	if body["wip"] != nil || wipOf(body, wip, "2026-10") != "" || wipOf(body, wip, "2026-04") != "-1700000" {
		t.Errorf("戻した後: wip %v 10月 %q 4月（実績）%q", body["wip"], wipOf(body, wip, "2026-10"), wipOf(body, wip, "2026-04"))
	}
	if status, body := recognize(f.member, "2026-09"); status != http.StatusUnprocessableEntity {
		t.Errorf("発生した月に計上の施策の計上月: %d %v", status, body)
	}

	// 振替の科目の指定を外すと、残っている振替を消す
	if status, body := a.do("PUT", fmt.Sprintf("/api/subjects/%d", wip), map[string]any{"code": "8900", "name": "仕掛品振替", "category": "expense", "role": "normal"}); status != http.StatusOK {
		t.Fatalf("振替の科目を外す: %d %v", status, body)
	}
	if got := wipOf(a.mustGet(spanPath), wip, "2027-06"); got != "" {
		t.Errorf("指定を外した後の振替 = %q", got)
	}
}
