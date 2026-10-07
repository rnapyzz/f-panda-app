package master

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/rnapyzz/f-panda-app/api/internal/audit"
	"github.com/rnapyzz/f-panda-app/api/internal/auth"
	"github.com/rnapyzz/f-panda-app/api/internal/codes"
	"github.com/rnapyzz/f-panda-app/api/internal/csvio"
	"github.com/rnapyzz/f-panda-app/api/internal/dbx"
	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
)

// CSV インポート・エクスポート。取込は「追加と更新」のみで、CSV にない行は削除しない。
// 行はコード（ユーザーはメールアドレス）で既存のデータと結びつける。

// unusablePasswordHash は、CSV で追加したユーザーの仮のパスワードハッシュ。どのパスワードとも一致しない。
// FP&A が画面でパスワードを設定するまでログインできない。
const unusablePasswordHash = "!"

// --- 組織・セグメント ---

var treeColumns = []string{"code", "name", "parent_code", "sort_order"}

// export は GET /api/{organizations|segments}/export。親が子より先に来る順で書き出す。
func (t *treeHandler) export(w http.ResponseWriter, r *http.Request) error {
	rows, err := t.db.QueryContext(r.Context(), `
		SELECT n.code, n.name, COALESCE(p.code, ''), n.sort_order
		FROM `+t.table+` n LEFT JOIN `+t.table+` p ON p.id = n.parent_id
		ORDER BY n.level, n.sort_order, n.id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var out [][]string
	for rows.Next() {
		var code, name, parent string
		var sortOrder int
		if err := rows.Scan(&code, &name, &parent, &sortOrder); err != nil {
			return err
		}
		out = append(out, []string{code, name, parent, strconv.Itoa(sortOrder)})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return csvio.WriteCSV(w, t.table, treeColumns, out)
}

type treeImportItem struct {
	line       int
	code       string
	name       string
	parentCode string
	sortOrder  int
}

// importCSV は POST /api/{organizations|segments}/import。
func (t *treeHandler) importCSV(w http.ResponseWriter, r *http.Request) error {
	up, err := csvio.ReadUpload(w, r)
	if err != nil {
		return err
	}
	rows, err := csvio.Parse(up.Data, treeColumns)
	if err != nil {
		return err
	}
	ctx := r.Context()
	result := csvio.Result{DryRun: up.DryRun, Rows: len(rows)}

	err = inTx(ctx, t.db, r, up.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		existing, err := t.lockAll(ctx, tx)
		if err != nil {
			return err
		}
		codeOf := map[int64]string{}
		for _, n := range existing {
			codeOf[n.ID] = n.Code
		}

		// 行の検証
		errs := &csvio.RowErrors{}
		items := map[string]treeImportItem{}
		var order []string
		for _, row := range rows {
			v := httpx.Validator{}
			it := treeImportItem{line: row.Line, code: row.Get("code"), parentCode: row.Get("parent_code")}
			it.name = v.Text("name", "名称", row.Get("name"), maxNameLen)
			if !codes.Pattern.MatchString(it.code) {
				v.Add("code", "コード "+strconv.Quote(it.code)+": "+codes.PatternMessage)
			}
			if s := row.Get("sort_order"); s != "" {
				n, err := strconv.Atoi(s)
				if err != nil {
					v.Add("sort_order", "表示順 "+strconv.Quote(s)+" は整数で入力してください")
				}
				it.sortOrder = n
			}
			if it.parentCode == it.code && it.code != "" {
				v.Add("parent_code", "自分自身を親にはできません")
			}
			if _, dup := items[it.code]; dup && it.code != "" {
				v.Add("code", "コード "+it.code+" が CSV 内で重複しています")
			}
			if err := v.Err(); err != nil {
				errs.AddDetails(row.Line, err)
				continue
			}
			items[it.code] = it
			order = append(order, it.code)
		}
		if err := errs.Err(); err != nil {
			return err
		}

		// 取込後の親子関係（コード → 親のコード）
		parentOf := map[string]string{}
		for _, n := range existing {
			if n.ParentID != nil {
				parentOf[n.Code] = codeOf[*n.ParentID]
			} else {
				parentOf[n.Code] = ""
			}
		}
		for _, it := range items {
			parentOf[it.code] = it.parentCode
		}
		for _, c := range order {
			it := items[c]
			if it.parentCode != "" {
				if _, ok := parentOf[it.parentCode]; !ok {
					errs.Add(it.line, "親のコード %s が見つかりません", it.parentCode)
				}
			}
		}
		if err := errs.Err(); err != nil {
			return err
		}
		depth, cyclic := depths(parentOf)
		for _, c := range order {
			if cyclic[c] {
				errs.Add(items[c].line, "親子関係が循環しています（%s）", c)
			}
		}
		// ユニットが所属しているノードの下には子を置けない
		unitNodes, err := t.nodesWithUnits(ctx, tx)
		if err != nil {
			return err
		}
		for _, c := range order {
			if p := items[c].parentCode; p != "" && unitNodes[p] {
				errs.Add(items[c].line, "ユニットが所属している%s %s の下には追加できません", t.label, p)
			}
		}
		if err := errs.Err(); err != nil {
			return err
		}

		// 親が先になるよう、深さの順に保存する
		sort.SliceStable(order, func(i, j int) bool { return depth[order[i]] < depth[order[j]] })
		idOf := map[string]int64{}
		for _, n := range existing {
			idOf[n.Code] = n.ID
		}
		for _, c := range order {
			it := items[c]
			var parentID *int64
			if it.parentCode != "" {
				id := idOf[it.parentCode]
				parentID = &id
			}
			level := depth[c] + 1
			before, exists := existing[c]
			if !exists {
				res, err := tx.ExecContext(ctx,
					"INSERT INTO "+t.table+" (parent_id, code, name, level, sort_order) VALUES (?, ?, ?, ?, ?)",
					dbx.NullInt64(parentID), c, it.name, level, it.sortOrder)
				if err != nil {
					return err
				}
				id, _ := res.LastInsertId()
				idOf[c] = id
				created, err := t.find(ctx, tx, id, "")
				if err != nil {
					return err
				}
				if err := rec.Insert(ctx, t.table, id, created); err != nil {
					return err
				}
				result.Inserted++
				continue
			}
			if sameParent(before.ParentID, parentID) && before.Name == it.name && before.SortOrder == it.sortOrder {
				result.Unchanged++
				continue
			}
			if _, err := tx.ExecContext(ctx,
				"UPDATE "+t.table+" SET parent_id = ?, name = ?, sort_order = ?, level = ? WHERE id = ?",
				dbx.NullInt64(parentID), it.name, it.sortOrder, level, before.ID); err != nil {
				return err
			}
			after, err := t.find(ctx, tx, before.ID, "")
			if err != nil {
				return err
			}
			if err := rec.Update(ctx, t.table, before.ID, before, after); err != nil {
				return err
			}
			result.Updated++
		}
		// 移動したノードの配下（CSV に含まれない行）の階層レベルも合わせる
		if err := t.recalcLevels(ctx, tx); err != nil {
			return err
		}
		if up.DryRun {
			return csvio.ErrDryRun
		}
		return nil
	})
	return csvio.Finish(w, result, err)
}

// lockAll は全ノードを行ロック付きで読み込み、コード → ノードで返す。
func (t *treeHandler) lockAll(ctx context.Context, tx *sql.Tx) (map[string]treeNode, error) {
	rows, err := tx.QueryContext(ctx, t.selectSQL()+" FOR UPDATE")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]treeNode{}
	for rows.Next() {
		n, err := scanTreeNode(rows)
		if err != nil {
			return nil, err
		}
		out[n.Code] = n
	}
	return out, rows.Err()
}

// nodesWithUnits はユニットが所属しているノードのコードを返す。
func (t *treeHandler) nodesWithUnits(ctx context.Context, tx *sql.Tx) (map[string]bool, error) {
	rows, err := tx.QueryContext(ctx, "SELECT DISTINCT n.code FROM "+t.table+" n JOIN units u ON u."+t.unitColumn+" = n.id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out[c] = true
	}
	return out, rows.Err()
}

// recalcLevels は親子関係から全ノードの階層レベルを計算し直す。
func (t *treeHandler) recalcLevels(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, "SELECT id, parent_id, level FROM "+t.table)
	if err != nil {
		return err
	}
	type node struct {
		parent sql.NullInt64
		level  int
	}
	nodes := map[int64]node{}
	for rows.Next() {
		var id int64
		var n node
		if err := rows.Scan(&id, &n.parent, &n.level); err != nil {
			rows.Close()
			return err
		}
		nodes[id] = n
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	var levelOf func(id int64, guard int) int
	levelOf = func(id int64, guard int) int {
		n := nodes[id]
		if !n.parent.Valid || guard > len(nodes) {
			return 1
		}
		return levelOf(n.parent.Int64, guard+1) + 1
	}
	for id, n := range nodes {
		if lv := levelOf(id, 0); lv != n.level {
			if _, err := tx.ExecContext(ctx, "UPDATE "+t.table+" SET level = ? WHERE id = ?", lv, id); err != nil {
				return err
			}
		}
	}
	return nil
}

// depths は親のコードの対応から、各コードの深さ（ルートが 0）と循環しているコードを返す。
func depths(parentOf map[string]string) (map[string]int, map[string]bool) {
	depth := map[string]int{}
	cyclic := map[string]bool{}
	for c := range parentOf {
		seen := map[string]bool{c: true}
		d := 0
		for p := parentOf[c]; p != ""; p = parentOf[p] {
			if seen[p] {
				cyclic[c] = true
				break
			}
			seen[p] = true
			d++
		}
		depth[c] = d
	}
	return depth, cyclic
}

// --- ユニット ---

var unitColumns = []string{"code", "name", "unit_type", "segment_code", "organization_code", "owner_email"}

// exportUnits は GET /api/units/export。
func (h *Handler) exportUnits(w http.ResponseWriter, r *http.Request) error {
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT u.code, u.name, u.unit_type, s.code, o.code, COALESCE(usr.email, '')
		FROM units u
		JOIN segments s ON s.id = u.segment_id
		JOIN organizations o ON o.id = u.organization_id
		LEFT JOIN users usr ON usr.id = u.owner_user_id
		ORDER BY u.code`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var out [][]string
	for rows.Next() {
		rec := make([]string, 6)
		if err := rows.Scan(&rec[0], &rec[1], &rec[2], &rec[3], &rec[4], &rec[5]); err != nil {
			return err
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return csvio.WriteCSV(w, "units", unitColumns, out)
}

// importUnits は POST /api/units/import。所属先のセグメント・組織は末端のノードであること。
func (h *Handler) importUnits(w http.ResponseWriter, r *http.Request) error {
	up, err := csvio.ReadUpload(w, r)
	if err != nil {
		return err
	}
	rows, err := csvio.Parse(up.Data, unitColumns)
	if err != nil {
		return err
	}
	ctx := r.Context()
	result := csvio.Result{DryRun: up.DryRun, Rows: len(rows)}

	err = inTx(ctx, h.db, r, up.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		segments, err := leafIndex(ctx, tx, "segments")
		if err != nil {
			return err
		}
		orgs, err := leafIndex(ctx, tx, "organizations")
		if err != nil {
			return err
		}
		users, err := emailIndex(ctx, tx)
		if err != nil {
			return err
		}

		errs := &csvio.RowErrors{}
		type item struct {
			line int
			req  unitRequest
			name string
		}
		var items []item
		seen := map[string]bool{}
		for _, row := range rows {
			req := unitRequest{Code: row.Get("code"), Name: row.Get("name"), UnitType: row.Get("unit_type")}
			v := httpx.Validator{}
			if s, ok := segments[row.Get("segment_code")]; !ok {
				v.Add("segment_id", "セグメントのコード "+strconv.Quote(row.Get("segment_code"))+" が見つかりません")
			} else if !s.leaf {
				v.Add("segment_id", "セグメント "+row.Get("segment_code")+" は末端ではありません（ユニットは末端のセグメントに所属します）")
			} else {
				req.SegmentID = s.id
			}
			if o, ok := orgs[row.Get("organization_code")]; !ok {
				v.Add("organization_id", "組織のコード "+strconv.Quote(row.Get("organization_code"))+" が見つかりません")
			} else if !o.leaf {
				v.Add("organization_id", "組織 "+row.Get("organization_code")+" は末端ではありません（ユニットは末端の組織に所属します）")
			} else {
				req.OrganizationID = o.id
			}
			if e := auth.NormalizeEmail(row.Get("owner_email")); e != "" {
				if id, ok := users[e]; ok {
					req.OwnerUserID = &id
				} else {
					v.Add("owner_user_id", "担当者のメールアドレス "+e+" のユーザーが見つかりません")
				}
			}
			if seen[req.Code] {
				v.Add("code", "コード "+req.Code+" が CSV 内で重複しています")
			}
			seen[req.Code] = true
			if err := v.Err(); err != nil {
				errs.AddDetails(row.Line, err)
				continue
			}
			name, err := validateUnitRequest(&req, false)
			if err != nil {
				errs.AddDetails(row.Line, err)
				continue
			}
			items = append(items, item{line: row.Line, req: req, name: name})
		}
		if err := errs.Err(); err != nil {
			return err
		}

		for _, it := range items {
			before, err := scanUnit(tx.QueryRowContext(ctx, unitSelect+" WHERE code = ? FOR UPDATE", it.req.Code))
			if errors.Is(err, sql.ErrNoRows) {
				res, err := tx.ExecContext(ctx,
					"INSERT INTO units (code, name, unit_type, segment_id, organization_id, owner_user_id) VALUES (?, ?, ?, ?, ?, ?)",
					it.req.Code, it.name, it.req.UnitType, it.req.SegmentID, it.req.OrganizationID, dbx.NullInt64(it.req.OwnerUserID))
				if err != nil {
					return err
				}
				id, _ := res.LastInsertId()
				created, err := findUnit(ctx, tx, id, "")
				if err != nil {
					return err
				}
				if err := rec.Insert(ctx, "units", id, created); err != nil {
					return err
				}
				result.Inserted++
				continue
			}
			if err != nil {
				return err
			}
			if before.Name == it.name && before.UnitType == it.req.UnitType && before.SegmentID == it.req.SegmentID &&
				before.OrganizationID == it.req.OrganizationID && sameParent(before.OwnerUserID, it.req.OwnerUserID) {
				result.Unchanged++
				continue
			}
			if _, err := tx.ExecContext(ctx,
				"UPDATE units SET name = ?, unit_type = ?, segment_id = ?, organization_id = ?, owner_user_id = ? WHERE id = ?",
				it.name, it.req.UnitType, it.req.SegmentID, it.req.OrganizationID, dbx.NullInt64(it.req.OwnerUserID), before.ID); err != nil {
				return err
			}
			after, err := findUnit(ctx, tx, before.ID, "")
			if err != nil {
				return err
			}
			if err := rec.Update(ctx, "units", before.ID, before, after); err != nil {
				return err
			}
			result.Updated++
		}
		if up.DryRun {
			return csvio.ErrDryRun
		}
		return nil
	})
	return csvio.Finish(w, result, err)
}

type nodeRef struct {
	id   int64
	leaf bool
}

// leafIndex はコード → （ID, 末端か）を返す。
func leafIndex(ctx context.Context, tx *sql.Tx, table string) (map[string]nodeRef, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT n.id, n.code, NOT EXISTS (SELECT 1 FROM `+table+` c WHERE c.parent_id = n.id)
		FROM `+table+` n FOR SHARE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]nodeRef{}
	for rows.Next() {
		var ref nodeRef
		var code string
		if err := rows.Scan(&ref.id, &code, &ref.leaf); err != nil {
			return nil, err
		}
		out[code] = ref
	}
	return out, rows.Err()
}

func emailIndex(ctx context.Context, tx *sql.Tx) (map[string]int64, error) {
	rows, err := tx.QueryContext(ctx, "SELECT email, id FROM users")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var email string
		var id int64
		if err := rows.Scan(&email, &id); err != nil {
			return nil, err
		}
		out[email] = id
	}
	return out, rows.Err()
}

