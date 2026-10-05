package server_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/rnapyzz/f-panda-app/api/internal/auth"
)

// activityFixture は施策のテストに使うユニットとユーザー。
type activityFixture struct {
	env                     *testEnv
	admin                   *client
	fn1, fn2                int64 // fn1 の担当は manager1、fn2 の担当は manager2
	manager1ID, memberID    int64
	manager1, manager2      *client
	member, member2, viewer *client
}

func newActivityFixture(t *testing.T) *activityFixture {
	t.Helper()
	admin, env := adminClient(t)
	f := &activityFixture{env: env, admin: admin}

	f.manager1ID = createUser(t, env.DB, "manager1@example.com", auth.RoleManager)
	manager2ID := createUser(t, env.DB, "manager2@example.com", auth.RoleManager)
	f.memberID = createUser(t, env.DB, "member@example.com", auth.RoleMember)
	createUser(t, env.DB, "member2@example.com", auth.RoleMember)
	createUser(t, env.DB, "viewer@example.com", auth.RoleViewer)

	seg := admin.mustCreate("/api/segments", map[string]any{"name": "XXX事業"})
	org := admin.mustCreate("/api/organizations", map[string]any{"name": "BBB部"})
	f.fn1 = admin.mustCreate("/api/units", map[string]any{"name": "CCC課", "segment_id": seg, "organization_id": org, "owner_user_id": f.manager1ID})
	f.fn2 = admin.mustCreate("/api/units", map[string]any{"name": "DDD課", "segment_id": seg, "organization_id": org, "owner_user_id": manager2ID})

	login := func(email string) *client {
		c := newClient(t, env.server)
		c.login(email)
		return c
	}
	f.manager1 = login("manager1@example.com")
	f.manager2 = login("manager2@example.com")
	f.member = login("member@example.com")
	f.member2 = login("member2@example.com")
	f.viewer = login("viewer@example.com")
	return f
}

// activityBody は施策の作成・更新リクエストを作る。overrides で項目を上書きする。
func activityBody(fn int64, code string, overrides map[string]any) map[string]any {
	b := map[string]any{
		"unit_id":       fn,
		"code":          code,
		"name":          "施策 " + code,
		"activity_type": "recurring",
		"status":        "in_progress",
	}
	for k, v := range overrides {
		b[k] = v
	}
	return b
}

func TestActivityPermissions(t *testing.T) {
	f := newActivityFixture(t)

	// 作成: FP&A はどこでも、マネージャーは担当ユニットのみ、担当者・閲覧者は不可
	f.admin.mustCreate("/api/activities", activityBody(f.fn2, "ACT-ADMIN", nil))
	id := f.manager1.mustCreate("/api/activities", activityBody(f.fn1, "ACT-1", map[string]any{"owner_user_id": f.memberID}))
	for name, c := range map[string]*client{"他ユニットのマネージャー": f.manager1, "担当者": f.member, "閲覧者": f.viewer} {
		fn := f.fn1
		if name == "他ユニットのマネージャー" {
			fn = f.fn2
		}
		if status, _ := c.do("POST", "/api/activities", activityBody(fn, "ACT-X", nil)); status != http.StatusForbidden {
			t.Errorf("%s の作成: status = %d, want 403", name, status)
		}
	}

	path := fmt.Sprintf("/api/activities/%d", id)
	update := activityBody(f.fn1, "ACT-1", map[string]any{"owner_user_id": f.memberID, "name": "更新後"})

	// 編集: 担当ユニットのマネージャー・施策の担当者は可、それ以外は不可
	for name, c := range map[string]*client{"担当ユニットのマネージャー": f.manager1, "施策の担当者": f.member} {
		if status, body := c.do("PUT", path, update); status != http.StatusOK {
			t.Errorf("%s の編集: status = %d, body = %v", name, status, body)
		}
	}
	for name, c := range map[string]*client{"他ユニットのマネージャー": f.manager2, "担当外の担当者": f.member2, "閲覧者": f.viewer} {
		if status, _ := c.do("PUT", path, update); status != http.StatusForbidden {
			t.Errorf("%s の編集: status = %d, want 403", name, status)
		}
	}

	// can_edit は閲覧者ごとに変わる
	if got := f.member.mustGet(path)["can_edit"]; got != true {
		t.Errorf("担当者から見た can_edit = %v, want true", got)
	}
	if got := f.member2.mustGet(path)["can_edit"]; got != false {
		t.Errorf("担当外から見た can_edit = %v, want false", got)
	}

	// 別のユニットへの移動は、移動先で作成できる権限が必要
	moved := activityBody(f.fn2, "ACT-1", map[string]any{"owner_user_id": f.memberID})
	for name, c := range map[string]*client{"マネージャー": f.manager1, "担当者": f.member} {
		if status, _ := c.do("PUT", path, moved); status != http.StatusForbidden {
			t.Errorf("%s による他ユニットへの移動: status = %d, want 403", name, status)
		}
	}

	// 削除: 担当者は不可、担当ユニットのマネージャーは可
	del := map[string]any{"reason": "施策の中止"}
	if status, _ := f.member.do("DELETE", path, del); status != http.StatusForbidden {
		t.Errorf("担当者の削除: status = %d, want 403", status)
	}
	if status, body := f.manager1.do("DELETE", path, del); status != http.StatusNoContent {
		t.Errorf("マネージャーの削除: status = %d, body = %v", status, body)
	}
}

