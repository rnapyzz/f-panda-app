package server_test

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// download はエクスポートの CSV を取得する（BOM を除いた本文を返す）。
func (c *client) download(path string) string {
	c.t.Helper()
	res, err := c.http.Get(c.base + path)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		c.t.Fatalf("GET %s: status = %d, body = %s", path, res.StatusCode, b)
	}
	if !strings.HasPrefix(string(b), "\xef\xbb\xbf") {
		c.t.Errorf("GET %s: BOM がない", path)
	}
	if cd := res.Header.Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
		c.t.Errorf("GET %s: Content-Disposition = %q", path, cd)
	}
	return strings.TrimPrefix(string(b), "\xef\xbb\xbf")
}

func resultCounts(body map[string]any) string {
	return fmt.Sprintf("inserted=%v updated=%v unchanged=%v", body["inserted"], body["updated"], body["unchanged"])
}

func rowErrors(body map[string]any) []string {
	var out []string
	e, _ := body["error"].(map[string]any)
	rows, _ := e["rows"].([]any)
	for _, r := range rows {
		m := r.(map[string]any)
		out = append(out, fmt.Sprintf("%v:%v", m["line"], m["message"]))
	}
	return out
}

func TestTreeImportExport(t *testing.T) {
	c, _ := adminClient(t)
	c.mustCreate("/api/organizations", map[string]any{"name": "本部", "code": "HQ"})

	// 同じ CSV 内の新しい親の下に子を追加できる。既存の名称も更新できる
	csv := "code,name,parent_code,sort_order\n" +
		"DEPT-A,営業部,HQ,1\n" +
		"SEC-A1,営業一課,DEPT-A,0\n" +
		"HQ,本社,,0\n"
	status, body := c.upload("/api/organizations/import?dry_run=true", csv, "")
	if status != http.StatusOK || resultCounts(body) != "inserted=2 updated=1 unchanged=0" {
		t.Fatalf("dry run: status = %d, body = %v", status, body)
	}
	if n := len(c.mustGet("/api/organizations")["items"].([]any)); n != 1 {
		t.Fatalf("dry run なのに保存された: %d 件", n)
	}
	status, body = c.upload("/api/organizations/import", csv, "組織の一括登録")
	if status != http.StatusOK || resultCounts(body) != "inserted=2 updated=1 unchanged=0" {
		t.Fatalf("取込: status = %d, body = %v", status, body)
	}
	levels := map[string]any{}
	for _, it := range c.mustGet("/api/organizations")["items"].([]any) {
		m := it.(map[string]any)
		levels[m["code"].(string)] = m["level"]
	}
	if fmt.Sprint(levels) != "map[DEPT-A:2 HQ:1 SEC-A1:3]" {
		t.Errorf("階層レベル = %v", levels)
	}

	// エクスポート → そのまま取込で変更なし（往復できる）
	exported := c.download("/api/organizations/export")
	if !strings.Contains(exported, "SEC-A1,営業一課,DEPT-A,0\r\n") {
		t.Errorf("エクスポート = %q", exported)
	}
	status, body = c.upload("/api/organizations/import", exported, "再取込")
	if status != http.StatusOK || resultCounts(body) != "inserted=0 updated=0 unchanged=3" {
		t.Errorf("往復: status = %d, body = %v", status, body)
	}

	// 親を付け替えると配下の階層レベルも変わる（課は CSV に含めない）
	status, body = c.upload("/api/organizations/import", "code,name,parent_code,sort_order\nDEPT-A,営業部,,1\n", "組織改編")
	if status != http.StatusOK || resultCounts(body) != "inserted=0 updated=1 unchanged=0" {
		t.Fatalf("付け替え: status = %d, body = %v", status, body)
	}
	for _, it := range c.mustGet("/api/organizations")["items"].([]any) {
		if m := it.(map[string]any); m["code"] == "SEC-A1" && m["level"] != float64(2) {
			t.Errorf("付け替え後の課の level = %v, want 2", m["level"])
		}
	}

	// エラー: 循環・存在しない親・コード形式・重複・ユニットが所属するノードへの子追加
	seg := c.mustCreate("/api/segments", map[string]any{"name": "S"})
	sec := int64(0)
	for _, it := range c.mustGet("/api/organizations")["items"].([]any) {
		if m := it.(map[string]any); m["code"] == "SEC-A1" {
			sec = int64(m["id"].(float64))
		}
	}
	c.mustCreate("/api/units", map[string]any{"name": "U", "segment_id": seg, "organization_id": sec})
	bad := "code,name,parent_code,sort_order\n" +
		"HQ,本社,SEC-A1,0\n" + // 2: ユニットが所属する SEC-A1 の下（形式エラーがあるため、ここでは検出されない）
		"X1,x,NOPE,0\n" + // 3: 親がない
		"人事,x,,0\n" + // 4: コード形式
		"Y1,y,Y2,0\n" + // 5: 循環
		"Y2,y,Y1,0\n" + // 6: 循環
		"DEPT-A,営業部,DEPT-A,0\n" // 7: 自分自身
	// 行の形式のエラーがあれば、構造（親・循環）の確認の前に止まる
	status, body = c.upload("/api/organizations/import", bad, "r")
	errs := rowErrors(body)
	if status != http.StatusUnprocessableEntity || len(errs) != 2 {
		t.Fatalf("エラー: status = %d, rows = %v", status, errs)
	}
	joined := strings.Join(errs, "\n")
	for _, want := range []string{"4:コード", "7:自分自身"} {
		if !strings.Contains(joined, want) {
			t.Errorf("エラーに %q がない: %v", want, errs)
		}
	}
	// 形式エラーを除いたものでも、存在しない親と循環は拒否される
	status, body = c.upload("/api/organizations/import", "code,name,parent_code,sort_order\nX1,x,NOPE,0\n", "r")
	if status != http.StatusUnprocessableEntity || !strings.Contains(strings.Join(rowErrors(body), ""), "見つかりません") {
		t.Errorf("存在しない親: status = %d, body = %v", status, body)
	}
	status, body = c.upload("/api/organizations/import", "code,name,parent_code,sort_order\nY1,y,Y2,0\nY2,y,Y1,0\n", "r")
	if status != http.StatusUnprocessableEntity || !strings.Contains(strings.Join(rowErrors(body), ""), "循環") {
		t.Errorf("循環: status = %d, body = %v", status, body)
	}
	status, body = c.upload("/api/organizations/import", "code,name,parent_code,sort_order\nZ1,z,SEC-A1,0\n", "r")
	if status != http.StatusUnprocessableEntity || !strings.Contains(strings.Join(rowErrors(body), ""), "ユニットが所属") {
		t.Errorf("ユニット所属ノードの下: status = %d, body = %v", status, body)
	}
	// ヘッダー違い・理由なし
	if status, _ := c.upload("/api/organizations/import", "code,name\nA,B\n", "r"); status != http.StatusBadRequest {
		t.Errorf("ヘッダー違い: status = %d, want 400", status)
	}
	if status, body := c.upload("/api/organizations/import", "code,name,parent_code,sort_order\nA,B,,0\n", ""); status != http.StatusUnprocessableEntity || detail(body, "reason") == "" {
		t.Errorf("理由なし: status = %d, body = %v", status, body)
	}
}

