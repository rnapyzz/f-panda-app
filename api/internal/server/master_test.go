package server_test

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rnapyzz/f-panda-app/api/internal/auth"
)

// testEnv はテスト用の DB とサーバー。
type testEnv struct {
	*sql.DB
	server  *httptest.Server
	adminID int64
}

// adminClient は FP&A 管理者でログイン済みのクライアントを返す。
func adminClient(t *testing.T) (*client, *testEnv) {
	t.Helper()
	srv, db := testServer(t)
	adminID := createUser(t, db, "admin@example.com", auth.RoleFPAAdmin)
	c := newClient(t, srv)
	c.login("admin@example.com")
	return c, &testEnv{DB: db, server: srv, adminID: adminID}
}

// mustCreate は POST して 201 を確認し、作成された ID を返す。
func (c *client) mustCreate(path string, body any) int64 {
	c.t.Helper()
	status, res := c.do("POST", path, body)
	if status != http.StatusCreated {
		c.t.Fatalf("POST %s: status = %d, body = %v", path, status, res)
	}
	return int64(res["id"].(float64))
}

func (c *client) mustGet(path string) map[string]any {
	c.t.Helper()
	status, res := c.do("GET", path, nil)
	if status != http.StatusOK {
		c.t.Fatalf("GET %s: status = %d, body = %v", path, status, res)
	}
	return res
}

func detail(body map[string]any, field string) string {
	e, _ := body["error"].(map[string]any)
	d, _ := e["details"].(map[string]any)
	s, _ := d[field].(string)
	return s
}

func TestMasterPermissions(t *testing.T) {
	srv, db := testServer(t)
	createUser(t, db, "viewer@example.com", auth.RoleViewer)
	createUser(t, db, "manager@example.com", auth.RoleManager)

	anon := newClient(t, srv)
	if status, _ := anon.do("GET", "/api/organizations", nil); status != http.StatusUnauthorized {
		t.Errorf("未ログインの GET: status = %d, want 401", status)
	}

	for _, email := range []string{"viewer@example.com", "manager@example.com"} {
		c := newClient(t, srv)
		c.login(email)
		for _, path := range []string{"/api/organizations", "/api/segments", "/api/functions", "/api/subjects", "/api/users"} {
			if status, _ := c.do("GET", path, nil); status != http.StatusOK {
				t.Errorf("%s GET %s: status = %d, want 200", email, path, status)
			}
			if status, _ := c.do("POST", path, map[string]any{}); status != http.StatusForbidden {
				t.Errorf("%s POST %s: status = %d, want 403", email, path, status)
			}
		}
	}
}