func TestActivityValidation(t *testing.T) {
	f := newActivityFixture(t)
	f.admin.mustCreate("/api/activities", activityBody(f.fn1, "ACT-1", nil))

	tests := []struct {
		name      string
		overrides map[string]any
		code      string
		field     string
	}{
		{"コード重複", nil, "ACT-1", "code"},
		{"コードに使えない文字", nil, "ACT 2", "code"},
		{"施策タイプ不正", map[string]any{"activity_type": "flow"}, "ACT-2", "activity_type"},
		{"ステータス不正", map[string]any{"status": "done"}, "ACT-2", "status"},
		{"プロジェクト型の期間なし", map[string]any{"activity_type": "project"}, "ACT-2", "start_date"},
		{"終了日が開始日より前", map[string]any{"start_date": "2026-10-01", "end_date": "2026-09-30"}, "ACT-2", "end_date"},
		{"日付形式", map[string]any{"start_date": "2026/10/01"}, "ACT-2", "start_date"},
		{"確度の段階が未登録", map[string]any{"confidence_level": "Z"}, "ACT-2", "confidence_level"},
		{"存在しない担当者", map[string]any{"owner_user_id": 99999}, "ACT-2", "owner_user_id"},
	}
	for _, tt := range tests {
		status, body := f.admin.do("POST", "/api/activities", activityBody(f.fn1, tt.code, tt.overrides))
		if status != http.StatusUnprocessableEntity || detail(body, tt.field) == "" {
			t.Errorf("%s: status = %d, body = %v", tt.name, status, body)
		}
	}

	// プロジェクト型は期間を入れれば作成でき、確度は小数4桁で返る
	status, body := f.admin.do("POST", "/api/activities", activityBody(f.fn1, "PRJ-1", map[string]any{
		"activity_type": "project", "start_date": "2026-04-01", "end_date": "2027-03-31", "confidence_level": "B",
	}))
	if status != http.StatusCreated {
		t.Fatalf("プロジェクト型の作成: status = %d, body = %v", status, body)
	}
	if body["confidence_level"] != "B" || body["end_date"] != "2027-03-31" {
		t.Errorf("作成結果 = %v", body)
	}
}

func TestActivityChangesRequireReason(t *testing.T) {
	f := newActivityFixture(t)
	id := f.admin.mustCreate("/api/activities", activityBody(f.fn1, "ACT-1", map[string]any{"confidence_level": "C"}))
	path := fmt.Sprintf("/api/activities/%d", id)

	// 名称だけの変更は理由不要
	if status, body := f.admin.do("PUT", path, activityBody(f.fn1, "ACT-1", map[string]any{"confidence_level": "C", "name": "改名"})); status != http.StatusOK {
		t.Errorf("名称変更: status = %d, body = %v", status, body)
	}

	for name, overrides := range map[string]map[string]any{
		"確度":   {"confidence_level": "B"},
		"前提条件": {"confidence_level": "C", "assumptions": "大型案件の受注が前提"},
		"期間":   {"confidence_level": "C", "start_date": "2026-10-01"},
	} {
		body := activityBody(f.fn1, "ACT-1", overrides)
		body["name"] = "改名"
		status, res := f.admin.do("PUT", path, body)
		if status != http.StatusUnprocessableEntity || detail(res, "reason") == "" {
			t.Errorf("%s の変更（理由なし）: status = %d, body = %v", name, status, res)
		}
		body["reason"] = "受注見込みの上方修正"
		if status, res := f.admin.do("PUT", path, body); status != http.StatusOK {
			t.Errorf("%s の変更（理由あり）: status = %d, body = %v", name, status, res)
		}
		// 次のケースのために元に戻す
		reset := activityBody(f.fn1, "ACT-1", map[string]any{"confidence_level": "C", "name": "改名", "reason": "元に戻す"})
		f.admin.do("PUT", path, reset)
	}

	// 削除は理由が必須
	if status, body := f.admin.do("DELETE", path, nil); status != http.StatusUnprocessableEntity || detail(body, "reason") == "" {
		t.Errorf("理由なしの削除: status = %d, body = %v", status, body)
	}
}