func TestUnitSubjectUserImport(t *testing.T) {
	f := newActivityFixture(t)
	a := f.admin

	// ユニット: セグメント・組織・担当者をコード・メールアドレスで指定
	segs := a.download("/api/segments/export")
	orgs := a.download("/api/organizations/export")
	segCode := strings.Split(strings.Split(segs, "\r\n")[1], ",")[0]
	orgCode := strings.Split(strings.Split(orgs, "\r\n")[1], ",")[0]
	csv := "code,name,unit_type,segment_code,organization_code,owner_email\n" +
		fmt.Sprintf("CC-1,共通経費,cost_center,%s,%s,manager1@example.com\n", segCode, orgCode)
	status, body := a.upload("/api/units/import", csv, "ユニット追加")
	if status != http.StatusOK || resultCounts(body) != "inserted=1 updated=0 unchanged=0" {
		t.Fatalf("ユニット取込: status = %d, body = %v", status, body)
	}
	exported := a.download("/api/units/export")
	if !strings.Contains(exported, "CC-1,共通経費,cost_center,"+segCode+","+orgCode+",manager1@example.com") {
		t.Errorf("ユニットのエクスポート = %q", exported)
	}
	if status, body := a.upload("/api/units/import", exported, "再取込"); status != http.StatusOK || body["updated"] != float64(0) || body["inserted"] != float64(0) {
		t.Errorf("ユニットの往復: status = %d, body = %v", status, body)
	}
	bad := "code,name,unit_type,segment_code,organization_code,owner_email\n" +
		fmt.Sprintf("CC-2,x,service,NOPE,%s,\n", orgCode) +
		fmt.Sprintf("CC-3,x,service,%s,%s,nobody@example.com\n", segCode, orgCode) +
		fmt.Sprintf("CC-4,x,profit,%s,%s,\n", segCode, orgCode)
	status, body = a.upload("/api/units/import", bad, "r")
	if errs := rowErrors(body); status != http.StatusUnprocessableEntity || len(errs) != 3 {
		t.Errorf("ユニットのエラー: status = %d, rows = %v", status, errs)
	}

	// 勘定科目: 親と区分が違えばエラー
	csv = "code,name,category,parent_code,sort_order\n4000,売上高,revenue,,1\n4100,受託売上,revenue,4000,1\n8000,外注費,expense,4000,2\n"
	status, body = a.upload("/api/subjects/import", csv, "科目登録")
	if errs := rowErrors(body); status != http.StatusUnprocessableEntity || len(errs) != 1 || !strings.Contains(errs[0], "4:") {
		t.Fatalf("区分違い: status = %d, rows = %v", status, errs)
	}
	csv = "code,name,category,parent_code,sort_order\n4100,受託売上,revenue,4000,1\n4000,売上高,revenue,,1\n"
	if status, body := a.upload("/api/subjects/import", csv, "科目登録"); status != http.StatusOK || resultCounts(body) != "inserted=2 updated=0 unchanged=0" {
		t.Errorf("科目取込（子が先の行でも可）: status = %d, body = %v", status, body)
	}
	if status, body := a.upload("/api/subjects/import", a.download("/api/subjects/export"), "再取込"); status != http.StatusOK || body["unchanged"] != float64(2) {
		t.Errorf("科目の往復: status = %d, body = %v", status, body)
	}

	// ユーザー: 追加したユーザーはパスワード未設定でログインできない
	csv = "email,name,role,is_active\nnew@example.com,新規 太郎,member,true\nmember2@example.com,担当 二郎,member,false\n"
	status, body = a.upload("/api/users/import", csv, "ユーザー登録")
	if status != http.StatusOK || resultCounts(body) != "inserted=1 updated=1 unchanged=0" {
		t.Fatalf("ユーザー取込: status = %d, body = %v", status, body)
	}
	var created map[string]any
	for _, it := range a.mustGet("/api/users")["items"].([]any) {
		if m := it.(map[string]any); m["email"] == "new@example.com" {
			created = m
		}
	}
	if created == nil || created["has_password"] != false {
		t.Fatalf("追加したユーザー = %v", created)
	}
	anon := newClient(t, f.env.server)
	if status, _ := anon.do("POST", "/api/auth/login", map[string]string{"email": "new@example.com", "password": "!"}); status != http.StatusUnauthorized {
		t.Errorf("パスワード未設定のログイン: status = %d, want 401", status)
	}
	// 無効化したユーザーのセッションは切れる
	if status, _ := f.member2.do("GET", "/api/auth/me", nil); status != http.StatusUnauthorized {
		t.Errorf("無効化されたユーザーのセッション: status = %d, want 401", status)
	}
	// パスワードを設定すればログインできる
	a.do("PUT", fmt.Sprintf("/api/users/%d/password", int64(created["id"].(float64))), map[string]any{"password": "new-user-pass-123"})
	if status, _ := anon.do("POST", "/api/auth/login", map[string]string{"email": "new@example.com", "password": "new-user-pass-123"}); status != http.StatusOK {
		t.Errorf("パスワード設定後のログイン: status = %d", status)
	}
	// 自分自身の無効化はできない
	status, body = a.upload("/api/users/import", "email,name,role,is_active\nadmin@example.com,管理者,fpa_admin,false\n", "r")
	if status != http.StatusUnprocessableEntity {
		t.Errorf("自分の無効化: status = %d, body = %v", status, body)
	}
	// 閲覧者はエクスポートできるが、取込はできない
	f.viewer.download("/api/users/export")
	if status, _ := f.viewer.upload("/api/users/import", csv, "r"); status != http.StatusForbidden {
		t.Errorf("閲覧者の取込: status = %d, want 403", status)
	}
}

