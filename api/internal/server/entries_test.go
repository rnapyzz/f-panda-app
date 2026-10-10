package server_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// TestActualEntriesAll は施策の実績の明細（すべての月・100件ずつ・エクスポート）を確かめる。
func TestActualEntriesAll(t *testing.T) {
	f := newScenarioFixture(t)
	a := f.admin
	a.mustCreate("/api/gl-accounts", map[string]any{"code": "6110", "name": "給与", "subject_id": f.cost, "hide_details": true})
	var b strings.Builder
	b.WriteString("target_month,account_code,box_code,amount,description\n")
	for i := 0; i < 150; i++ {
		fmt.Fprintf(&b, "2026-04,4110,PRJ-1,1,4月の請求 %d\n", i+1)
	}
	b.WriteString("2026-05,4110,PRJ-1,100,5月の請求\n2026-06,6110,PRJ-1,300,山田 給与\n")
	if status, body := a.upload("/api/actuals/import", b.String(), "実績取込"); status != http.StatusOK {
		t.Fatalf("取込: %d %v", status, body)
	}
	path := fmt.Sprintf("/api/activities/%d/actual-entries", f.manualAct)

	// 既定はすべての月。新しい月から 100 件。合計は全件。給与は担当者には合計だけ
	v := f.member.mustGet(path)
	items := v["items"].([]any)
	if fmt.Sprint(v["months"]) != "[2026-04 2026-05 2026-06]" || v["total"] != 151.0 || len(items) != 100 || v["has_more"] != true || v["entries_total"] != "550" || v["fact_total"] != "550" {
		t.Fatalf("1ページ目 = months %v total %v items %d has_more %v entries_total %v fact_total %v", v["months"], v["total"], len(items), v["has_more"], v["entries_total"], v["fact_total"])
	}
	if first := items[0].(map[string]any); first["target_month"] != "2026-05" {
		t.Errorf("先頭 = %v, want 2026-05（新しい月から。給与の明細は出さない）", first)
	}
	if h := v["hidden"].([]any); len(h) != 1 || h[0].(map[string]any)["amount"] != "300" {
		t.Errorf("見せない会計科目の合計 = %v", h)
	}
	if v := f.member.mustGet(path + "?offset=100"); len(v["items"].([]any)) != 51 || v["has_more"] != false {
		t.Errorf("2ページ目 = %d件, has_more %v", len(v["items"].([]any)), v["has_more"])
	}
	// 絞り込み
	if v := f.member.mustGet(path + "?month=2026-05"); v["total"] != 1.0 || v["entries_total"] != "100" {
		t.Errorf("月で絞り込み = %v", v)
	}
	if v := f.member.mustGet(path + "?fiscal_year=2027"); v["total"] != 0.0 || v["entries_total"] != "0" {
		t.Errorf("年度で絞り込み = %v", v)
	}
	if v := a.mustGet(fmt.Sprintf("%s?subject_id=%d", path, f.cost)); v["total"] != 1.0 || len(v["hidden"].([]any)) != 0 {
		t.Errorf("FP&A の科目で絞り込み = %v", v)
	}
	for _, q := range []string{"?month=2026", "?fiscal_year=abc", "?limit=1000", "?offset=-1"} {
		if status, _ := f.member.do("GET", path+q, nil); status != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", q, status)
		}
	}

	// エクスポート: 絞り込みどおりの全件。給与は担当者には月 × 会計科目の合計の行
	csv := f.member.download(path + "/export")
	lines := strings.Split(strings.TrimSpace(csv), "\r\n")
	if len(lines) != 1+151+1 || !strings.HasPrefix(lines[0], "target_month,subject_code,") {
		t.Fatalf("CSV = %d 行, 先頭 %q", len(lines), lines[0])
	}
	if !strings.HasPrefix(lines[1], "2026-06,8110,外注費,6110,給与,,,明細は FP&A のみ（1 行の合計）,300,") || strings.Contains(csv, "山田") {
		t.Errorf("給与の行 = %q", lines[1])
	}
	if !strings.Contains(lines[2], "2026-05,4110,受託売上,4110,受託売上,,PRJ-1,5月の請求,100,施策コード") {
		t.Errorf("明細の行 = %q", lines[2])
	}
	if csv := a.download(path + "/export?month=2026-06"); !strings.Contains(csv, "山田 給与") || len(strings.Split(strings.TrimSpace(csv), "\r\n")) != 2 {
		t.Errorf("FP&A の CSV = %q", csv)
	}
}
