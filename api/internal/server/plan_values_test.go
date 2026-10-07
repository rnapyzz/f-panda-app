package server_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func rowMessages(body map[string]any) string {
	var out []string
	for _, r := range body["error"].(map[string]any)["rows"].([]any) {
		row := r.(map[string]any)
		out = append(out, fmt.Sprintf("%v:%v", row["line"], row["message"]))
	}
	return strings.Join(out, "\n")
}

// TestPlanValuesCSV は計画値の CSV（docs/plan.md「6.3」）の出力と取込を確かめる。
func TestPlanValuesCSV(t *testing.T) {
	f := newScenarioFixture(t)
	a := f.admin
	importPath := fmt.Sprintf("/api/scenarios/%d/plan-values/import", f.budget)
	amountsPath := fmt.Sprintf("/api/scenarios/%d/amounts/export", f.budget)
	driversPath := fmt.Sprintf("/api/scenarios/%d/driver-values/export", f.budget)
	a.mustCreate(fmt.Sprintf("/api/activities/%d/lines", f.manualAct), map[string]any{"subject_id": f.sales, "name": "基本", "reason": "r"})
	a.mustCreate("/api/activities", activityBody(f.fn1, "OTHER-1", nil)) // 担当者なし（member は編集できない）

	// 確認（dry run）は保存しない
	csv := "activity_code,subject_code,line_name,2026-04,2026-05\nPRJ-1,4110,,1000,2000\nPRJ-1,4110,基本,500,\nPRJ-1,8110,,300,0\n"
	status, body := a.upload(importPath+"?dry_run=true", csv, "")
	if status != http.StatusOK || counts(body) != "inserted=5 updated=0 deleted=0 unchanged=0" || body["kind"] != "amounts" {
		t.Fatalf("確認: status = %d, body = %v", status, body)
	}
	if acts := body["activities"].([]any); len(acts) != 1 || acts[0].(map[string]any)["changed"] != float64(5) {
		t.Errorf("施策ごとの件数 = %v", acts)
	}
	if status, _ := a.upload(importPath, csv, ""); status != http.StatusUnprocessableEntity {
		t.Errorf("理由なし: status = %d", status)
	}
	if status, body = a.upload(importPath, csv, "期初計画の入力"); status != http.StatusOK || counts(body) != "inserted=5 updated=0 deleted=0 unchanged=0" {
		t.Fatalf("取込: status = %d, body = %v", status, body)
	}

	// 出力（月を横に並べる）と、出力した CSV の取込は「変更なし」
	exported := a.download(amountsPath)
	for _, want := range []string{
		"activity_code,activity_name,subject_code,subject_name,line_name,line_type,2026-04,2026-05,2026-06",
		"PRJ-1,施策 PRJ-1,4110,受託売上,,manual,1000,2000,,",
		"PRJ-1,施策 PRJ-1,4110,受託売上,基本,manual,500,,",
		"PRJ-1,施策 PRJ-1,8110,外注費,,manual,300,0,",
		"SAAS-1,施策 SAAS-1,4110,受託売上,利用料,formula,,",
	} {
		if !strings.Contains(exported, want) {
			t.Errorf("金額の出力に %q がない:\n%s", want, exported)
		}
	}
	if status, body = a.upload(importPath, exported, "再取込"); status != http.StatusOK || counts(body) != "inserted=0 updated=0 deleted=0 unchanged=5" || len(body["warnings"].([]any)) != 0 {
		t.Errorf("出力した CSV の取込: status = %d, body = %v", status, body)
	}

	// ドライバー値の取込で、計算式の金額を計算し直す
	csv = "activity_code,driver_code,2026-04\nSAAS-1,unit_price,100\nSAAS-1,volume,10.5\n"
	if status, body = a.upload(importPath, csv, "ドライバー"); status != http.StatusOK || body["kind"] != "driver_values" || counts(body) != "inserted=2 updated=0 deleted=0 unchanged=0" {
		t.Fatalf("ドライバー値: status = %d, body = %v", status, body)
	}
	if exported = a.download(amountsPath); !strings.Contains(exported, "SAAS-1,施策 SAAS-1,4110,受託売上,利用料,formula,525,") {
		t.Errorf("再計算の結果:\n%s", exported)
	}
	drivers := a.download(driversPath)
	if !strings.Contains(drivers, "SAAS-1,施策 SAAS-1,volume,件数,,10.5,") {
		t.Errorf("ドライバー値の出力:\n%s", drivers)
	}
	if status, body = a.upload(importPath, drivers, "再取込"); status != http.StatusOK || counts(body) != "inserted=0 updated=0 deleted=0 unchanged=2" {
		t.Errorf("ドライバー値の再取込: %v", body)
	}

	// 「-」で消す。計算式の内訳の値は取り込まず、注意を返す
	csv = "activity_code,subject_code,line_name,2026-04,2026-05\nPRJ-1,4110,,1500,-\nSAAS-1,4110,利用料,999,\n"
	if status, body = a.upload(importPath, csv, "見直し"); status != http.StatusOK || counts(body) != "inserted=0 updated=1 deleted=1 unchanged=0" {
		t.Fatalf("更新・削除: status = %d, body = %v", status, body)
	}
	if w := fmt.Sprint(body["warnings"]); w != "[map[count:1 kind:formula_line]]" {
		t.Errorf("注意 = %s", w)
	}
	if exported = a.download(amountsPath); !strings.Contains(exported, "PRJ-1,施策 PRJ-1,4110,受託売上,,manual,1500,,") {
		t.Errorf("更新・削除の結果:\n%s", exported)
	}

	// エラーが1件でもあれば何も取り込まない
	csv = "activity_code,subject_code,line_name,2026-04\nPRJ-1,4110,,1\nNONE,4110,,1\nPRJ-1,4110,なし,1\nPRJ-1,8110,,1.5\nPRJ-1,4110,,2\n"
	status, body = a.upload(importPath, csv, "誤り")
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("エラー: status = %d, body = %v", status, body)
	}
	msgs := rowMessages(body)
	for _, want := range []string{"3:施策コード「NONE」", "4:施策「PRJ-1」の科目「4110」に内訳「なし」", "5:2026-04 列: 金額は円単位の整数", "6:2 行目と同じ"} {
		if !strings.Contains(msgs, want) {
			t.Errorf("エラーに %q がない:\n%s", want, msgs)
		}
	}
	if status, _ := a.upload(importPath, "activity_code,subject_code,line_name,2025-04\nPRJ-1,4110,,1\n", "年度違い"); status != http.StatusBadRequest {
		t.Errorf("年度の違う月の列: status = %d", status)
	}

	// 現場担当: 権限のない施策の行は、値が変わるときだけエラー
	m := f.member
	a.upload(importPath, "activity_code,subject_code,line_name,2026-06\nOTHER-1,4110,,5\n", "他の施策")
	csv = "activity_code,subject_code,line_name,2026-06\nPRJ-1,4110,,700\nOTHER-1,4110,,1\n"
	if status, body = m.upload(importPath, csv, "担当分"); status != http.StatusUnprocessableEntity || !strings.Contains(rowMessages(body), "3:施策「OTHER-1」の数値を入力する権限がありません") {
		t.Errorf("権限のない施策: status = %d, body = %v", status, body)
	}
	if status, body = m.upload(importPath, a.download(amountsPath), "全施策の再取込"); status != http.StatusOK || body["inserted"] != float64(0) {
		t.Errorf("変更のない全施策の CSV: status = %d, body = %v", status, body)
	}
	if status, body = m.upload(importPath, "activity_code,subject_code,line_name,2026-06\nPRJ-1,4110,,700\n", "担当分"); status != http.StatusOK || body["inserted"] != float64(1) {
		t.Errorf("担当の施策: status = %d, body = %v", status, body)
	}

	// 実績の月の値は取り込まず、注意を返す
	a.do("PUT", fmt.Sprintf("/api/scenarios/%d", f.budget), map[string]any{"name": "2026年度 当初予算", "plan_role": "initial", "actual_through": "2026-04", "reason": "4月確定"})
	if status, body = a.upload(importPath, "activity_code,subject_code,line_name,2026-04,2026-05\nPRJ-1,4110,,1,2\n", "実績の月"); status != http.StatusOK || counts(body) != "inserted=1 updated=0 deleted=0 unchanged=0" ||
		fmt.Sprint(body["warnings"]) != "[map[count:1 kind:actual_month]]" {
		t.Errorf("実績の月: status = %d, body = %v", status, body)
	}

	// 作成中ではないシナリオは FP&A のみ。ロック済みには取り込めない
	other := a.mustCreate("/api/scenarios", map[string]any{"name": "比較用", "fiscal_year": 2026})
	otherPath := fmt.Sprintf("/api/scenarios/%d/plan-values/import", other)
	if status, _ := m.upload(otherPath, "activity_code,subject_code,line_name,2026-06\nPRJ-1,4110,,1\n", "r"); status != http.StatusConflict {
		t.Errorf("作成中ではないシナリオ（現場）: status = %d", status)
	}
	a.do("POST", fmt.Sprintf("/api/scenarios/%d/lock", other), map[string]any{"reason": "確定"})
	if status, _ := a.upload(otherPath, "activity_code,subject_code,line_name,2026-06\nPRJ-1,4110,,1\n", "r"); status != http.StatusConflict {
		t.Errorf("ロック済み: status = %d", status)
	}
}