func TestDriversAndLines(t *testing.T) {
	f := newActivityFixture(t)
	sales := f.admin.mustCreate("/api/subjects", map[string]any{"code": "4110", "name": "受託売上", "category": "revenue"})
	id := f.manager1.mustCreate("/api/activities", activityBody(f.fn1, "ACT-1", map[string]any{"owner_user_id": f.memberID}))
	base := fmt.Sprintf("/api/activities/%d", id)

	// ドライバーは担当者も追加できる
	price := f.member.mustCreate(base+"/drivers", map[string]any{"code": "unit_price", "name": "単価", "driver_kind": "value", "unit": "円"})
	f.member.mustCreate(base+"/drivers", map[string]any{"code": "volume", "name": "件数", "driver_kind": "kpi", "unit": "件"})

	for name, tc := range map[string]struct {
		body  map[string]any
		field string
	}{
		"コード重複": {map[string]any{"code": "volume", "name": "x", "driver_kind": "kpi"}, "code"},
		"コード形式": {map[string]any{"code": "Volume", "name": "x", "driver_kind": "kpi"}, "code"},
		"種別不正":  {map[string]any{"code": "x", "name": "x", "driver_kind": "money"}, "driver_kind"},
	} {
		if status, body := f.member.do("POST", base+"/drivers", tc.body); status != http.StatusUnprocessableEntity || detail(body, tc.field) == "" {
			t.Errorf("%s: status = %d, body = %v", name, status, body)
		}
	}

	lbase := base + "/lines"
	for name, tc := range map[string]struct {
		body  map[string]any
		field string
	}{
		"理由なし":      {map[string]any{"subject_id": sales, "name": "利用料", "expression": "unit_price * volume", "formula_enabled": true}, "reason"},
		"科目なし":      {map[string]any{"name": "利用料", "reason": "r"}, "subject_id"},
		"名前なし":      {map[string]any{"subject_id": sales, "reason": "r"}, "name"},
		"反映するのに式なし": {map[string]any{"subject_id": sales, "name": "利用料", "formula_enabled": true, "reason": "r"}, "expression"},
		"構文エラー":     {map[string]any{"subject_id": sales, "name": "利用料", "expression": "unit_price *", "reason": "r"}, "expression"},
		"未定義のドライバー": {map[string]any{"subject_id": sales, "name": "利用料", "expression": "unit_price * qty", "reason": "r"}, "expression"},
	} {
		if status, body := f.member.do("POST", lbase, tc.body); status != http.StatusUnprocessableEntity || detail(body, tc.field) == "" {
			t.Errorf("内訳 %s: status = %d, body = %v", name, status, body)
		}
	}

	// 直接入力の内訳は理由なしで作れる。計算式で反映する内訳は理由が必須
	f.member.mustCreate(lbase, map[string]any{"subject_id": sales, "name": "スポット"})
	lid := f.member.mustCreate(lbase, map[string]any{"subject_id": sales, "name": "利用料", "expression": "unit_price * volume * 2", "formula_enabled": true, "reason": "式で算出する"})
	if status, body := f.member.do("POST", lbase, map[string]any{"subject_id": sales, "name": "利用料"}); status != http.StatusUnprocessableEntity || detail(body, "name") == "" {
		t.Errorf("同名の内訳: status = %d, body = %v", status, body)
	}
	lpath := fmt.Sprintf("%s/%d", lbase, lid)
	if status, body := f.member.do("PUT", lpath, map[string]any{"name": "月額利用料", "expression": "unit_price * volume * 2", "formula_enabled": true}); status != http.StatusOK {
		t.Errorf("名前だけの変更（理由なし）: status = %d, body = %v", status, body)
	}
	if status, body := f.member.do("PUT", lpath, map[string]any{"name": "月額利用料", "expression": "unit_price * volume", "formula_enabled": true}); status != http.StatusUnprocessableEntity || detail(body, "reason") == "" {
		t.Errorf("式の変更（理由なし）: status = %d, body = %v", status, body)
	}
	if status, _ := f.member.do("PUT", lpath, map[string]any{"name": "月額利用料", "expression": "unit_price * volume", "formula_enabled": true, "reason": "確度は別で管理"}); status != http.StatusOK {
		t.Errorf("式の変更: status = %d, want 200", status)
	}
	if status, _ := f.member.do("PUT", fmt.Sprintf("%s/99999", lbase), map[string]any{"name": "x", "reason": "r"}); status != http.StatusNotFound {
		t.Errorf("存在しない内訳: status = %d, want 404", status)
	}

	// 計算式で使われているドライバーは、コード変更も削除もできない
	dpath := fmt.Sprintf("%s/drivers/%d", base, price)
	if status, body := f.member.do("PUT", dpath, map[string]any{"code": "price", "name": "単価", "driver_kind": "value"}); status != http.StatusUnprocessableEntity || detail(body, "code") == "" {
		t.Errorf("使用中のコード変更: status = %d, body = %v", status, body)
	}
	if status, _ := f.member.do("PUT", dpath, map[string]any{"code": "unit_price", "name": "販売単価", "driver_kind": "value"}); status != http.StatusOK {
		t.Errorf("名称のみの変更: status = %d, want 200", status)
	}
	if status, _ := f.member.do("DELETE", dpath, nil); status != http.StatusConflict {
		t.Errorf("使用中の削除: status = %d, want 409", status)
	}

	// 詳細にドライバー・内訳が含まれる
	detailBody := f.viewer.mustGet(base)
	if n := len(detailBody["drivers"].([]any)); n != 2 {
		t.Errorf("drivers の件数 = %d, want 2", n)
	}
	lines := detailBody["lines"].([]any)
	if len(lines) != 2 {
		t.Errorf("lines = %v", lines)
	}
	for _, l := range lines {
		if lm := l.(map[string]any); lm["name"] == "月額利用料" && (lm["expression"] != "unit_price * volume" || lm["formula_enabled"] != true) {
			t.Errorf("内訳 = %v", lm)
		}
	}

	// 計算式を使う内訳を消せばドライバーも消せる（反映しない式も参照に数える）
	if status, _ := f.member.do("PUT", lpath, map[string]any{"name": "月額利用料", "expression": "unit_price * volume", "formula_enabled": false, "reason": "直接入力に切り替え"}); status != http.StatusOK {
		t.Errorf("反映をやめる: status = %d, want 200", status)
	}
	if status, _ := f.member.do("DELETE", dpath, nil); status != http.StatusConflict {
		t.Errorf("反映しない式で使用中の削除: status = %d, want 409", status)
	}
	if status, _ := f.member.do("DELETE", lpath, nil); status != http.StatusUnprocessableEntity {
		t.Errorf("理由なしの内訳削除: status = %d, want 422", status)
	}
	if status, _ := f.member.do("DELETE", lpath, map[string]any{"reason": "内訳の整理"}); status != http.StatusNoContent {
		t.Errorf("内訳の削除: status = %d, want 204", status)
	}
	if status, _ := f.member.do("DELETE", dpath, nil); status != http.StatusNoContent {
		t.Errorf("ドライバーの削除: status = %d, want 204", status)
	}

	// 担当外のユーザーはドライバーを追加できない
	if status, _ := f.member2.do("POST", base+"/drivers", map[string]any{"code": "x", "name": "x", "driver_kind": "kpi"}); status != http.StatusForbidden {
		t.Errorf("担当外のドライバー追加: status = %d, want 403", status)
	}
}