func TestTreeHierarchy(t *testing.T) {
	for _, path := range []string{"/api/organizations", "/api/segments"} {
		t.Run(path, func(t *testing.T) {
			c, _ := adminClient(t)

			root := c.mustCreate(path, map[string]any{"name": "本部"})
			dept := c.mustCreate(path, map[string]any{"name": "部", "parent_id": root})
			sect := c.mustCreate(path, map[string]any{"name": "課", "parent_id": dept})
			other := c.mustCreate(path, map[string]any{"name": "別本部"})

			if lv := c.mustGet(fmt.Sprintf("%s/%d", path, sect))["level"]; lv != float64(3) {
				t.Errorf("課の level = %v, want 3", lv)
			}

			// 部を別本部の下に移動すると、配下の課の level も変わらない（同じ深さ）
			if status, body := c.do("PUT", fmt.Sprintf("%s/%d", path, dept), map[string]any{"name": "部", "parent_id": other}); status != http.StatusOK {
				t.Fatalf("移動: status = %d, body = %v", status, body)
			}
			// 部をルートに移動すると、部は 1、課は 2 になる
			if status, body := c.do("PUT", fmt.Sprintf("%s/%d", path, dept), map[string]any{"name": "部", "parent_id": nil}); status != http.StatusOK {
				t.Fatalf("ルートへ移動: status = %d, body = %v", status, body)
			}
			if lv := c.mustGet(fmt.Sprintf("%s/%d", path, dept))["level"]; lv != float64(1) {
				t.Errorf("部の level = %v, want 1", lv)
			}
			if lv := c.mustGet(fmt.Sprintf("%s/%d", path, sect))["level"]; lv != float64(2) {
				t.Errorf("課の level = %v, want 2", lv)
			}

			// 配下のノードを親にはできない（循環）
			status, body := c.do("PUT", fmt.Sprintf("%s/%d", path, dept), map[string]any{"name": "部", "parent_id": sect})
			if status != http.StatusUnprocessableEntity || detail(body, "parent_id") == "" {
				t.Errorf("循環: status = %d, body = %v", status, body)
			}
			// 自分自身も親にできない
			if status, _ := c.do("PUT", fmt.Sprintf("%s/%d", path, dept), map[string]any{"name": "部", "parent_id": dept}); status != http.StatusUnprocessableEntity {
				t.Errorf("自己参照: status = %d, want 422", status)
			}
			// 存在しない親
			if status, _ := c.do("POST", path, map[string]any{"name": "x", "parent_id": 99999}); status != http.StatusUnprocessableEntity {
				t.Errorf("存在しない親: status = %d, want 422", status)
			}
			// 名称は必須
			if status, body := c.do("POST", path, map[string]any{"name": "  "}); status != http.StatusUnprocessableEntity || detail(body, "name") == "" {
				t.Errorf("名称なし: status = %d, body = %v", status, body)
			}

			// 子がいるノードは削除できない
			if status, _ := c.do("DELETE", fmt.Sprintf("%s/%d", path, dept), nil); status != http.StatusConflict {
				t.Errorf("子ありの削除: status = %d, want 409", status)
			}
			if status, _ := c.do("DELETE", fmt.Sprintf("%s/%d", path, sect), nil); status != http.StatusNoContent {
				t.Errorf("末端の削除: status = %d, want 204", status)
			}
			if status, _ := c.do("GET", fmt.Sprintf("%s/%d", path, sect), nil); status != http.StatusNotFound {
				t.Errorf("削除後の GET: status = %d, want 404", status)
			}

			list := c.mustGet(path)["items"].([]any)
			if len(list) != 3 {
				t.Errorf("一覧の件数 = %d, want 3", len(list))
			}
		})
	}
}

func TestFunctionsRequireLeafNodes(t *testing.T) {
	c, _ := adminClient(t)

	segRoot := c.mustCreate("/api/segments", map[string]any{"name": "XXX事業"})
	segLeaf := c.mustCreate("/api/segments", map[string]any{"name": "YYYサービス", "parent_id": segRoot})
	orgRoot := c.mustCreate("/api/organizations", map[string]any{"name": "AAA本部"})
	orgLeaf := c.mustCreate("/api/organizations", map[string]any{"name": "BBB部", "parent_id": orgRoot})

	// 末端でないノードには所属できない
	status, body := c.do("POST", "/api/functions", map[string]any{"name": "CCC課", "segment_id": segRoot, "organization_id": orgLeaf})
	if status != http.StatusUnprocessableEntity || detail(body, "segment_id") == "" {
		t.Errorf("末端でないセグメント: status = %d, body = %v", status, body)
	}
	// 存在しない参照
	status, body = c.do("POST", "/api/functions", map[string]any{"name": "CCC課", "segment_id": 99999, "organization_id": orgLeaf, "owner_user_id": 99999})
	if status != http.StatusUnprocessableEntity || detail(body, "segment_id") == "" || detail(body, "owner_user_id") == "" {
		t.Errorf("存在しない参照: status = %d, body = %v", status, body)
	}

	fn := c.mustCreate("/api/functions", map[string]any{"name": "CCC課", "segment_id": segLeaf, "organization_id": orgLeaf})

	// ユニットが所属しているノードの下には子を作れない
	status, body = c.do("POST", "/api/segments", map[string]any{"name": "子", "parent_id": segLeaf})
	if status != http.StatusUnprocessableEntity || detail(body, "parent_id") == "" {
		t.Errorf("ユニットありノードへの子追加: status = %d, body = %v", status, body)
	}
	// ユニットが所属しているノードは削除できない
	if status, _ := c.do("DELETE", fmt.Sprintf("/api/organizations/%d", orgLeaf), nil); status != http.StatusConflict {
		t.Errorf("ユニットありノードの削除: status = %d, want 409", status)
	}

	// 更新
	if status, body := c.do("PUT", fmt.Sprintf("/api/functions/%d", fn), map[string]any{"name": "CCC課（改）", "segment_id": segLeaf, "organization_id": orgLeaf}); status != http.StatusOK || body["name"] != "CCC課（改）" {
		t.Errorf("更新: status = %d, body = %v", status, body)
	}
	if status, _ := c.do("DELETE", fmt.Sprintf("/api/functions/%d", fn), nil); status != http.StatusNoContent {
		t.Errorf("削除: status = %d, want 204", status)
	}
}

