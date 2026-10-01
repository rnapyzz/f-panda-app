package server_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestActivityCodeAutoNumbering(t *testing.T) {
	f := newActivityFixture(t)
	create := func(code string) string {
		t.Helper()
		status, body := f.admin.do("POST", "/api/activities", activityBody(f.fn1, code, nil))
		if status != http.StatusCreated {
			t.Fatalf("作成: status = %d, body = %v", status, body)
		}
		return body["code"].(string)
	}
	if got := create(""); got != "ACT-0001" {
		t.Errorf("1件目 = %s, want ACT-0001", got)
	}
	if got := create(""); got != "ACT-0002" {
		t.Errorf("2件目 = %s, want ACT-0002", got)
	}
	// 手入力のコードは自由。ACT-数字 の形なら続きから採番する
	create("ACT-0010")
	create("X-99")
	if got := create(""); got != "ACT-0011" {
		t.Errorf("手入力の後 = %s, want ACT-0011", got)
	}
	// 自動採番しない場合（更新）は空欄を許さない
	status, body := f.admin.do("PUT", "/api/activities/1", activityBody(f.fn1, "", nil))
	if status != http.StatusUnprocessableEntity || detail(body, "code") == "" {
		t.Errorf("更新で空欄: status = %d, body = %v", status, body)
	}
}

func TestExternalCodes(t *testing.T) {
	f := newScenarioFixture(t)
	base := fmt.Sprintf("/api/activities/%d/external-codes", f.manualAct)

	// 枠の施策に複数の外部コードを付けられる。担当者も付けられる
	p1 := f.member.mustCreate(base, map[string]any{"code": "P-1001", "note": "A社 受託"})
	f.member.mustCreate(base, map[string]any{"code": "P-1002"})
	detailBody := f.viewer.mustGet(fmt.Sprintf("/api/activities/%d", f.manualAct))
	if n := len(detailBody["external_codes"].([]any)); n != 2 {
		t.Errorf("外部コードの件数 = %d, want 2", n)
	}

	tests := []struct {
		name, path, code, want string
	}{
		{"他の施策に登録済み", fmt.Sprintf("/api/activities/%d/external-codes", f.formulaAct), "P-1001", "施策「施策 PRJ-1」に登録されています"},
		{"同じ施策に登録済み", base, "P-1002", "この施策に登録済みです"},
		{"施策コードと同じ", base, "SAAS-1", "施策コードと同じです"},
		{"空白を含む", base, "P 1003", "空白"},
		{"カンマを含む", base, "P,1003", "カンマ"},
	}
	for _, tt := range tests {
		status, body := f.admin.do("POST", tt.path, map[string]any{"code": tt.code})
		if status != http.StatusUnprocessableEntity || !strings.Contains(detail(body, "code"), tt.want) {
			t.Errorf("%s: status = %d, body = %v", tt.name, status, body)
		}
	}
	// 担当外は付けられない
	if status, _ := f.member2.do("POST", base, map[string]any{"code": "P-2000"}); status != http.StatusForbidden {
		t.Errorf("担当外: status = %d, want 403", status)
	}

	// 施策コードを外部コードと同じにはできない（作成・更新とも）
	if status, body := f.admin.do("POST", "/api/activities", activityBody(f.fn1, "P-1002", nil)); status != http.StatusUnprocessableEntity || detail(body, "code") == "" {
		t.Errorf("外部コードと同じ施策コードで作成: status = %d, body = %v", status, body)
	}
	update := activityBody(f.fn1, "P-1001", map[string]any{"owner_user_id": f.memberID})
	if status, body := f.admin.do("PUT", fmt.Sprintf("/api/activities/%d", f.manualAct), update); status != http.StatusUnprocessableEntity || detail(body, "code") == "" {
		t.Errorf("外部コードと同じ施策コードに更新: status = %d, body = %v", status, body)
	}

	// 一覧のキーワード検索は外部コードにも一致する
	items := f.viewer.mustGet("/api/activities?q=P-1002")["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["code"] != "PRJ-1" {
		t.Errorf("外部コードで検索 = %v", items)
	}

	// 実績 CSV は外部コードでも取り込め、同じ施策の行は合算される
	csv := "target_month,activity_code,subject_code,amount\n" +
		"2026-09,P-1001,4110,1000\n" +
		"2026-09,P-1002,4110,250\n" +
		"2026-09,PRJ-1,4110,5\n"
	status, body := f.admin.upload("/api/actuals/import", csv, "外部コードで取込")
	if status != http.StatusOK || body["facts"] != float64(1) {
		t.Fatalf("取込: status = %d, body = %v", status, body)
	}
	if got := f.actualAmount(f.manualAct, f.sales, "2026-09"); got != "1255" {
		t.Errorf("合算された実績 = %q, want 1255", got)
	}

	// 外部コードを外しても実績は残る。変更履歴に残る
	if status, _ := f.member.do("DELETE", fmt.Sprintf("%s/%d", base, p1), map[string]any{"reason": "案件番号の付け替え"}); status != http.StatusNoContent {
		t.Fatalf("削除: status = %d", status)
	}
	if got := f.actualAmount(f.manualAct, f.sales, "2026-09"); got != "1255" {
		t.Errorf("外部コード削除後の実績 = %q", got)
	}
	hist := f.viewer.mustGet(fmt.Sprintf("/api/change-sets?activity_id=%d&reason=with", f.manualAct))
	latest := hist["items"].([]any)[0].(map[string]any)
	if latest["reason"] != "案件番号の付け替え" {
		t.Errorf("最新の履歴 = %v", latest)
	}
	d := f.viewer.mustGet(fmt.Sprintf("/api/change-sets/%d", int64(latest["id"].(float64))))
	if label := d["logs"].([]any)[0].(map[string]any)["label"]; label != "施策 PRJ-1 / 外部コード「P-1001」" {
		t.Errorf("label = %v", label)
	}
	// 外した外部コードは別の施策に付け替えられる
	f.admin.mustCreate(fmt.Sprintf("/api/activities/%d/external-codes", f.formulaAct), map[string]any{"code": "P-1001"})
}