func TestMilestones(t *testing.T) {
	f := newActivityFixture(t)
	id := f.admin.mustCreate("/api/activities", activityBody(f.fn1, "ACT-1", map[string]any{"owner_user_id": f.memberID}))
	base := fmt.Sprintf("/api/activities/%d/milestones", id)

	mid := f.member.mustCreate(base, map[string]any{"name": "要件定義完了", "due_date": "2026-11-30"})
	mpath := fmt.Sprintf("%s/%d", base, mid)

	if status, body := f.member.do("POST", base, map[string]any{"name": "x", "due_date": ""}); status != http.StatusUnprocessableEntity || detail(body, "due_date") == "" {
		t.Errorf("期日なし: status = %d, body = %v", status, body)
	}
	// ステータスだけの変更は理由不要、期日の変更は理由が必須
	if status, body := f.member.do("PUT", mpath, map[string]any{"name": "要件定義完了", "due_date": "2026-11-30", "status": "in_progress"}); status != http.StatusOK {
		t.Errorf("ステータス変更: status = %d, body = %v", status, body)
	}
	if status, body := f.member.do("PUT", mpath, map[string]any{"name": "要件定義完了", "due_date": "2026-12-15", "status": "delayed"}); status != http.StatusUnprocessableEntity || detail(body, "reason") == "" {
		t.Errorf("理由なしの期日変更: status = %d, body = %v", status, body)
	}
	if status, _ := f.member.do("PUT", mpath, map[string]any{"name": "要件定義完了", "due_date": "2026-12-15", "status": "delayed", "reason": "顧客レビューの遅れ"}); status != http.StatusOK {
		t.Errorf("理由ありの期日変更: status = %d", status)
	}
	// 別の施策のマイルストーンとしては扱えない
	other := f.admin.mustCreate("/api/activities", activityBody(f.fn1, "ACT-2", nil))
	if status, _ := f.admin.do("PUT", fmt.Sprintf("/api/activities/%d/milestones/%d", other, mid), map[string]any{"name": "x", "due_date": "2026-12-15", "status": "delayed"}); status != http.StatusNotFound {
		t.Errorf("他施策のマイルストーン: status = %d, want 404", status)
	}
	if status, _ := f.member.do("DELETE", mpath, map[string]any{"reason": "計画見直し"}); status != http.StatusNoContent {
		t.Errorf("削除: status = %d, want 204", status)
	}
}

