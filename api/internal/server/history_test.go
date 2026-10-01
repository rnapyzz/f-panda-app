package server_test

import (
	"fmt"
	"net/http"
	"testing"
)

// changeSetIDs は一覧レスポンスの変更セットを (理由, ID) の順で返す。
func changeSetReasons(body map[string]any) []string {
	var out []string
	for _, it := range body["items"].([]any) {
		out = append(out, it.(map[string]any)["reason"].(string))
	}
	return out
}

func TestChangeHistory(t *testing.T) {
	f := newScenarioFixture(t)
	activityPath := fmt.Sprintf("/api/activities/%d", f.manualAct)

	// 施策の確度を変更（理由あり）
	update := activityBody(f.fn1, "PRJ-1", map[string]any{"owner_user_id": f.memberID, "probability": 0.6, "reason": "受注確度の見直し"})
	if status, body := f.member.do("PUT", activityPath, update); status != http.StatusOK {
		t.Fatalf("確度変更: status = %d, body = %v", status, body)
	}
	// 金額の入力（理由あり）
	if status, body := f.member.do("PUT", f.valuesPath(f.budget, f.manualAct)+"/amounts", map[string]any{
		"reason": "予算の初回入力", "amounts": []map[string]any{{"subject_id": f.sales, "target_month": "2026-10", "amount": 1000}},
	}); status != http.StatusOK {
		t.Fatalf("金額入力: status = %d, body = %v", status, body)
	}
	// 別の施策のドライバー値（理由あり）
	if status, body := f.member.do("PUT", f.valuesPath(f.budget, f.formulaAct)+"/driver-values", map[string]any{
		"reason": "単価の入力", "values": []map[string]any{{"driver_id": f.priceID, "target_month": "2026-10", "value": 500}},
	}); status != http.StatusOK {
		t.Fatalf("ドライバー値: status = %d, body = %v", status, body)
	}
	// マスタの変更（理由なし）
	f.admin.mustCreate("/api/subjects", map[string]any{"code": "9999", "name": "雑費", "category": "expense"})

	// 一覧は新しい順。閲覧者も参照できる
	body := f.viewer.mustGet("/api/change-sets?limit=4")
	if got := changeSetReasons(body); fmt.Sprint(got) != "[ 単価の入力 予算の初回入力 受注確度の見直し]" {
		t.Errorf("一覧 = %q", got)
	}
	if body["has_more"] != true {
		t.Errorf("has_more = %v, want true（フィクスチャの作成分がまだある）", body["has_more"])
	}
	latest := body["items"].([]any)[2].(map[string]any)
	acts := latest["activities"].([]any)
	if len(acts) != 1 || acts[0].(map[string]any)["code"] != "PRJ-1" || latest["tables"].(map[string]any)["budget_facts"] != float64(1) {
		t.Errorf("金額入力の変更セット = %v", latest)
	}

	// 続きの取得
	lastID := int64(body["items"].([]any)[3].(map[string]any)["id"].(float64))
	more := f.viewer.mustGet(fmt.Sprintf("/api/change-sets?limit=4&before_id=%d", lastID))
	for _, it := range more["items"].([]any) {
		if int64(it.(map[string]any)["id"].(float64)) >= lastID {
			t.Errorf("before_id より新しい変更セットが含まれている: %v", it)
		}
	}

	// 施策で絞り込むと、施策自体・金額の変更が含まれ、別施策やマスタの変更は含まれない
	body = f.viewer.mustGet(fmt.Sprintf("/api/change-sets?activity_id=%d", f.manualAct))
	reasons := fmt.Sprint(changeSetReasons(body))
	if reasons != "[予算の初回入力 受注確度の見直し ]" {
		t.Errorf("施策 PRJ-1 の履歴 = %s（最後の空は作成時）", reasons)
	}
	// ドライバー値の変更は、ドライバー経由で施策に結びつく
	body = f.viewer.mustGet(fmt.Sprintf("/api/change-sets?activity_id=%d&reason=with", f.formulaAct))
	if got := fmt.Sprint(changeSetReasons(body)); got != "[単価の入力 算出式の設定]" {
		t.Errorf("施策 SAAS-1 の理由ありの履歴 = %s", got)
	}

	// シナリオ・理由の有無で絞り込み
	body = f.viewer.mustGet(fmt.Sprintf("/api/change-sets?scenario_id=%d", f.budget))
	if got := fmt.Sprint(changeSetReasons(body)); got != "[単価の入力 予算の初回入力 ]" {
		t.Errorf("シナリオの履歴 = %s（最後の空は作成中の指定）", got)
	}
	body = f.viewer.mustGet("/api/change-sets?reason=without&limit=1")
	if got := changeSetReasons(body); len(got) != 1 || got[0] != "" {
		t.Errorf("理由なしの履歴 = %q", got)
	}

	// 詳細: 対象が人の読める名前になる
	id := int64(latest["id"].(float64))
	detail := f.viewer.mustGet(fmt.Sprintf("/api/change-sets/%d", id))
	logs := detail["logs"].([]any)
	if len(logs) != 1 {
		t.Fatalf("logs = %v", logs)
	}
	l := logs[0].(map[string]any)
	if l["label"] != "施策 PRJ-1 / 受託売上 / 2026-10" || l["action"] != "insert" || l["after"].(map[string]any)["amount"] != "1000" {
		t.Errorf("log = %v", l)
	}

	// 期間の絞り込み（未来の日付からは何も出ない）
	if got := f.viewer.mustGet("/api/change-sets?from=2999-01-01")["items"].([]any); len(got) != 0 {
		t.Errorf("未来の期間 = %d 件", len(got))
	}
	if status, _ := f.viewer.do("GET", "/api/change-sets/99999", nil); status != http.StatusNotFound {
		t.Errorf("存在しない変更セット: status = %d, want 404", status)
	}
	for _, q := range []string{"reason=maybe", "from=2026/01/01", "activity_id=x", "limit=0"} {
		if status, _ := f.viewer.do("GET", "/api/change-sets?"+q, nil); status != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", q, status)
		}
	}
}
