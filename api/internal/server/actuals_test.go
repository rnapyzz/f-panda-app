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

// actualAmount は実績データの金額を返す（なければ空文字）。
func (f *scenarioFixture) actualAmount(activityID, subjectID int64, month string) string {
	f.admin.t.Helper()
	var amount string
	err := f.env.QueryRow("SELECT CAST(amount AS CHAR) FROM actual_facts WHERE activity_id = ? AND subject_id = ? AND target_month = ?",
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

	csv := "target_month,activity_code,subject_code,amount\n" +
		"2026-09,PRJ-1,4110,1200000\n" +
		"2026-09,PRJ-1,8110,-350000\n" +
		"2026-09,PRJ-1,8110,-50000\n" + // 同じキーは合算 → -400000
		"2026-10,SAAS-1,4110,300000\n"

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
	totals := body["totals"].([]any)
	if sep := totals[0].(map[string]any); sep["month"] != "2026-09" || sep["revenue"] != "1200000" || sep["expense"] != "-400000" {
		t.Errorf("2026-09 の合計 = %v", sep)
	}
	if got := f.actualAmount(f.manualAct, f.cost, "2026-09"); got != "-400000" {
		t.Errorf("取り込んだ外注費 = %q", got)
	}
	months := f.viewer.mustGet("/api/actuals/months?fiscal_year=2026")["months"].([]any)
	if len(months) != 2 || months[1] != "2026-10" {
		t.Errorf("取込済みの月 = %v", months)
	}

	// 同じファイルの再取込では何も変わらない
	status, body = f.admin.upload(actualsPath, csv, "再取込")
	if status != http.StatusOK || counts(body) != "inserted=0 updated=0 deleted=0 unchanged=3" {
		t.Errorf("同じファイルの再取込: status = %d, body = %v", status, body)
	}

	// 2026-09 だけを取り込み直すと、2026-09 は CSV の内容に置き換わり、2026-10 はそのまま
	status, body = f.admin.upload(actualsPath, "target_month,activity_code,subject_code,amount\n2026-09,PRJ-1,4110,1250000\n", "2026-09 実績修正")
	if status != http.StatusOK || counts(body) != "inserted=0 updated=1 deleted=1 unchanged=0" {
		t.Errorf("2026-09 の再取込: status = %d, body = %v", status, body)
	}
	if got := f.actualAmount(f.manualAct, f.cost, "2026-09"); got != "" {
		t.Errorf("CSV にない 2026-09 の外注費が残っている: %q", got)
	}
	if got := f.actualAmount(f.formulaAct, f.sales, "2026-10"); got != "300000" {
		t.Errorf("2026-10 が変わった: %q", got)
	}

	// 取込は変更セット（理由付き、シナリオなし）と監査ログに残る
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
	csv := "target_month,activity_code,subject_code,amount\n" +
		"2026-09,PRJ-1,4110,1200000\n" +
		"2026-09,NOPE,4110,1\n" +
		"2026-13,PRJ-1,4110,1\n"
	status, body := f.admin.upload(actualsPath, csv, "取込")
	if status != http.StatusUnprocessableEntity || errorCode(body) != "invalid_csv" {
		t.Fatalf("status = %d, body = %v", status, body)
	}
	if got := f.actualAmount(f.manualAct, f.sales, "2026-09"); got != "" {
		t.Errorf("エラーがあるのに一部が保存された: %q", got)
	}

	valid := "target_month,activity_code,subject_code,amount\n2026-09,PRJ-1,4110,1\n"
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