func TestFunctionWithActivityCannotBeDeleted(t *testing.T) {
	c, env := adminClient(t)
	seg := c.mustCreate("/api/segments", map[string]any{"name": "S"})
	org := c.mustCreate("/api/organizations", map[string]any{"name": "O"})
	fn := c.mustCreate("/api/functions", map[string]any{"name": "F", "segment_id": seg, "organization_id": org})
	if _, err := env.Exec("INSERT INTO activities (function_id, code, name, activity_type, status) VALUES (?, 'ACT-1', 'a', 'project', 'active')", fn); err != nil {
		t.Fatal(err)
	}
	if status, _ := c.do("DELETE", fmt.Sprintf("/api/functions/%d", fn), nil); status != http.StatusConflict {
		t.Errorf("施策ありの削除: status = %d, want 409", status)
	}
}

func TestSubjects(t *testing.T) {
	c, _ := adminClient(t)

	sales := c.mustCreate("/api/subjects", map[string]any{"code": "4000", "name": "売上高", "category": "revenue"})
	c.mustCreate("/api/subjects", map[string]any{"code": "4110", "name": "受託売上", "category": "revenue", "parent_id": sales})
	cost := c.mustCreate("/api/subjects", map[string]any{"code": "8110", "name": "外注費", "category": "expense"})

	tests := []struct {
		name  string
		body  map[string]any
		field string
	}{
		{"コード重複", map[string]any{"code": "4000", "name": "x", "category": "revenue"}, "code"},
		{"区分が不正", map[string]any{"code": "9999", "name": "x", "category": "asset"}, "category"},
		{"親と区分が違う", map[string]any{"code": "9998", "name": "x", "category": "expense", "parent_id": sales}, "parent_id"},
	}
	for _, tt := range tests {
		status, body := c.do("POST", "/api/subjects", tt.body)
		if status != http.StatusUnprocessableEntity || detail(body, tt.field) == "" {
			t.Errorf("%s: status = %d, body = %v", tt.name, status, body)
		}
	}

	// 循環: 売上高の親を子の受託売上にはできない
	list := c.mustGet("/api/subjects")["items"].([]any)
	var child float64
	for _, it := range list {
		if m := it.(map[string]any); m["code"] == "4110" {
			child = m["id"].(float64)
		}
	}
	status, body := c.do("PUT", fmt.Sprintf("/api/subjects/%d", sales), map[string]any{"code": "4000", "name": "売上高", "category": "revenue", "parent_id": child})
	if status != http.StatusUnprocessableEntity || detail(body, "parent_id") == "" {
		t.Errorf("循環: status = %d, body = %v", status, body)
	}
	// 子科目がある科目は削除できない
	if status, _ := c.do("DELETE", fmt.Sprintf("/api/subjects/%d", sales), nil); status != http.StatusConflict {
		t.Errorf("子ありの削除: status = %d, want 409", status)
	}
	if status, _ := c.do("DELETE", fmt.Sprintf("/api/subjects/%d", cost), nil); status != http.StatusNoContent {
		t.Errorf("削除: status = %d, want 204", status)
	}
}