func TestDeleteActivity(t *testing.T) {
	f := newActivityFixture(t)
	sales := f.admin.mustCreate("/api/subjects", map[string]any{"code": "4110", "name": "受託売上", "category": "revenue"})

	// 子データ（マイルストーン・ドライバー・内訳）は施策と一緒に削除され、監査ログに残る
	id := f.admin.mustCreate("/api/activities", activityBody(f.fn1, "ACT-1", nil))
	base := fmt.Sprintf("/api/activities/%d", id)
	f.admin.mustCreate(base+"/milestones", map[string]any{"name": "m", "due_date": "2026-12-01"})
	f.admin.mustCreate(base+"/drivers", map[string]any{"code": "volume", "name": "件数", "driver_kind": "kpi"})
	f.admin.mustCreate(base+"/lines", map[string]any{"subject_id": sales, "name": "売上", "expression": "volume * 1000", "formula_enabled": true, "reason": "r"})
	if status, body := f.admin.do("DELETE", base, map[string]any{"reason": "施策の統合"}); status != http.StatusNoContent {
		t.Fatalf("削除: status = %d, body = %v", status, body)
	}
	var n int
	if err := f.env.QueryRow(`
		SELECT COUNT(*) FROM audit_logs a JOIN change_sets c ON c.id = a.change_set_id
		WHERE a.action = 'delete' AND c.reason = '施策の統合'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Errorf("削除の監査ログ = %d件, want 4（施策・マイルストーン・ドライバー・計算式）", n)
	}

	// 金額データがある施策は削除できない
	id2 := f.admin.mustCreate("/api/activities", activityBody(f.fn1, "ACT-2", nil))
	var scenarioID int64
	res, err := f.env.Exec("INSERT INTO scenarios (name, fiscal_year, created_by) VALUES ('2026予算', 2026, ?)", f.env.adminID)
	if err != nil {
		t.Fatal(err)
	}
	scenarioID, _ = res.LastInsertId()
	if _, err := f.env.Exec("INSERT INTO budget_facts (scenario_id, activity_id, subject_id, target_month, amount, source) VALUES (?, ?, ?, '2026-10-01', 1000, 'manual')", scenarioID, id2, sales); err != nil {
		t.Fatal(err)
	}
	if status, _ := f.admin.do("DELETE", fmt.Sprintf("/api/activities/%d", id2), map[string]any{"reason": "r"}); status != http.StatusConflict {
		t.Errorf("金額ありの削除: status = %d, want 409", status)
	}
}

func TestListActivities(t *testing.T) {
	f := newActivityFixture(t)
	f.admin.mustCreate("/api/activities", activityBody(f.fn1, "SAAS-1", map[string]any{"name": "SaaS 月額課金"}))
	f.admin.mustCreate("/api/activities", activityBody(f.fn1, "PRJ-1", map[string]any{"name": "受託開発 A社", "activity_type": "project", "start_date": "2026-04-01", "end_date": "2026-12-31"}))
	f.admin.mustCreate("/api/activities", activityBody(f.fn2, "COST-1", map[string]any{"name": "情シス運用", "activity_type": "cost_pool"}))

	codes := func(path string) []string {
		items := f.viewer.mustGet(path)["items"].([]any)
		var out []string
		for _, it := range items {
			out = append(out, it.(map[string]any)["code"].(string))
		}
		return out
	}
	tests := map[string]string{
		"/api/activities":                                "[COST-1 PRJ-1 SAAS-1]",
		"/api/activities?activity_type=project":          "[PRJ-1]",
		fmt.Sprintf("/api/activities?unit_id=%d", f.fn2): "[COST-1]",
		"/api/activities?q=%E5%8F%97%E8%A8%97":           "[PRJ-1]", // q=受託
		"/api/activities?q=100%25":                       "[]",      // % は文字として扱う
	}
	for path, want := range tests {
		if got := fmt.Sprint(codes(path)); got != want {
			t.Errorf("GET %s = %s, want %s", path, got, want)
		}
	}
	if status, _ := f.viewer.do("GET", "/api/activities?unit_id=abc", nil); status != http.StatusBadRequest {
		t.Errorf("不正な unit_id: status = %d, want 400", status)
	}
}

func TestPriorityAndWatch(t *testing.T) {
	f := newActivityFixture(t)
	id := f.manager1.mustCreate("/api/activities", activityBody(f.fn1, "ACT-1", map[string]any{"owner_user_id": f.memberID}))
	base := fmt.Sprintf("/api/activities/%d", id)

	// 重点施策: 所管ユニットのマネージャー・FP&A が設定できる。担当者・他ユニットのマネージャーは不可
	for name, c := range map[string]*client{"担当者": f.member, "他ユニットのマネージャー": f.manager2, "閲覧者": f.viewer} {
		if status, _ := c.do("PUT", base+"/priority", map[string]any{"is_priority": true}); status != http.StatusForbidden {
			t.Errorf("%s の重点施策の設定: status = %d, want 403", name, status)
		}
	}
	if status, body := f.manager1.do("PUT", base+"/priority", map[string]any{"is_priority": true}); status != http.StatusOK || body["is_priority"] != true {
		t.Fatalf("重点施策の設定: status = %d, body = %v", status, body)
	}
	if got := f.viewer.mustGet(base); got["is_priority"] != true || got["can_manage"] != false {
		t.Errorf("閲覧者から見た施策 = is_priority %v, can_manage %v", got["is_priority"], got["can_manage"])
	}
	items := f.viewer.mustGet("/api/activities?priority=true")["items"].([]any)
	if len(items) != 1 {
		t.Errorf("重点施策の絞り込み = %d件, want 1", len(items))
	}

	// ウォッチ: だれでも付けられ、本人にだけ見える
	if status, _ := f.viewer.do("PUT", base+"/watch", nil); status != http.StatusOK {
		t.Errorf("閲覧者のウォッチ: status = %d", status)
	}
	if got := f.viewer.mustGet(base)["is_watched"]; got != true {
		t.Errorf("本人の is_watched = %v, want true", got)
	}
	if got := f.member.mustGet(base)["is_watched"]; got != false {
		t.Errorf("他の人の is_watched = %v, want false", got)
	}
	if n := len(f.viewer.mustGet("/api/activities?watched=true")["items"].([]any)); n != 1 {
		t.Errorf("ウォッチの絞り込み = %d件, want 1", n)
	}
	f.viewer.do("PUT", base+"/watch", nil) // 2回付けても1件
	if status, _ := f.viewer.do("DELETE", base+"/watch", nil); status != http.StatusOK {
		t.Errorf("ウォッチの解除: status = %d", status)
	}
	if n := len(f.viewer.mustGet("/api/activities?watched=true")["items"].([]any)); n != 0 {
		t.Errorf("解除後のウォッチ = %d件, want 0", n)
	}
	if status, _ := f.viewer.do("PUT", "/api/activities/99999/watch", nil); status != http.StatusNotFound {
		t.Errorf("存在しない施策のウォッチ: status = %d, want 404", status)
	}

	// 重点施策の変更は変更履歴に残る
	var n int
	f.env.QueryRow("SELECT COUNT(*) FROM audit_logs WHERE table_name = 'activities' AND record_id = ? AND action = 'update'", id).Scan(&n)
	if n != 1 {
		t.Errorf("重点施策の監査ログ = %d件, want 1", n)
	}
}

func TestLineConfidenceAndOutlook(t *testing.T) {
	f := newActivityFixture(t)
	sales := f.admin.mustCreate("/api/subjects", map[string]any{"code": "4110", "name": "売上", "category": "revenue"})
	id := f.admin.mustCreate("/api/activities", activityBody(f.fn1, "ACT-1", map[string]any{"owner_user_id": f.memberID}))
	lbase := fmt.Sprintf("/api/activities/%d/lines", id)

	// 既定: 段階は施策の段階（null）、見通しの種類はベース
	lid := f.member.mustCreate(lbase, map[string]any{"subject_id": sales, "name": "既存顧客"})
	line := func() map[string]any {
		for _, l := range f.viewer.mustGet(fmt.Sprintf("/api/activities/%d", id))["lines"].([]any) {
			if m := l.(map[string]any); m["id"] == float64(lid) {
				return m
			}
		}
		t.Fatal("内訳が見つからない")
		return nil
	}
	if l := line(); l["confidence_level"] != nil || l["outlook"] != "base" {
		t.Errorf("既定 = %v / %v, want null / base", l["confidence_level"], l["outlook"])
	}

	for name, tc := range map[string]struct {
		body  map[string]any
		field string
	}{
		"未登録の段階":    {map[string]any{"subject_id": sales, "name": "x", "confidence_level": "Z"}, "confidence_level"},
		"見通しの種類が不正": {map[string]any{"subject_id": sales, "name": "x", "outlook": "upside"}, "outlook"},
	} {
		if status, body := f.member.do("POST", lbase, tc.body); status != http.StatusUnprocessableEntity || detail(body, tc.field) == "" {
			t.Errorf("%s: status = %d, body = %v", name, status, body)
		}
	}
	f.member.mustCreate(lbase, map[string]any{"subject_id": sales, "name": "解約リスク", "confidence_level": "D", "outlook": "downside"})

	// 段階・見通しの種類の変更は理由が必須
	lpath := fmt.Sprintf("%s/%d", lbase, lid)
	change := map[string]any{"name": "既存顧客", "confidence_level": "B", "outlook": "addon"}
	if status, body := f.member.do("PUT", lpath, change); status != http.StatusUnprocessableEntity || detail(body, "reason") == "" {
		t.Errorf("理由なしの変更: status = %d, body = %v", status, body)
	}
	change["reason"] = "追加発注の内示"
	if status, body := f.member.do("PUT", lpath, change); status != http.StatusOK || body["confidence_level"] != "B" || body["outlook"] != "addon" {
		t.Errorf("変更: status = %d, body = %v", status, body)
	}
	// 数値入力画面の内訳にも返る
	scenario := f.admin.mustCreate("/api/scenarios", map[string]any{"name": "予算", "fiscal_year": 2026})
	for _, row := range f.viewer.mustGet(fmt.Sprintf("/api/scenarios/%d/activities/%d", scenario, id))["amounts"].([]any) {
		for _, l := range row.(map[string]any)["lines"].([]any) {
			if m := l.(map[string]any); m["id"] == float64(lid) && (m["confidence_level"] != "B" || m["outlook"] != "addon") {
				t.Errorf("数値入力の内訳 = %v", m)
			}
		}
	}
	// 内訳から使われている段階は削除できない
	var levelID int64
	f.env.QueryRow("SELECT id FROM confidence_levels WHERE code = 'D'").Scan(&levelID)
	if status, _ := f.admin.do("DELETE", fmt.Sprintf("/api/confidence-levels/%d", levelID), nil); status != http.StatusConflict {
		t.Errorf("内訳で使われている段階の削除: status = %d, want 409", status)
	}
}