func TestActivityImportExport(t *testing.T) {
	f := newActivityFixture(t)
	a := f.admin
	unitCode := strings.Split(strings.Split(a.download("/api/units/export"), "\r\n")[1], ",")[0]
	header := "code,name,unit_code,activity_type,status,start_date,end_date,owner_email,confidence_level,assumptions,external_codes\n"

	// code が空なら自動採番して追加。外部コードも登録
	csv := header +
		fmt.Sprintf(",新規受託 3件想定,%s,project,planned,2026-04-01,2027-03-31,member@example.com,C,下期に2件受注が前提,P-100 P-101\n", unitCode) +
		fmt.Sprintf("SAAS-X,SaaS X,%s,recurring,in_progress,,,,,,\n", unitCode)
	status, body := a.upload("/api/activities/import", csv, "期初計画の登録")
	if status != http.StatusOK || resultCounts(body) != "inserted=2 updated=0 unchanged=0" {
		t.Fatalf("取込: status = %d, body = %v", status, body)
	}
	exported := a.download("/api/activities/export")
	if !strings.Contains(exported, "ACT-0001,新規受託 3件想定,"+unitCode+",project,planned,2026-04-01,2027-03-31,member@example.com,C,下期に2件受注が前提,P-100 P-101") {
		t.Errorf("エクスポート = %q", exported)
	}
	// 往復で変更なし
	if status, body := a.upload("/api/activities/import", exported, "再取込"); status != http.StatusOK || resultCounts(body) != "inserted=0 updated=0 unchanged=2" {
		t.Errorf("往復: status = %d, body = %v", status, body)
	}
	// 確度の更新と外部コードの追加（記載のない外部コードは外さない）
	csv = header + fmt.Sprintf("ACT-0001,新規受託 3件想定,%s,project,in_progress,2026-04-01,2027-03-31,member@example.com,B,下期に2件受注が前提,P-102\n", unitCode)
	if status, body := a.upload("/api/activities/import", csv, "受注見込みの更新"); status != http.StatusOK || resultCounts(body) != "inserted=0 updated=1 unchanged=0" {
		t.Fatalf("更新: status = %d, body = %v", status, body)
	}
	items := a.mustGet("/api/activities?q=ACT-0001")["items"].([]any)
	id := int64(items[0].(map[string]any)["id"].(float64))
	d := a.mustGet(fmt.Sprintf("/api/activities/%d", id))
	if d["confidence_level"] != "B" || len(d["external_codes"].([]any)) != 3 {
		t.Errorf("更新後 = confidence_level %v, external_codes %v", d["confidence_level"], d["external_codes"])
	}

	// エラー: 存在しないユニット・担当者、外部コードの衝突、タイプ不正
	bad := header +
		"X-1,x,NOPE,recurring,planned,,,,,,\n" +
		fmt.Sprintf("X-2,x,%s,recurring,planned,,,nobody@example.com,,,\n", unitCode) +
		fmt.Sprintf("SAAS-X,SaaS X,%s,recurring,in_progress,,,,,,P-100\n", unitCode) +
		fmt.Sprintf("X-3,x,%s,flow,planned,,,,,,\n", unitCode) +
		fmt.Sprintf("X-4,x,%s,recurring,planned,,,,,,SAAS-X\n", unitCode) +
		fmt.Sprintf("X-5,x,%s,recurring,planned,,,,Z,,\n", unitCode)
	status, body = a.upload("/api/activities/import", bad, "r")
	errs := rowErrors(body)
	if status != http.StatusUnprocessableEntity || len(errs) != 6 {
		t.Errorf("エラー: status = %d, rows = %v", status, errs)
	}
	if !strings.Contains(strings.Join(errs, "\n"), "4:外部コード P-100 は別の施策に登録されています") {
		t.Errorf("外部コードの衝突のエラーがない: %v", errs)
	}
	// FP&A 以外は取込できない（マネージャーでも）
	if status, _ := f.manager1.upload("/api/activities/import", csv, "r"); status != http.StatusForbidden {
		t.Errorf("マネージャーの取込: status = %d, want 403", status)
	}
}