func TestUsers(t *testing.T) {
	c, env := adminClient(t)

	status, body := c.do("POST", "/api/users", map[string]any{"name": "山田", "email": "Yamada@Example.com", "role": "member", "password": "member-pass-123"})
	if status != http.StatusCreated {
		t.Fatalf("作成: status = %d, body = %v", status, body)
	}
	if body["email"] != "yamada@example.com" {
		t.Errorf("email = %v, want 小文字に正規化", body["email"])
	}
	if _, ok := body["password_hash"]; ok {
		t.Error("レスポンスに password_hash が含まれている")
	}
	id := int64(body["id"].(float64))

	for name, req := range map[string]map[string]any{
		"email":    {"name": "x", "email": "yamada@example.com", "role": "member", "password": "member-pass-123"},
		"role":     {"name": "x", "email": "x@example.com", "role": "owner", "password": "member-pass-123"},
		"password": {"name": "x", "email": "y@example.com", "role": "member", "password": "short"},
	} {
		if status, body := c.do("POST", "/api/users", req); status != http.StatusUnprocessableEntity || detail(body, name) == "" {
			t.Errorf("%s の検証: status = %d, body = %v", name, status, body)
		}
	}
	if status, body := c.do("POST", "/api/users", map[string]any{"name": "x", "email": "a@b@example.com", "role": "member", "password": "member-pass-123"}); status != http.StatusUnprocessableEntity || detail(body, "email") == "" {
		t.Errorf("メール形式: status = %d, body = %v", status, body)
	}

	// 自分自身のロール変更・無効化はできない
	self := fmt.Sprintf("/api/users/%d", env.adminID)
	if status, _ := c.do("PUT", self, map[string]any{"name": "管理者", "email": "admin@example.com", "role": "viewer", "is_active": true}); status != http.StatusUnprocessableEntity {
		t.Errorf("自分のロール変更: status = %d, want 422", status)
	}
	if status, _ := c.do("PUT", self, map[string]any{"name": "管理者", "email": "admin@example.com", "role": "fpa_admin", "is_active": false}); status != http.StatusUnprocessableEntity {
		t.Errorf("自分の無効化: status = %d, want 422", status)
	}

	// パスワード再設定で既存セッションが無効になり、新しいパスワードでログインできる
	member := newClient(t, env.server)
	if status, _ := member.do("POST", "/api/auth/login", map[string]string{"email": "yamada@example.com", "password": "member-pass-123"}); status != http.StatusOK {
		t.Fatalf("member login: status = %d", status)
	}
	if status, _ := c.do("PUT", fmt.Sprintf("/api/users/%d/password", id), map[string]any{"password": "new-member-pass-456", "reason": "パスワード忘れ"}); status != http.StatusNoContent {
		t.Fatalf("パスワード再設定: status = %d", status)
	}
	if status, _ := member.do("GET", "/api/auth/me", nil); status != http.StatusUnauthorized {
		t.Errorf("再設定後の旧セッション: status = %d, want 401", status)
	}
	if status, _ := member.do("POST", "/api/auth/login", map[string]string{"email": "yamada@example.com", "password": "new-member-pass-456"}); status != http.StatusOK {
		t.Errorf("新しいパスワードでのログイン: status = %d", status)
	}

	// 無効化するとセッションが切れる
	if status, body := c.do("PUT", fmt.Sprintf("/api/users/%d", id), map[string]any{"name": "山田", "email": "yamada@example.com", "role": "member", "is_active": false}); status != http.StatusOK || body["is_active"] != false {
		t.Fatalf("無効化: status = %d, body = %v", status, body)
	}
	if status, _ := member.do("GET", "/api/auth/me", nil); status != http.StatusUnauthorized {
		t.Errorf("無効化後のセッション: status = %d, want 401", status)
	}

	// 監査ログにパスワードハッシュが残らない
	var n int
	if err := env.QueryRow("SELECT COUNT(*) FROM audit_logs WHERE table_name = 'users' AND (JSON_SEARCH(after_json, 'one', 'pbkdf2%') IS NOT NULL OR JSON_CONTAINS_PATH(after_json, 'one', '$.password_hash'))").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("監査ログにパスワードハッシュが含まれている: %d件", n)
	}
}

