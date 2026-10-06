package server_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
)

// upload は CSV を multipart/form-data で送る。
func (c *client) upload(path, csv, reason string) (int, map[string]any) {
	c.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if reason != "" {
		mw.WriteField("reason", reason)
	}
	if csv != "" {
		fw, _ := mw.CreateFormFile("file", "actuals.csv")
		io.WriteString(fw, csv)
	}
	mw.Close()

	req, _ := http.NewRequest("POST", c.base+path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	res, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

func counts(body map[string]any) string {
	return fmt.Sprintf("inserted=%v updated=%v deleted=%v unchanged=%v", body["inserted"], body["updated"], body["deleted"], body["unchanged"])
}

func allocation(body map[string]any) string {
	a := body["allocation"].(map[string]any)
	return fmt.Sprintf("activity_code=%v external_code=%v rule=%v unallocated=%v", a["activity_code"], a["external_code"], a["rule"], a["unallocated"])
}

// actualAmount は実績データの金額を返す（なければ空文字）。activityID の 0 は未割当。
func (f *scenarioFixture) actualAmount(activityID, subjectID int64, month string) string {
	f.admin.t.Helper()
	var amount string
	err := f.env.QueryRow("SELECT CAST(amount AS CHAR) FROM actual_facts WHERE activity_key = ? AND subject_id = ? AND target_month = ?",
		activityID, subjectID, month+"-01").Scan(&amount)
	if errors.Is(err, sql.ErrNoRows) {
		return ""
	}
	if err != nil {
		f.admin.t.Fatal(err)
	}
	return amount
}

const actualsPath = "/api/actuals/import"

func TestImportActuals(t *testing.T) {
	f := newScenarioFixture(t)

	csv := "target_month,account_code,department_code,box_code,amount,description\n" +
		"2026-09,4110,D100,PRJ-1,1200000,A社 受託\n" +
		"2026-09,8110,D100,PRJ-1,-350000,外注 戻し\n" +
		"2026-09,8110,D100,PRJ-1,-50000,\n" + // 同じ施策・科目は合算 → -400000
		"2026-10,4110,,SAAS-1,300000,\n"

	// dry_run は保存せずに件数を返す（理由は不要）
	status, body := f.admin.upload(actualsPath+"?dry_run=true", csv, "")
	if status != http.StatusOK || body["dry_run"] != true || counts(body) != "inserted=3 updated=0 deleted=0 unchanged=0" {
		t.Fatalf("dry_run: status = %d, body = %v", status, body)
	}
	if got := f.actualAmount(f.manualAct, f.sales, "2026-09"); got != "" {
		t.Fatalf("dry_run なのに保存された: %q", got)
	}

	// 取込
	status, body = f.admin.upload(actualsPath, csv, "2026-09・10 実績取込")
	if status != http.StatusOK || counts(body) != "inserted=3 updated=0 deleted=0 unchanged=0" || body["rows"] != float64(4) || body["facts"] != float64(3) {
		t.Fatalf("取込: status = %d, body = %v", status, body)
	}
	if got := allocation(body); got != "activity_code=4 external_code=0 rule=0 unallocated=0" {
		t.Errorf("割当の根拠 = %s", got)
	}
	totals := body["totals"].([]any)
	if sep := totals[0].(map[string]any); sep["month"] != "2026-09" || sep["revenue"] != "1200000" || sep["expense"] != "-400000" || sep["unallocated_revenue"] != "0" {
		t.Errorf("2026-09 の合計 = %v", sep)
	}
	if got := f.actualAmount(f.manualAct, f.cost, "2026-09"); got != "-400000" {
		t.Errorf("取り込んだ外注費 = %q", got)
	}
	months := f.viewer.mustGet("/api/actuals/months?fiscal_year=2026")["months"].([]any)
	if len(months) != 2 || months[1] != "2026-10" {
		t.Errorf("取込済みの月 = %v", months)
	}

	// 同じファイルの再取込では合計は何も変わらない（監査ログも増えない）
	status, body = f.admin.upload(actualsPath, csv, "再取込")
	if status != http.StatusOK || counts(body) != "inserted=0 updated=0 deleted=0 unchanged=3" {
		t.Errorf("同じファイルの再取込: status = %d, body = %v", status, body)
	}

	// 2026-09 だけを取り込み直すと、2026-09 は CSV の内容に置き換わり、2026-10 はそのまま。列は省略できる
	status, body = f.admin.upload(actualsPath, "target_month,account_code,box_code,amount\n2026-09,4110,PRJ-1,1250000\n", "2026-09 実績修正")
	if status != http.StatusOK || counts(body) != "inserted=0 updated=1 deleted=1 unchanged=0" {
		t.Errorf("2026-09 の再取込: status = %d, body = %v", status, body)
	}
	if got := f.actualAmount(f.manualAct, f.cost, "2026-09"); got != "" {
		t.Errorf("CSV にない 2026-09 の外注費が残っている: %q", got)
	}
	if got := f.actualAmount(f.formulaAct, f.sales, "2026-10"); got != "300000" {
		t.Errorf("2026-10 が変わった: %q", got)
	}
	var entries int
	f.env.QueryRow("SELECT COUNT(*) FROM actual_entries WHERE target_month = '2026-09-01'").Scan(&entries)
	if entries != 1 {
		t.Errorf("2026-09 の明細 = %d 行, want 1", entries)
	}

	// 取込は変更セット（理由付き、シナリオなし）と監査ログ（合計の変更）に残る
	var n int
	if err := f.env.QueryRow(`
		SELECT COUNT(*) FROM audit_logs a JOIN change_sets c ON c.id = a.change_set_id
		WHERE c.scenario_id IS NULL AND c.reason = '2026-09 実績修正' AND a.table_name = 'actual_facts'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("監査ログ = %d件, want 2（更新1・削除1）", n)
	}
}

func TestImportActualsRejectsInvalidData(t *testing.T) {
	f := newScenarioFixture(t)

	// 1行でもエラーがあれば全件取り込まない
	csv := "target_month,account_code,box_code,amount\n" +
		"2026-09,4110,PRJ-1,1200000\n" +
		"2026-13,4110,PRJ-1,1\n"
	status, body := f.admin.upload(actualsPath, csv, "取込")
	if status != http.StatusUnprocessableEntity || errorCode(body) != "invalid_csv" {
		t.Fatalf("status = %d, body = %v", status, body)
	}
	if got := f.actualAmount(f.manualAct, f.sales, "2026-09"); got != "" {
		t.Errorf("エラーがあるのに一部が保存された: %q", got)
	}

	// 未登録の会計科目はコードごとにまとめて返す。登録されていない箱の ID はエラーにしない
	csv = "target_month,account_code,box_code,amount\n" +
		"2026-09,9999,NOPE,1\n" +
		"2026-09,9999,,2\n" +
		"2026-09,4110,NOPE,3\n"
	status, body = f.admin.upload(actualsPath, csv, "取込")
	rows := body["error"].(map[string]any)["rows"].([]any)
	if status != http.StatusUnprocessableEntity || len(rows) != 1 || !strings.Contains(rows[0].(map[string]any)["message"].(string), "9999") {
		t.Errorf("未登録の会計科目: status = %d, body = %v", status, body)
	}

	valid := "target_month,account_code,amount\n2026-09,4110,1\n"
	if status, body := f.admin.upload(actualsPath, valid, ""); status != http.StatusUnprocessableEntity || detail(body, "reason") == "" {
		t.Errorf("理由なし: status = %d, body = %v", status, body)
	}
	if status, body := f.admin.upload(actualsPath, "", "取込"); status != http.StatusUnprocessableEntity || detail(body, "file") == "" {
		t.Errorf("ファイルなし: status = %d, body = %v", status, body)
	}
	if status, _ := f.manager1.upload(actualsPath, valid, "取込"); status != http.StatusForbidden {
		t.Errorf("マネージャーの取込: status = %d, want 403", status)
	}
}

// TestActualAllocation は docs/plan.md「2.12 実績の割当」の流れを確かめる。
func TestActualAllocation(t *testing.T) {
	f := newScenarioFixture(t)
	a := f.admin

	// 受け皿の施策と、会計科目（減価償却費・給与・対象外の預金）
	pool := a.mustCreate("/api/activities", activityBody(f.fn1, "POOL-1", map[string]any{"activity_type": "cost_pool"}))
	depreciation := a.mustCreate("/api/gl-accounts", map[string]any{"code": "6210", "name": "減価償却費", "subject_id": f.cost})
	salary := a.mustCreate("/api/gl-accounts", map[string]any{"code": "6110", "name": "給与", "subject_id": f.cost, "hide_details": true})
	a.mustCreate("/api/gl-accounts", map[string]any{"code": "1110", "name": "普通預金", "is_excluded": true})
	if status, body := a.do("POST", "/api/gl-accounts", map[string]any{"code": "1120", "name": "当座預金"}); status != http.StatusUnprocessableEntity || detail(body, "subject_id") == "" {
		t.Errorf("科目なしの会計科目: status = %d, body = %v", status, body)
	}
	// 割当ルール: 給与は全部門で受け皿へ。減価償却費は D200 だけ SAAS-1 へ
	a.mustCreate("/api/allocation-rules", map[string]any{"gl_account_id": salary, "activity_id": pool})
	a.mustCreate("/api/allocation-rules", map[string]any{"gl_account_id": depreciation, "department_code": "D200", "activity_id": f.formulaAct})
	if status, body := a.do("POST", "/api/allocation-rules", map[string]any{"gl_account_id": depreciation, "department_code": "D200", "activity_id": pool}); status != http.StatusUnprocessableEntity || detail(body, "department_code") == "" {
		t.Errorf("同じ会計科目・部門のルール: status = %d, body = %v", status, body)
	}

	csv := "target_month,account_code,department_code,box_code,amount,description\n" +
		"2026-04,4110,D100,PRJ-1,1000,施策コード\n" +
		"2026-04,4110,D100,P-9001,500,未登録の箱\n" +
		"2026-04,6110,D100,,300,山田 給与\n" + // ルール（全部門）
		"2026-04,6210,D200,,200,サーバー\n" + // ルール（D200）
		"2026-04,6210,D300,,70,PC\n" + // 未割当（D300 のルールなし）
		"2026-05,6210,D300,,30,PC\n" + // 未割当
		"2026-04,1110,D100,,99999,預金\n" // 対象外
	status, body := a.upload(actualsPath, csv, "4・5月実績")
	if status != http.StatusOK {
		t.Fatalf("取込: status = %d, body = %v", status, body)
	}
	if got := allocation(body); got != "activity_code=1 external_code=0 rule=2 unallocated=3" || body["excluded"] != float64(1) {
		t.Errorf("割当の根拠 = %s, excluded = %v", got, body["excluded"])
	}
	apr := body["totals"].([]any)[0].(map[string]any)
	if apr["revenue"] != "1500" || apr["expense"] != "570" || apr["unallocated_revenue"] != "500" || apr["unallocated_expense"] != "70" {
		t.Errorf("4月の合計（未割当を含む） = %v", apr)
	}
	if got := f.actualAmount(0, f.sales, "2026-04"); got != "500" {
		t.Errorf("未割当の売上 = %q", got)
	}
	if got := f.actualAmount(pool, f.cost, "2026-04"); got != "300" {
		t.Errorf("受け皿の給与 = %q", got)
	}

	// 予実比較: 全社では未割当を unit_id: null の行で返す（会計の合計と一致させる）
	// 決算確定月を 5月にした作成中のシナリオで確かめる
	a.do("PUT", fmt.Sprintf("/api/scenarios/%d", f.budget), map[string]any{"name": "2026年度 当初予算", "plan_role": "initial", "actual_through": "2026-05", "reason": "5月確定"})
	report := f.viewer.mustGet(fmt.Sprintf("/api/reports/comparison?scenario_ids=%d&include_actual=true", f.budget))
	unallocated := map[string]string{}
	for _, r := range report["rows"].([]any) {
		row := r.(map[string]any)
		if row["unit_id"] == nil {
			values := row["values"].(map[string]any)
			unallocated[fmt.Sprintf("%v/%v", row["subject_id"], row["month"])] = fmt.Sprintf("%v|%v", values["s1"], values["actual"])
		}
	}
	if got := unallocated[fmt.Sprintf("%d/2026-04", f.cost)]; got != "70|70" {
		t.Errorf("予実比較の未割当（4月の費用） = %q, all = %v", got, unallocated)
	}

	// 未割当の一覧: 箱の ID ごと・会計科目 × 部門ごと
	list := a.mustGet("/api/actuals/unallocated?fiscal_year=2026")
	if list["count"] != float64(3) || len(list["items"].([]any)) != 2 {
		t.Fatalf("未割当の一覧 = %v", list)
	}
	if status, _ := f.manager1.do("GET", "/api/actuals/unallocated", nil); status != http.StatusForbidden {
		t.Errorf("マネージャーの未割当の一覧: status = %d, want 403", status)
	}

	// 箱の ID を施策に割り当てると、外部コードとして登録される
	status, body = a.do("POST", "/api/actuals/unallocated/assign", map[string]any{"box_code": "P-9001", "activity_id": f.manualAct, "reason": "新規案件"})
	if status != http.StatusOK || body["assigned"] != float64(1) || body["external_code"] != "P-9001" {
		t.Fatalf("箱の ID の割当: status = %d, body = %v", status, body)
	}
	if got := f.actualAmount(f.manualAct, f.sales, "2026-04"); got != "1500" {
		t.Errorf("割り当てた後の売上 = %q", got)
	}
	if got := f.actualAmount(0, f.sales, "2026-04"); got != "" {
		t.Errorf("割り当てた後の未割当の売上 = %q", got)
	}
	codes := a.mustGet(fmt.Sprintf("/api/activities/%d", f.manualAct))["external_codes"].([]any)
	if len(codes) != 1 || codes[0].(map[string]any)["code"] != "P-9001" {
		t.Errorf("登録された外部コード = %v", codes)
	}
	// 同じ箱の ID はもう登録できない
	if status, _ := a.do("POST", "/api/actuals/unallocated/assign", map[string]any{"box_code": "P-9001", "activity_id": pool, "reason": "再"}); status != http.StatusConflict {
		t.Errorf("登録済みの箱の ID: status = %d, want 409", status)
	}

	// 会計科目 × 部門を割り当てると、割当ルールが追加され、すべての月の未割当が割り当たる
	status, body = a.do("POST", "/api/actuals/unallocated/assign", map[string]any{"gl_account_id": depreciation, "department_code": "D300", "activity_id": pool, "reason": "PC は共通費"})
	if status != http.StatusOK || body["assigned"] != float64(2) || len(body["months"].([]any)) != 2 || body["rule"] == nil {
		t.Fatalf("会計科目 × 部門の割当: status = %d, body = %v", status, body)
	}
	if got := f.actualAmount(pool, f.cost, "2026-05"); got != "30" {
		t.Errorf("割り当てた後の5月の費用 = %q", got)
	}
	if list := a.mustGet("/api/actuals/unallocated"); list["count"] != float64(0) {
		t.Errorf("割り当てた後の未割当 = %v", list)
	}

	// 取り込み直しても同じ施策に入る（ルールとして残っている）
	status, body = a.upload(actualsPath, csv, "4・5月実績 再取込")
	if status != http.StatusOK || allocation(body) != "activity_code=1 external_code=1 rule=4 unallocated=0" {
		t.Errorf("再取込: status = %d, body = %v", status, body)
	}

	// ルールを変えても取込済みの明細は変わらない。再割当で当て直す（dry run で増減を確認できる）
	rules := a.mustGet("/api/allocation-rules")["items"].([]any)
	var salaryRule int64
	for _, r := range rules {
		if rule := r.(map[string]any); rule["gl_account_code"] == "6110" {
			salaryRule = int64(rule["id"].(float64))
		}
	}
	a.do("PUT", fmt.Sprintf("/api/allocation-rules/%d", salaryRule), map[string]any{"gl_account_id": salary, "activity_id": f.formulaAct, "reason": "付け替え"})
	if got := f.actualAmount(pool, f.cost, "2026-04"); got != "370" {
		t.Errorf("ルール変更後（再割当前）の受け皿 = %q, want 370", got)
	}
	status, body = a.do("POST", "/api/actuals/reallocate", map[string]any{"months": []string{"2026-04"}, "dry_run": true})
	if status != http.StatusOK || body["changed"] != float64(1) || len(body["changes"].([]any)) != 2 {
		t.Fatalf("再割当（dry run）: status = %d, body = %v", status, body)
	}
	if got := f.actualAmount(pool, f.cost, "2026-04"); got != "370" {
		t.Errorf("dry run なのに変わった: %q", got)
	}
	if status, body := a.do("POST", "/api/actuals/reallocate", map[string]any{"months": []string{"2026-04"}}); status != http.StatusUnprocessableEntity || detail(body, "reason") == "" {
		t.Errorf("理由なしの再割当: status = %d, body = %v", status, body)
	}
	if status, body := a.do("POST", "/api/actuals/reallocate", map[string]any{"months": []string{"2026-06"}, "reason": "x"}); status != http.StatusUnprocessableEntity || detail(body, "months") == "" {
		t.Errorf("明細のない月の再割当: status = %d, body = %v", status, body)
	}
	if status, body := a.do("POST", "/api/actuals/reallocate", map[string]any{"months": []string{"2026-04"}, "reason": "給与の付け替え"}); status != http.StatusOK {
		t.Fatalf("再割当: status = %d, body = %v", status, body)
	}
	if got := f.actualAmount(pool, f.cost, "2026-04"); got != "70" {
		t.Errorf("再割当後の受け皿 = %q, want 70", got)
	}

	// 施策の明細: 給与（hide_details）は FP&A 以外には合計だけ
	path := fmt.Sprintf("/api/activities/%d/actual-entries?month=2026-04&subject_id=%d", f.formulaAct, f.cost)
	view := f.member.mustGet(path)
	if len(view["items"].([]any)) != 1 || len(view["hidden"].([]any)) != 1 || view["entries_total"] != "500" || view["fact_total"] != "500" {
		t.Errorf("担当者の明細 = %v", view)
	}
	if hidden := view["hidden"].([]any)[0].(map[string]any); hidden["gl_account_code"] != "6110" || hidden["amount"] != "300" || hidden["count"] != float64(1) {
		t.Errorf("見せない会計科目の合計 = %v", hidden)
	}
	if strings.Contains(fmt.Sprint(view), "山田") {
		t.Errorf("給与の摘要が見えている: %v", view)
	}
	view = a.mustGet(path)
	if len(view["items"].([]any)) != 2 || len(view["hidden"].([]any)) != 0 {
		t.Errorf("FP&A の明細 = %v", view)
	}

	// ロックすると未割当も含めて実績を固定する
	a.do("POST", "/api/actuals/unallocated/assign", map[string]any{"gl_account_id": depreciation, "activity_id": f.manualAct, "reason": "x"}) // 何も割り当たらない（全部門のルールだけ追加）
	a.upload(actualsPath, "target_month,account_code,box_code,amount\n2026-05,4110,UNKNOWN,40\n", "5月 再取込")
	if got := f.actualAmount(0, f.sales, "2026-05"); got != "40" {
		t.Fatalf("5月の未割当 = %q", got)
	}
	if status, body := a.do("POST", fmt.Sprintf("/api/scenarios/%d/lock", f.budget), nil); status != http.StatusOK {
		t.Fatalf("ロック: status = %d, body = %v", status, body)
	}
	var frozen string
	if err := f.env.QueryRow("SELECT CAST(SUM(amount) AS CHAR) FROM scenario_unallocated WHERE scenario_id = ?", f.budget).Scan(&frozen); err != nil || frozen != "40" {
		t.Errorf("ロックしたシナリオの未割当 = %q, err = %v", frozen, err)
	}
}

func TestGLAccountsAndRulesCSV(t *testing.T) {
	f := newScenarioFixture(t)
	a := f.admin

	status, body := a.upload("/api/gl-accounts/import", "code,name,subject_code,hide_details\n4110,受託売上（改）,4110,false\n6110,給与,8110,true\n1110,普通預金,,\n", "会計科目")
	if status != http.StatusOK || body["inserted"] != float64(2) || body["updated"] != float64(1) {
		t.Fatalf("会計科目の取込: status = %d, body = %v", status, body)
	}
	exported := a.download("/api/gl-accounts/export")
	if !strings.Contains(exported, "1110,普通預金,,false") || !strings.Contains(exported, "6110,給与,8110,true") {
		t.Errorf("会計科目のエクスポート = %q", exported)
	}
	status, body = a.upload("/api/allocation-rules/import", "account_code,department_code,activity_code\n6110,,PRJ-1\n6110,D1,SAAS-1\n", "ルール")
	if status != http.StatusOK || body["inserted"] != float64(2) {
		t.Fatalf("割当ルールの取込: status = %d, body = %v", status, body)
	}
	status, body = a.upload("/api/allocation-rules/import", "account_code,department_code,activity_code\n6110,,SAAS-1\n6110,D1,SAAS-1\n", "ルール")
	if status != http.StatusOK || body["updated"] != float64(1) || body["unchanged"] != float64(1) {
		t.Errorf("割当ルールの再取込: status = %d, body = %v", status, body)
	}
	if got := a.download("/api/allocation-rules/export"); !strings.Contains(got, "6110,,SAAS-1") || !strings.Contains(got, "6110,D1,SAAS-1") {
		t.Errorf("割当ルールのエクスポート = %q", got)
	}
	status, body = a.upload("/api/allocation-rules/import", "account_code,department_code,activity_code\n9999,,NOPE\n", "ルール")
	if status != http.StatusUnprocessableEntity || len(body["error"].(map[string]any)["rows"].([]any)) != 2 {
		t.Errorf("未登録のコード: status = %d, body = %v", status, body)
	}
}
