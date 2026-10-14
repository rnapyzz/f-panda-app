package server_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// TestPartners は取引先のマスタと内訳への紐づけ（docs/plan.md「2.29」）を確かめる。
func TestPartners(t *testing.T) {
	f := newScenarioFixture(t)
	a := f.admin

	// 作成: 取引先コードは空なら TP-0001 から採番する
	status, body := a.do("POST", "/api/partners", map[string]any{"name": "A社 情報システム部", "company_name": "A社", "accounting_code": "C001", "direction": "sales"})
	if status != http.StatusCreated || body["code"] != "TP-0001" || body["is_active"] != true {
		t.Fatalf("作成: %d %v", status, body)
	}
	customer := int64(body["id"].(float64))
	vendor := a.mustCreate("/api/partners", map[string]any{"name": "B社", "company_name": "B社", "direction": "purchase"})
	stopped := a.mustCreate("/api/partners", map[string]any{"name": "C社 旧営業部", "company_name": "C社", "direction": "sales", "is_active": false})

	for name, tc := range map[string]struct {
		body  map[string]any
		field string
	}{
		"コードの重複":   {map[string]any{"code": "TP-0001", "name": "x", "company_name": "x", "direction": "sales"}, "code"},
		"会計コードの重複": {map[string]any{"name": "x", "company_name": "x", "accounting_code": "C001", "direction": "sales"}, "accounting_code"},
		"区分が不正":    {map[string]any{"name": "x", "company_name": "x", "direction": "both"}, "direction"},
		"会社名がない":   {map[string]any{"name": "x", "direction": "sales"}, "company_name"},
	} {
		if status, body := a.do("POST", "/api/partners", tc.body); status != http.StatusUnprocessableEntity || detail(body, tc.field) == "" {
			t.Errorf("%s: %d %v", name, status, body)
		}
	}
	// 会計コードは空なら重複してよい
	a.mustCreate("/api/partners", map[string]any{"name": "D社", "company_name": "D社", "direction": "purchase"})
	// 登録・変更は FP&A だけ。参照は全員
	if status, _ := f.manager1.do("POST", "/api/partners", map[string]any{"name": "x", "company_name": "x", "direction": "sales"}); status != http.StatusForbidden {
		t.Errorf("マネージャーの作成: %d, want 403", status)
	}
	if list := f.member.mustGet("/api/partners")["items"].([]any); len(list) != 4 {
		t.Errorf("担当者の参照: %d 件", len(list))
	}

	// 内訳に付ける（施策の編集権限があれば担当者も付けられる）
	linesPath := fmt.Sprintf("/api/activities/%d/lines", f.manualAct)
	status, body = f.member.do("POST", linesPath, map[string]any{"subject_id": f.sales, "name": "A社分", "partner_id": customer})
	if status != http.StatusCreated || numString(body["partner_id"]) != fmt.Sprint(customer) {
		t.Fatalf("取引先付きの内訳: %d %v", status, body)
	}
	line := int64(body["id"].(float64))
	for name, tc := range map[string]map[string]any{
		"収益の内訳に発注の取引先": {"subject_id": f.sales, "name": "x1", "partner_id": vendor},
		"費用の内訳に受注の取引先": {"subject_id": f.cost, "name": "x2", "partner_id": customer},
		"取引停止の取引先":     {"subject_id": f.sales, "name": "x3", "partner_id": stopped},
		"ない取引先":        {"subject_id": f.sales, "name": "x4", "partner_id": 999999},
	} {
		if status, body := f.member.do("POST", linesPath, tc); status != http.StatusUnprocessableEntity || detail(body, "partner_id") == "" {
			t.Errorf("%s: %d %v", name, status, body)
		}
	}
	f.member.mustCreate(linesPath, map[string]any{"subject_id": f.cost, "name": "B社外注", "partner_id": vendor})

	// 更新: partner_id を省略すると今のまま、null で外す
	linePath := fmt.Sprintf("%s/%d", linesPath, line)
	if status, body := f.member.do("PUT", linePath, map[string]any{"name": "A社分（保守）", "outlook": "base"}); status != http.StatusOK || numString(body["partner_id"]) != fmt.Sprint(customer) {
		t.Errorf("取引先を省略した更新: %d %v", status, body)
	}

	// 数値入力の画面に、内訳の取引先が出る。名前を変えても紐づきは変わらない
	if status, body := a.do("PUT", fmt.Sprintf("/api/partners/%d", customer), map[string]any{"name": "A社 DX推進部", "company_name": "A社", "accounting_code": "C001", "direction": "sales"}); status != http.StatusOK || body["code"] != "TP-0001" {
		t.Fatalf("取引先名の変更: %d %v", status, body)
	}
	view := a.mustGet(f.valuesPath(f.budget, f.manualAct))
	var found map[string]any
	for _, row := range view["amounts"].([]any) {
		for _, l := range row.(map[string]any)["lines"].([]any) {
			if lm := l.(map[string]any); int64(lm["id"].(float64)) == line {
				found, _ = lm["partner"].(map[string]any)
			}
		}
	}
	if found == nil || found["name"] != "A社 DX推進部" {
		t.Errorf("数値入力の画面の取引先 = %v", found)
	}

	// 取引先の画面: 付いている内訳と施策
	lines := a.mustGet(fmt.Sprintf("/api/partners/%d/lines", customer))["items"].([]any)
	if len(lines) != 1 || lines[0].(map[string]any)["activity_code"] != "PRJ-1" {
		t.Errorf("取引先の内訳 = %v", lines)
	}

	// 内訳に付いている取引先は、区分を変えられず、削除できない（取引停止にする）
	if status, body := a.do("PUT", fmt.Sprintf("/api/partners/%d", customer), map[string]any{"name": "A社 DX推進部", "company_name": "A社", "direction": "purchase"}); status != http.StatusUnprocessableEntity || detail(body, "direction") == "" {
		t.Errorf("付いている取引先の区分の変更: %d %v", status, body)
	}
	if status, _ := a.do("DELETE", fmt.Sprintf("/api/partners/%d", customer), map[string]any{}); status != http.StatusConflict {
		t.Errorf("付いている取引先の削除: %d, want 409", status)
	}
	if status, _ := a.do("PUT", fmt.Sprintf("/api/partners/%d", customer), map[string]any{"name": "A社 DX推進部", "company_name": "A社", "accounting_code": "C001", "direction": "sales", "is_active": false}); status != http.StatusOK {
		t.Errorf("取引停止: %d", status)
	}
	// 取引停止になっても、付いている内訳のほかの項目は直せる
	if status, body := f.member.do("PUT", linePath, map[string]any{"name": "A社分", "outlook": "base"}); status != http.StatusOK {
		t.Errorf("取引停止の取引先が付いた内訳の更新: %d %v", status, body)
	}
	if status, body := f.member.do("PUT", linePath, map[string]any{"name": "A社分", "outlook": "base", "partner_id": nil}); status != http.StatusOK || body["partner_id"] != nil {
		t.Errorf("取引先を外す: %d %v", status, body)
	}
	if status, _ := a.do("DELETE", fmt.Sprintf("/api/partners/%d", stopped), map[string]any{}); status != http.StatusNoContent {
		t.Errorf("付いていない取引先の削除: %d", status)
	}

	// 監査ログ
	var logs int
	if err := f.env.QueryRow("SELECT COUNT(*) FROM audit_logs WHERE table_name = 'partners'").Scan(&logs); err != nil || logs < 6 {
		t.Errorf("取引先の監査ログ = %d (%v)", logs, err)
	}

	// 施策をコピーして作ると、内訳の取引先も写る
	status, body = a.do("POST", "/api/activities", activityBody(f.fn1, "PRJ-COPY", map[string]any{"copy_from_activity_id": f.manualAct}))
	if status != http.StatusCreated {
		t.Fatalf("コピー: %d %v", status, body)
	}
	copied := a.mustGet(fmt.Sprintf("/api/activities/%d", int64(body["id"].(float64))))
	var copiedPartner string
	for _, l := range copied["lines"].([]any) {
		if lm := l.(map[string]any); lm["name"] == "B社外注" {
			copiedPartner = numString(lm["partner_id"])
		}
	}
	if copiedPartner != fmt.Sprint(vendor) {
		t.Errorf("コピーした内訳の取引先 = %q, want %d", copiedPartner, vendor)
	}

	// CSV: 書き出して、取り込むと変化なし。新しい行は追加、区分の書き方は 受注／発注 も可
	req, _ := http.NewRequest("GET", a.base+"/api/partners/export", nil)
	res, err := a.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := res.Body.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	res.Body.Close()
	exported := strings.TrimPrefix(sb.String(), "\ufeff")
	if !strings.HasPrefix(exported, "code,name,company_name,accounting_code,direction,is_active,note") {
		t.Fatalf("CSV の見出し = %q", strings.SplitN(exported, "\n", 2)[0])
	}
	if status, body := a.upload("/api/partners/import", exported, "往復"); status != http.StatusOK || body["unchanged"] != float64(3) || body["inserted"] != float64(0) {
		t.Errorf("同じ CSV の取込: %d %v", status, body)
	}
	if status, body := a.upload("/api/partners/import", "code,name,company_name,direction\nTP-0100,E社,E社,発注\n", "追加"); status != http.StatusOK || body["inserted"] != float64(1) {
		t.Errorf("CSV の追加: %d %v", status, body)
	}
	if status, body := a.upload("/api/partners/import", "code,name,company_name,direction\nTP-0002,B社,B社,sales\n", "区分の変更"); status != http.StatusUnprocessableEntity {
		t.Errorf("CSV で付いている取引先の区分を変える: %d %v", status, body)
	}
}