func TestMasterChangesAreAudited(t *testing.T) {
	c, env := adminClient(t)

	id := c.mustCreate("/api/organizations", map[string]any{"name": "経営企画部", "reason": "組織改編"})
	if status, _ := c.do("PUT", fmt.Sprintf("/api/organizations/%d", id), map[string]any{"name": "FP&A部"}); status != http.StatusOK {
		t.Fatalf("更新: status = %d", status)
	}
	if status, _ := c.do("DELETE", fmt.Sprintf("/api/organizations/%d", id), map[string]any{"reason": "廃止"}); status != http.StatusNoContent {
		t.Fatalf("削除: status = %d", status)
	}

	rows, err := env.Query(`
		SELECT a.action, cs.user_id, COALESCE(cs.reason, ''), a.before_json IS NULL, a.after_json IS NULL,
		       COALESCE(JSON_UNQUOTE(JSON_EXTRACT(a.after_json, '$.name')), '')
		FROM audit_logs a JOIN change_sets cs ON cs.id = a.change_set_id
		WHERE a.table_name = 'organizations' AND a.record_id = ?
		ORDER BY a.id`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	type logRow struct {
		action               string
		userID               int64
		reason               string
		beforeNull, afterNil bool
		afterName            string
	}
	var got []logRow
	for rows.Next() {
		var r logRow
		if err := rows.Scan(&r.action, &r.userID, &r.reason, &r.beforeNull, &r.afterNil, &r.afterName); err != nil {
			t.Fatal(err)
		}
		got = append(got, r)
	}
	want := []logRow{
		{"insert", env.adminID, "組織改編", true, false, "経営企画部"},
		{"update", env.adminID, "", false, false, "FP&A部"},
		{"delete", env.adminID, "廃止", false, true, ""},
	}
	if len(got) != len(want) {
		t.Fatalf("監査ログ = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("監査ログ[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestMasterRejectsMalformedJSON(t *testing.T) {
	c, _ := adminClient(t)
	req, _ := http.NewRequest("POST", c.base+"/api/organizations", strings.NewReader(`{"name":"x","unknown":1}`))
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("未知のフィールド: status = %d, want 400", res.StatusCode)
	}
}

func TestUnitTypes(t *testing.T) {
	c, _ := adminClient(t)
	seg := c.mustCreate("/api/segments", map[string]any{"name": "S"})
	org := c.mustCreate("/api/organizations", map[string]any{"name": "O"})
	body := func(name, unitType string) map[string]any {
		b := map[string]any{"name": name, "segment_id": seg, "organization_id": org}
		if unitType != "" {
			b["unit_type"] = unitType
		}
		return b
	}

	// 省略時はサービス
	id := c.mustCreate("/api/functions", body("SaaSサービス", ""))
	if got := c.mustGet(fmt.Sprintf("/api/functions/%d", id))["unit_type"]; got != "service" {
		t.Errorf("既定の種別 = %v, want service", got)
	}
	cc := c.mustCreate("/api/functions", body("事業共通経費", "cost_center"))
	if got := c.mustGet(fmt.Sprintf("/api/functions/%d", cc))["unit_type"]; got != "cost_center" {
		t.Errorf("共通費 = %v", got)
	}
	if status, res := c.do("POST", "/api/functions", body("x", "profit")); status != http.StatusUnprocessableEntity || detail(res, "unit_type") == "" {
		t.Errorf("不正な種別: status = %d, body = %v", status, res)
	}
	if status, res := c.do("PUT", fmt.Sprintf("/api/functions/%d", cc), body("人事部", "corporate")); status != http.StatusOK || res["unit_type"] != "corporate" {
		t.Errorf("種別の変更: status = %d, body = %v", status, res)
	}
}