// --- 勘定科目 ---

var subjectColumns = []string{"code", "name", "category", "parent_code", "sort_order"}

// exportSubjects は GET /api/subjects/export。親が子より先に来る順で書き出す。
func (h *Handler) exportSubjects(w http.ResponseWriter, r *http.Request) error {
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT s.code, s.name, s.category, COALESCE(p.code, ''), s.sort_order
		FROM subjects s LEFT JOIN subjects p ON p.id = s.parent_id
		ORDER BY s.parent_id IS NOT NULL, s.category, s.sort_order, s.code`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var out [][]string
	for rows.Next() {
		rec := make([]string, 5)
		var sortOrder int
		if err := rows.Scan(&rec[0], &rec[1], &rec[2], &rec[3], &sortOrder); err != nil {
			return err
		}
		rec[4] = strconv.Itoa(sortOrder)
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return csvio.WriteCSV(w, "subjects", subjectColumns, out)
}

// importSubjects は POST /api/subjects/import。親科目は同じ区分であること。
func (h *Handler) importSubjects(w http.ResponseWriter, r *http.Request) error {
	up, err := csvio.ReadUpload(w, r)
	if err != nil {
		return err
	}
	rows, err := csvio.Parse(up.Data, subjectColumns)
	if err != nil {
		return err
	}
	ctx := r.Context()
	result := csvio.Result{DryRun: up.DryRun, Rows: len(rows)}

	err = inTx(ctx, h.db, r, up.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		existing := map[string]subject{}
		codeOf := map[int64]string{}
		list, err := tx.QueryContext(ctx, subjectSelect+" FOR UPDATE")
		if err != nil {
			return err
		}
		for list.Next() {
			s, err := scanSubject(list)
			if err != nil {
				list.Close()
				return err
			}
			existing[s.Code] = s
			codeOf[s.ID] = s.Code
		}
		list.Close()

		type item struct {
			line       int
			code, name string
			category   string
			parentCode string
			sortOrder  int
		}
		errs := &csvio.RowErrors{}
		items := map[string]item{}
		var order []string
		for _, row := range rows {
			req := subjectRequest{Code: row.Get("code"), Name: row.Get("name"), Category: row.Get("category")}
			code, name, err := validateSubjectRequest(&req)
			if err != nil {
				errs.AddDetails(row.Line, err)
				continue
			}
			it := item{line: row.Line, code: code, name: name, category: req.Category, parentCode: row.Get("parent_code")}
			if s := row.Get("sort_order"); s != "" {
				n, err := strconv.Atoi(s)
				if err != nil {
					errs.Add(row.Line, "表示順 %q は整数で入力してください", s)
					continue
				}
				it.sortOrder = n
			}
			if _, dup := items[code]; dup {
				errs.Add(row.Line, "コード %s が CSV 内で重複しています", code)
				continue
			}
			items[code] = it
			order = append(order, code)
		}
		if err := errs.Err(); err != nil {
			return err
		}

		// 取込後の親子関係と区分
		parentOf := map[string]string{}
		categoryOf := map[string]string{}
		for c, s := range existing {
			parentOf[c] = ""
			if s.ParentID != nil {
				parentOf[c] = codeOf[*s.ParentID]
			}
			categoryOf[c] = s.Category
		}
		for c, it := range items {
			parentOf[c] = it.parentCode
			categoryOf[c] = it.category
		}
		for _, c := range order {
			it := items[c]
			if it.parentCode == "" {
				continue
			}
			if _, ok := parentOf[it.parentCode]; !ok {
				errs.Add(it.line, "親科目のコード %s が見つかりません", it.parentCode)
			}
		}
		if err := errs.Err(); err != nil {
			return err
		}
		depth, cyclic := depths(parentOf)
		for _, c := range order {
			it := items[c]
			if cyclic[c] {
				errs.Add(it.line, "親子関係が循環しています（%s）", c)
			}
		}
		// 区分は親と同じであること（既存の子科目との関係も含めて確認する）
		for c, p := range parentOf {
			if p == "" || categoryOf[c] == categoryOf[p] {
				continue
			}
			line := 0
			if it, ok := items[c]; ok {
				line = it.line
			} else if it, ok := items[p]; ok {
				line = it.line
			}
			if line > 0 {
				errs.Add(line, "科目 %s と親科目 %s の区分が異なります", c, p)
			}
		}
		if err := errs.Err(); err != nil {
			return err
		}

		sort.SliceStable(order, func(i, j int) bool { return depth[order[i]] < depth[order[j]] })
		idOf := map[string]int64{}
		for c, s := range existing {
			idOf[c] = s.ID
		}
		for _, c := range order {
			it := items[c]
			var parentID *int64
			if it.parentCode != "" {
				id := idOf[it.parentCode]
				parentID = &id
			}
			before, exists := existing[c]
			if !exists {
				res, err := tx.ExecContext(ctx,
					"INSERT INTO subjects (parent_id, code, name, category, sort_order) VALUES (?, ?, ?, ?, ?)",
					dbx.NullInt64(parentID), c, it.name, it.category, it.sortOrder)
				if err != nil {
					return err
				}
				id, _ := res.LastInsertId()
				idOf[c] = id
				created, err := findSubject(ctx, tx, id, "")
				if err != nil {
					return err
				}
				if err := rec.Insert(ctx, "subjects", id, created); err != nil {
					return err
				}
				result.Inserted++
				continue
			}
			if sameParent(before.ParentID, parentID) && before.Name == it.name && before.Category == it.category && before.SortOrder == it.sortOrder {
				result.Unchanged++
				continue
			}
			if _, err := tx.ExecContext(ctx,
				"UPDATE subjects SET parent_id = ?, name = ?, category = ?, sort_order = ? WHERE id = ?",
				dbx.NullInt64(parentID), it.name, it.category, it.sortOrder, before.ID); err != nil {
				return err
			}
			after, err := findSubject(ctx, tx, before.ID, "")
			if err != nil {
				return err
			}
			if err := rec.Update(ctx, "subjects", before.ID, before, after); err != nil {
				return err
			}
			result.Updated++
		}
		if up.DryRun {
			return csvio.ErrDryRun
		}
		return nil
	})
	return csvio.Finish(w, result, err)
}

// --- ユーザー ---

var (
	userColumns = []string{"email", "name", "role", "is_active", "slack_user_id"}
	// userRequired はインポートで必須の列。slack_user_id は省略できる（省略した行は Slack のメンバー ID を変えない）
	userRequired = []string{"email", "name", "role", "is_active"}
)

// exportUsers は GET /api/users/export。パスワードは含めない。
func (h *Handler) exportUsers(w http.ResponseWriter, r *http.Request) error {
	rows, err := h.db.QueryContext(r.Context(), "SELECT email, name, role, is_active, COALESCE(slack_user_id, '') FROM users ORDER BY id")
	if err != nil {
		return err
	}
	defer rows.Close()
	var out [][]string
	for rows.Next() {
		var email, name, role, slackID string
		var active bool
		if err := rows.Scan(&email, &name, &role, &active, &slackID); err != nil {
			return err
		}
		out = append(out, []string{email, name, role, strconv.FormatBool(active), slackID})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return csvio.WriteCSV(w, "users", userColumns, out)
}

// importUsers は POST /api/users/import。パスワードは扱わない。
// 追加したユーザーはパスワード未設定（ログインできない）で作成し、FP&A が画面で設定する。
// 自分自身のロール変更・無効化はできない。
func (h *Handler) importUsers(w http.ResponseWriter, r *http.Request) error {
	me, _ := auth.UserFrom(r.Context())
	up, err := csvio.ReadUpload(w, r)
	if err != nil {
		return err
	}
	rows, err := csvio.ParseWithOptional(up.Data, userRequired, []string{"slack_user_id"})
	if err != nil {
		return err
	}
	ctx := r.Context()
	result := csvio.Result{DryRun: up.DryRun, Rows: len(rows)}

	err = inTx(ctx, h.db, r, up.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		type item struct {
			name, email string
			role        auth.Role
			active      bool
			slackID     string
			keepSlack   bool // slack_user_id の列がないので、既存の値を変えない
		}
		errs := &csvio.RowErrors{}
		var items []item
		seen := map[string]bool{}
		for _, row := range rows {
			v := httpx.Validator{}
			role := auth.Role(row.Get("role"))
			name, email := validateUserFields(v, row.Get("name"), row.Get("email"), role)
			slackID := validateSlackID(v, row.Get("slack_user_id"))
			active, ok := parseBool(row.Get("is_active"))
			if !ok {
				v.Add("is_active", "is_active は true / false で入力してください")
			}
			if email == me.Email && (role != me.Role || !active) {
				v.Add("role", "自分自身のロール変更・無効化はできません")
			}
			if seen[email] {
				v.Add("email", "メールアドレス "+email+" が CSV 内で重複しています")
			}
			seen[email] = true
			if err := v.Err(); err != nil {
				errs.AddDetails(row.Line, err)
				continue
			}
			items = append(items, item{name: name, email: email, role: role, active: active, slackID: slackID, keepSlack: !row.Has("slack_user_id")})
		}
		if err := errs.Err(); err != nil {
			return err
		}

		for _, it := range items {
			var id int64
			err := tx.QueryRowContext(ctx, "SELECT id FROM users WHERE email = ? FOR UPDATE", it.email).Scan(&id)
			if errors.Is(err, sql.ErrNoRows) {
				res, err := tx.ExecContext(ctx,
					"INSERT INTO users (name, email, password_hash, role, is_active, slack_user_id) VALUES (?, ?, ?, ?, ?, ?)",
					it.name, it.email, unusablePasswordHash, it.role, it.active, dbx.NullString(it.slackID))
				if err != nil {
					return err
				}
				id, _ = res.LastInsertId()
				created, err := findUser(ctx, tx, id, "")
				if err != nil {
					return err
				}
				if err := rec.Insert(ctx, "users", id, created); err != nil {
					return err
				}
				result.Inserted++
				continue
			}
			if err != nil {
				return err
			}
			before, err := findUser(ctx, tx, id, "")
			if err != nil {
				return err
			}
			if it.keepSlack {
				it.slackID = before.SlackUserID
			}
			if before.Name == it.name && before.Role == it.role && before.IsActive == it.active && before.SlackUserID == it.slackID {
				result.Unchanged++
				continue
			}
			if _, err := tx.ExecContext(ctx, "UPDATE users SET name = ?, role = ?, is_active = ?, slack_user_id = ? WHERE id = ?", it.name, it.role, it.active, dbx.NullString(it.slackID), id); err != nil {
				return err
			}
			if !it.active {
				if _, err := tx.ExecContext(ctx, "DELETE FROM sessions WHERE user_id = ?", id); err != nil {
					return err
				}
			}
			after, err := findUser(ctx, tx, id, "")
			if err != nil {
				return err
			}
			if err := rec.Update(ctx, "users", id, before, after); err != nil {
				return err
			}
			result.Updated++
		}
		// 無効にしたユーザーに担当が残っていれば知らせる（CSV では付け替えない。docs/plan.md「2.15」）
		for _, it := range items {
			if it.active {
				continue
			}
			var name string
			var acts, units int
			err := tx.QueryRowContext(ctx, `
				SELECT u.name,
				       (SELECT COUNT(*) FROM activities WHERE owner_user_id = u.id),
				       (SELECT COUNT(*) FROM units WHERE owner_user_id = u.id AND NOT is_archived)
				FROM users u WHERE u.email = ?`, it.email).Scan(&name, &acts, &units)
			if err != nil {
				return err
			}
			if acts+units > 0 {
				result.Warnings = append(result.Warnings, fmt.Sprintf("無効なユーザー「%s」に、担当の施策 %d 件・所管ユニット %d 件が残っています。ユーザーの画面で後任者に付け替えてください", name, acts, units))
			}
		}
		if up.DryRun {
			return csvio.ErrDryRun
		}
		return nil
	})
	return csvio.Finish(w, result, err)
}

// parseBool は true / false（1 / 0、有効 / 無効も可）を読む。空欄は true とする。
func parseBool(s string) (bool, bool) {
	switch strings.ToLower(s) {
	case "", "true", "1", "yes", "有効":
		return true, true
	case "false", "0", "no", "無効":
		return false, true
	}
	return false, false
}

// importRoutes はインポート・エクスポートのルートを登録する。
func (h *Handler) importRoutes(read, write func(httpx.HandlerFunc) http.Handler, mux *http.ServeMux) {
	for path, t := range map[string]*treeHandler{"organizations": h.organizations, "segments": h.segments} {
		mux.Handle("GET /api/"+path+"/export", read(t.export))
		mux.Handle("POST /api/"+path+"/import", write(t.importCSV))
	}
	mux.Handle("GET /api/units/export", read(h.exportUnits))
	mux.Handle("POST /api/units/import", write(h.importUnits))
	mux.Handle("GET /api/subjects/export", read(h.exportSubjects))
	mux.Handle("POST /api/subjects/import", write(h.importSubjects))
	mux.Handle("GET /api/users/export", read(h.exportUsers))
	mux.Handle("POST /api/users/import", write(h.importUsers))
}
