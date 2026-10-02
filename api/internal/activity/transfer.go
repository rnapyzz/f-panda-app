package activity

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/rnapyzz/f-panda-app/api/internal/audit"
	"github.com/rnapyzz/f-panda-app/api/internal/auth"
	"github.com/rnapyzz/f-panda-app/api/internal/csvio"
	"github.com/rnapyzz/f-panda-app/api/internal/dbx"
	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
)

// 施策の CSV インポート・エクスポート。取込は「追加と更新」のみで、CSV にない施策は削除しない。
// code が空の行は新しい施策として追加し、施策コードを自動採番する。
// external_codes は空白区切りの外部コード。記載した外部コードを施策に追加する（記載のない外部コードは外さない）。

var activityColumns = []string{
	"code", "name", "unit_code", "activity_type", "status", "start_date", "end_date",
	"owner_email", "confidence_level", "assumptions", "external_codes",
}

// export は GET /api/activities/export。
func (h *Handler) export(w http.ResponseWriter, r *http.Request) error {
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT a.code, a.name, un.code, a.activity_type, a.status,
		       COALESCE(DATE_FORMAT(a.start_date, '%Y-%m-%d'), ''), COALESCE(DATE_FORMAT(a.end_date, '%Y-%m-%d'), ''),
		       COALESCE(usr.email, ''), a.confidence_level, COALESCE(a.assumptions, ''),
		       COALESCE((SELECT GROUP_CONCAT(e.code ORDER BY e.code SEPARATOR ' ') FROM activity_external_codes e WHERE e.activity_id = a.id), '')
		FROM activities a
		JOIN units un ON un.id = a.unit_id
		LEFT JOIN users usr ON usr.id = a.owner_user_id
		ORDER BY a.code`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var out [][]string
	for rows.Next() {
		rec := make([]string, len(activityColumns))
		dest := make([]any, len(rec))
		for i := range rec {
			dest[i] = &rec[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return err
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return csvio.WriteCSV(w, "activities", activityColumns, out)
}

type activityImportItem struct {
	line      int
	in        activityInput
	externals []string
}

// importCSV は POST /api/activities/import。FP&A のみ。変更理由が必須。
func (h *Handler) importCSV(w http.ResponseWriter, r *http.Request) error {
	u, err := currentUser(r)
	if err != nil {
		return err
	}
	if u.Role != auth.RoleFPAAdmin {
		return httpx.Forbidden()
	}
	up, err := csvio.ReadUpload(w, r)
	if err != nil {
		return err
	}
	rows, err := csvio.Parse(up.Data, activityColumns)
	if err != nil {
		return err
	}
	ctx := r.Context()
	result := csvio.Result{DryRun: up.DryRun, Rows: len(rows)}

	err = inTx(r, h.db, u, up.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		units, err := idIndex(ctx, tx, "SELECT code, id FROM units FOR SHARE")
		if err != nil {
			return err
		}
		users, err := idIndex(ctx, tx, "SELECT email, id FROM users")
		if err != nil {
			return err
		}
		activityIDs, err := idIndex(ctx, tx, "SELECT code, id FROM activities FOR UPDATE")
		if err != nil {
			return err
		}
		externalOwner, err := idIndex(ctx, tx, "SELECT code, activity_id FROM activity_external_codes FOR UPDATE")
		if err != nil {
			return err
		}
		levels, err := idIndex(ctx, tx, "SELECT code, id FROM confidence_levels")
		if err != nil {
			return err
		}

		errs := &csvio.RowErrors{}
		var items []activityImportItem
		seenCodes := map[string]bool{}
		seenExternals := map[string]int{} // 外部コード → CSV の行
		for _, row := range rows {
			req := activityRequest{
				Code:         row.Get("code"),
				Name:         row.Get("name"),
				ActivityType: row.Get("activity_type"),
				Status:       row.Get("status"),
				Assumptions:  row.Get("assumptions"),
			}
			if req.Status == "" {
				req.Status = "planned"
			}
			for _, d := range []struct {
				col string
				dst **string
			}{{"start_date", &req.StartDate}, {"end_date", &req.EndDate}} {
				if s := row.Get(d.col); s != "" {
					*d.dst = &s
				}
			}
			req.ConfidenceLevel = row.Get("confidence_level")

			v := httpx.Validator{}
			if id, ok := units[row.Get("unit_code")]; ok {
				req.UnitID = id
			} else {
				v.Add("unit_id", "ユニットのコード "+strconv.Quote(row.Get("unit_code"))+" が見つかりません")
			}
			if e := auth.NormalizeEmail(row.Get("owner_email")); e != "" {
				if id, ok := users[e]; ok {
					req.OwnerUserID = &id
				} else {
					v.Add("owner_user_id", "担当者のメールアドレス "+e+" のユーザーが見つかりません")
				}
			}
			if req.Code != "" {
				if seenCodes[req.Code] {
					v.Add("code", "施策コード "+req.Code+" が CSV 内で重複しています")
				}
				seenCodes[req.Code] = true
				if _, isExternal := externalOwner[req.Code]; isExternal {
					if _, isActivity := activityIDs[req.Code]; !isActivity {
						v.Add("code", "施策コード "+req.Code+" は外部コードとして使われています")
					}
				}
			}
			// 項目の検証。ユニット・担当者はコードの解決で出したメッセージを優先する（同じ内容を2重に出さない）
			in, err := validateActivity(req, true)
			var apiErr *httpx.Error
			if errors.As(err, &apiErr) {
				for field, msg := range apiErr.Details {
					if _, dup := v[field]; !dup {
						v.Add(field, msg)
					}
				}
			} else if err != nil {
				return err
			}
			if _, ok := levels[in.ConfidenceLevel]; err == nil && !ok {
				v.Add("confidence_level", "確度の段階 "+strconv.Quote(in.ConfidenceLevel)+" は登録されていません")
			}
			if verr := v.Err(); verr != nil {
				errs.AddDetails(row.Line, verr)
			}

			// 外部コード
			externals := strings.Fields(row.Get("external_codes"))
			for _, ext := range externals {
				switch {
				case !externalCodePattern.MatchString(ext):
					errs.Add(row.Line, "外部コード %q に使えない文字が含まれています", ext)
				case seenExternals[ext] > 0:
					errs.Add(row.Line, "外部コード %s が CSV の %d 行目と重複しています", ext, seenExternals[ext])
				default:
					if _, isActivityCode := activityIDs[ext]; isActivityCode || seenCodes[ext] {
						errs.Add(row.Line, "外部コード %s は施策コードとして使われています", ext)
					} else if owner, ok := externalOwner[ext]; ok && (req.Code == "" || owner != activityIDs[req.Code]) {
						errs.Add(row.Line, "外部コード %s は別の施策に登録されています", ext)
					}
				}
				seenExternals[ext] = row.Line
			}
			if v.Err() == nil {
				items = append(items, activityImportItem{line: row.Line, in: in, externals: externals})
			}
		}
		if err := errs.Err(); err != nil {
			return err
		}

		for _, it := range items {
			changed, created, id, err := h.upsertActivity(ctx, tx, rec, it.in)
			if err != nil {
				return err
			}
			for _, ext := range it.externals {
				if owner, ok := externalOwner[ext]; ok && owner == id {
					continue
				}
				res, err := tx.ExecContext(ctx, "INSERT INTO activity_external_codes (activity_id, code) VALUES (?, ?)", id, ext)
				if err != nil {
					return err
				}
				eid, _ := res.LastInsertId()
				e, err := scanExternalCode(tx.QueryRowContext(ctx, externalCodeSelect+" WHERE id = ?", eid))
				if err != nil {
					return err
				}
				if err := rec.Insert(ctx, "activity_external_codes", eid, e); err != nil {
					return err
				}
				changed = true
			}
			switch {
			case created:
				result.Inserted++
			case changed:
				result.Updated++
			default:
				result.Unchanged++
			}
		}
		if up.DryRun {
			return csvio.ErrDryRun
		}
		return nil
	})
	return csvio.Finish(w, result, err)
}

// upsertActivity は施策を追加または更新する。コードが空なら自動採番して追加する。
func (h *Handler) upsertActivity(ctx context.Context, tx *sql.Tx, rec *audit.Recorder, in activityInput) (changed, created bool, id int64, err error) {
	var before activity
	found := false
	if in.Code != "" {
		before, err = scanActivity(tx.QueryRowContext(ctx, activitySelect+" WHERE a.code = ? FOR UPDATE", in.Code))
		switch {
		case err == nil:
			found = true
		case errors.Is(err, sql.ErrNoRows):
		default:
			return false, false, 0, err
		}
	}

	if !found {
		if in.Code == "" {
			if in.Code, err = nextActivityCode(ctx, tx); err != nil {
				return false, false, 0, err
			}
		}
		res, err := tx.ExecContext(ctx, `
			INSERT INTO activities (unit_id, code, name, activity_type, status, start_date, end_date,
			                        owner_user_id, confidence_level, assumptions)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			in.UnitID, in.Code, in.Name, in.ActivityType, in.Status, in.StartDate, in.EndDate,
			dbx.NullInt64(in.OwnerUserID), in.ConfidenceLevel, dbx.NullString(in.Assumptions))
		if err != nil {
			return false, false, 0, err
		}
		id, _ = res.LastInsertId()
		createdRow, err := findActivity(ctx, tx, id, "")
		if err != nil {
			return false, false, 0, err
		}
		return true, true, id, rec.Insert(ctx, "activities", id, createdRow)
	}

	same := before.UnitID == in.UnitID && before.Name == in.Name && before.ActivityType == in.ActivityType &&
		before.Status == in.Status && sameDate(before.StartDate, in.StartDate) && sameDate(before.EndDate, in.EndDate) &&
		sameOwner(before.OwnerUserID, in.OwnerUserID) &&
		before.ConfidenceLevel == in.ConfidenceLevel && before.Assumptions == in.Assumptions
	if same {
		return false, false, before.ID, nil
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE activities SET unit_id = ?, name = ?, activity_type = ?, status = ?, start_date = ?, end_date = ?,
		       owner_user_id = ?, confidence_level = ?, assumptions = ?
		WHERE id = ?`,
		in.UnitID, in.Name, in.ActivityType, in.Status, in.StartDate, in.EndDate,
		dbx.NullInt64(in.OwnerUserID), in.ConfidenceLevel, dbx.NullString(in.Assumptions), before.ID); err != nil {
		return false, false, 0, err
	}
	after, err := findActivity(ctx, tx, before.ID, "")
	if err != nil {
		return false, false, 0, err
	}
	if err := rec.Update(ctx, "activities", before.ID, before, after); err != nil {
		return false, false, 0, err
	}
	return true, false, before.ID, nil
}

func sameOwner(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// idIndex は「キー, ID」を返すクエリから対応表を作る。
func idIndex(ctx context.Context, tx *sql.Tx, query string) (map[string]int64, error) {
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var k string
		var id int64
		if err := rows.Scan(&k, &id); err != nil {
			return nil, err
		}
		out[k] = id
	}
	return out, rows.Err()
}
