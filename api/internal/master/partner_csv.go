package master

import (
	"database/sql"
	"net/http"
	"strconv"

	"github.com/rnapyzz/f-panda-app/api/internal/audit"
	"github.com/rnapyzz/f-panda-app/api/internal/csvio"
	"github.com/rnapyzz/f-panda-app/api/internal/dbx"
)

// 取引先の CSV（docs/plan.md「2.29」）。取引先コードで突き合わせ、追加と更新だけを行う（CSV にない取引先は消さない）。

var (
	partnerColumns = []string{"code", "name", "company_name", "accounting_code", "direction", "is_active", "note"}
	// partnerRequired はインポートで必須の列。ほかの列は省略でき、省略した列は今の値を変えない（追加では空・取引中）
	partnerRequired = []string{"code", "name", "company_name", "direction"}
	partnerOptional = []string{"accounting_code", "is_active", "note"}
)

// partnerDirections は CSV で受け付ける区分の書き方。
var partnerDirections = map[string]string{"sales": "sales", "受注": "sales", "purchase": "purchase", "発注": "purchase"}

// exportPartners は GET /api/partners/export。
func (h *Handler) exportPartners(w http.ResponseWriter, r *http.Request) error {
	rows, err := h.db.QueryContext(r.Context(), partnerSelect+" ORDER BY p.code")
	if err != nil {
		return err
	}
	defer rows.Close()
	var out [][]string
	for rows.Next() {
		p, err := scanPartner(rows)
		if err != nil {
			return err
		}
		accounting := ""
		if p.AccountingCode != nil {
			accounting = *p.AccountingCode
		}
		out = append(out, []string{p.Code, p.Name, p.CompanyName, accounting, p.Direction, strconv.FormatBool(p.IsActive), p.Note})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return csvio.WriteCSV(w, "partners", partnerColumns, out)
}

// importPartners は POST /api/partners/import。
func (h *Handler) importPartners(w http.ResponseWriter, r *http.Request) error {
	up, err := csvio.ReadUpload(w, r)
	if err != nil {
		return err
	}
	rows, err := csvio.ParseWithOptional(up.Data, partnerRequired, partnerOptional)
	if err != nil {
		return err
	}
	ctx := r.Context()
	result := csvio.Result{DryRun: up.DryRun, Rows: len(rows)}

	err = inTx(ctx, h.db, r, up.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		existing := map[string]partner{}
		list, err := tx.QueryContext(ctx, partnerSelect+" FOR UPDATE")
		if err != nil {
			return err
		}
		for list.Next() {
			p, err := scanPartner(list)
			if err != nil {
				list.Close()
				return err
			}
			existing[p.Code] = p
		}
		list.Close()

		errs := &csvio.RowErrors{}
		seen := map[string]int{}
		type item struct {
			line   int
			in     partnerInput
			active bool
			before partner
			exists bool
		}
		var items []item
		for _, row := range rows {
			before, exists := existing[row.Get("code")]
			req := partnerRequest{
				Code: row.Get("code"), Name: row.Get("name"), CompanyName: row.Get("company_name"),
				Direction: partnerDirections[row.Get("direction")],
			}
			// 省略した列は今の値（追加では空・取引中）
			if exists {
				req.Note = before.Note
				if before.AccountingCode != nil {
					req.AccountingCode = *before.AccountingCode
				}
			}
			if row.Has("accounting_code") {
				req.AccountingCode = row.Get("accounting_code")
			}
			if row.Has("note") {
				req.Note = row.Get("note")
			}
			active := !exists || before.IsActive
			if row.Has("is_active") {
				v, ok := parseBool(row.Get("is_active"))
				if !ok {
					errs.Add(row.Line, "状態 %q は true（取引中）か false（取引停止）で入力してください", row.Get("is_active"))
					continue
				}
				active = v
			}
			in, err := validatePartner(req, false)
			if err != nil {
				errs.AddDetails(row.Line, err)
				continue
			}
			if first, dup := seen[in.code]; dup {
				errs.Add(row.Line, "%d 行目と同じ取引先コード %s の行です", first, in.code)
				continue
			}
			seen[in.code] = row.Line
			if exists && in.direction != before.Direction && before.LineCount > 0 {
				errs.Add(row.Line, "取引先 %s は内訳に付いているため、区分を変えられません", in.code)
				continue
			}
			items = append(items, item{line: row.Line, in: in, active: active, before: before, exists: exists})
		}
		if err := errs.Err(); err != nil {
			return err
		}

		for _, it := range items {
			in := it.in
			if !it.exists {
				res, err := tx.ExecContext(ctx,
					"INSERT INTO partners (code, name, company_name, accounting_code, direction, is_active, note) VALUES (?, ?, ?, ?, ?, ?, ?)",
					in.code, in.name, in.company, dbx.NullString(in.accounting), in.direction, it.active, dbx.NullString(in.note))
				if err := partnerDuplicate(err); err != nil {
					errs.AddDetails(it.line, err)
					continue
				}
				id, _ := res.LastInsertId()
				created, err := findPartner(ctx, tx, id, "")
				if err != nil {
					return err
				}
				if err := rec.Insert(ctx, "partners", id, created); err != nil {
					return err
				}
				result.Inserted++
				continue
			}
			b := it.before
			accounting := ""
			if b.AccountingCode != nil {
				accounting = *b.AccountingCode
			}
			if b.Name == in.name && b.CompanyName == in.company && accounting == in.accounting && b.Direction == in.direction && b.IsActive == it.active && b.Note == in.note {
				result.Unchanged++
				continue
			}
			_, err := tx.ExecContext(ctx,
				"UPDATE partners SET name = ?, company_name = ?, accounting_code = ?, direction = ?, is_active = ?, note = ? WHERE id = ?",
				in.name, in.company, dbx.NullString(in.accounting), in.direction, it.active, dbx.NullString(in.note), b.ID)
			if err := partnerDuplicate(err); err != nil {
				errs.AddDetails(it.line, err)
				continue
			}
			after, err := findPartner(ctx, tx, b.ID, "")
			if err != nil {
				return err
			}
			if err := rec.Update(ctx, "partners", b.ID, b, after); err != nil {
				return err
			}
			result.Updated++
		}
		if err := errs.Err(); err != nil {
			return err
		}
		if up.DryRun {
			return csvio.ErrDryRun
		}
		return nil
	})
	return csvio.Finish(w, result, err)
}
