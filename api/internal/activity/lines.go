package activity

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/rnapyzz/f-panda-app/api/internal/audit"
	"github.com/rnapyzz/f-panda-app/api/internal/calc"
	"github.com/rnapyzz/f-panda-app/api/internal/dbx"
	"github.com/rnapyzz/f-panda-app/api/internal/formula"
	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
)

// 金額の内訳。施策 × 科目の下に、名前付きの内訳を複数持てる（例: 売上高 = 月額利用料 + 初期導入費）。
// 内訳ごとに、計算式で反映する（formula_enabled）か、直接入力するかを決める。反映しない場合も式は残せる。
// 科目の金額は、内訳の金額と、科目への直接入力（内訳なし）の金額の合計。

const maxExpressionLen = 1000

// line は金額の内訳。
type line struct {
	ID             int64  `json:"id"`
	ActivityID     int64  `json:"activity_id"`
	SubjectID      int64  `json:"subject_id"`
	Name           string `json:"name"`
	Expression     string `json:"expression"`
	FormulaEnabled bool   `json:"formula_enabled"`
	// ConfidenceLevel は内訳の確度の段階。nil なら施策の段階を使う
	ConfidenceLevel *string `json:"confidence_level"`
	// Outlook は見通しの種類（base: ベース / addon: アドオン / downside: ダウンサイド）
	Outlook   string `json:"outlook"`
	SortOrder int    `json:"sort_order"`
	timestamps
}

// outlooks は見通しの種類。
var outlooks = []string{"base", "addon", "downside"}

type lineRequest struct {
	SubjectID      int64  `json:"subject_id"` // 作成時のみ
	Name           string `json:"name"`
	Expression     string `json:"expression"`
	FormulaEnabled bool   `json:"formula_enabled"`
	// ConfidenceLevel は確度の段階のコード。空なら施策の段階を使う
	ConfidenceLevel string `json:"confidence_level"`
	// Outlook は見通しの種類。空ならベース
	Outlook   string `json:"outlook"`
	SortOrder int    `json:"sort_order"`
	Reason    string `json:"reason"`
}

const lineSelect = "SELECT id, activity_id, subject_id, name, COALESCE(expression, ''), formula_enabled, confidence_level, outlook, sort_order, created_at, updated_at FROM activity_lines"

func scanLine(row interface{ Scan(...any) error }) (line, error) {
	var l line
	var level sql.NullString
	err := row.Scan(&l.ID, &l.ActivityID, &l.SubjectID, &l.Name, &l.Expression, &l.FormulaEnabled, &level, &l.Outlook, &l.SortOrder, &l.CreatedAt, &l.UpdatedAt)
	if level.Valid {
		l.ConfidenceLevel = &level.String
	}
	return l, err
}

func listLines(ctx context.Context, q querier, activityID int64) ([]line, error) {
	rows, err := q.QueryContext(ctx, lineSelect+" WHERE activity_id = ? ORDER BY subject_id, sort_order, id", activityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []line{}
	for rows.Next() {
		l, err := scanLine(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, l)
	}
	return items, rows.Err()
}

func findLine(ctx context.Context, tx *sql.Tx, activityID, id int64) (line, error) {
	l, err := scanLine(tx.QueryRowContext(ctx, lineSelect+" WHERE id = ? AND activity_id = ? FOR UPDATE", id, activityID))
	if errors.Is(err, sql.ErrNoRows) {
		return line{}, httpx.NotFound("内訳が見つかりません")
	}
	return l, err
}

// validateLine は内訳の入力を検証し、計算式を解析する。式に使える識別子は、施策のドライバーの code。
func validateLine(ctx context.Context, tx *sql.Tx, activityID int64, req *lineRequest) error {
	v := httpx.Validator{}
	req.Name = v.Text("name", "内訳名", req.Name, 100)
	req.ConfidenceLevel = strings.TrimSpace(req.ConfidenceLevel)
	if req.ConfidenceLevel != "" {
		n, err := dbx.Count(ctx, tx, "SELECT COUNT(*) FROM confidence_levels WHERE code = ?", req.ConfidenceLevel)
		if err != nil {
			return err
		}
		if n == 0 {
			v.Add("confidence_level", "確度の段階「"+req.ConfidenceLevel+"」は登録されていません")
		}
	}
	if req.Outlook == "" {
		req.Outlook = "base"
	}
	if !slices.Contains(outlooks, req.Outlook) {
		v.Add("outlook", "見通しの種類は base（ベース）/ addon（アドオン）/ downside（ダウンサイド）のいずれかを指定してください")
	}
	req.Expression = v.OptionalText("expression", "計算式", req.Expression, maxExpressionLen)
	if req.FormulaEnabled && req.Expression == "" {
		v.Add("expression", "計算式で反映するには、計算式を入力してください")
	}
	if req.Expression != "" {
		parsed, err := formula.Parse(req.Expression)
		if err != nil {
			v.Add("expression", err.Error())
		} else {
			drivers, err := listDrivers(ctx, tx, activityID)
			if err != nil {
				return err
			}
			var known []string
			for _, d := range drivers {
				known = append(known, d.Code)
			}
			var unknown []string
			for _, id := range parsed.Idents() {
				if !slices.Contains(known, id) {
					unknown = append(unknown, id)
				}
			}
			if len(unknown) > 0 {
				v.Add("expression", "未定義のドライバーが使われています: "+strings.Join(unknown, ", "))
			}
		}
	}
	return v.Err()
}

// createLine は POST /api/activities/{id}/lines。計算式で反映する内訳を作る場合は変更理由が必須。
func (h *Handler) createLine(w http.ResponseWriter, r *http.Request) error {
	u, err := currentUser(r)
	if err != nil {
		return err
	}
	activityID, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var req lineRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	if req.FormulaEnabled {
		if err := requireReason(req.Reason); err != nil {
			return err
		}
	}

	ctx := r.Context()
	var created line
	err = inTx(r, h.db, u, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		if _, err := lockEditable(ctx, tx, u, activityID); err != nil {
			return err
		}
		ok, err := dbx.Exists(ctx, tx, "subjects", req.SubjectID)
		if err != nil {
			return err
		}
		if !ok {
			return httpx.Validation(map[string]string{"subject_id": "科目を選択してください"})
		}
		if err := validateLine(ctx, tx, activityID, &req); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx,
			"INSERT INTO activity_lines (activity_id, subject_id, name, expression, formula_enabled, confidence_level, outlook, sort_order) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
			activityID, req.SubjectID, req.Name, dbx.NullString(req.Expression), req.FormulaEnabled, dbx.NullString(req.ConfidenceLevel), req.Outlook, req.SortOrder)
		if dbx.ErrNo(err) == dbx.ErrDuplicateEntry {
			return httpx.Validation(map[string]string{"name": "この科目に同じ名前の内訳があります"})
		}
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		if created, err = scanLine(tx.QueryRowContext(ctx, lineSelect+" WHERE id = ?", id)); err != nil {
			return err
		}
		if err := rec.Insert(ctx, "activity_lines", id, created); err != nil {
			return err
		}
		if created.FormulaEnabled {
			return calc.RecalculateActivity(ctx, tx, rec, activityID)
		}
		return nil
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, created)
	return nil
}

// updateLine は PUT /api/activities/{id}/lines/{lid}。科目は変えられない。
// 計算式・反映の有無・確度の段階・見通しの種類を変える場合は変更理由が必須（金額や加重見込に影響するため）。
// 反映をやめた内訳の金額はそのまま残り、以後は直接入力で編集できる。
func (h *Handler) updateLine(w http.ResponseWriter, r *http.Request) error {
	u, err := currentUser(r)
	if err != nil {
		return err
	}
	activityID, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	id, err := httpx.PathID(r, "lid")
	if err != nil {
		return err
	}
	var req lineRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}

	ctx := r.Context()
	var updated line
	err = inTx(r, h.db, u, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		if _, err := lockEditable(ctx, tx, u, activityID); err != nil {
			return err
		}
		before, err := findLine(ctx, tx, activityID, id)
		if err != nil {
			return err
		}
		if err := validateLine(ctx, tx, activityID, &req); err != nil {
			return err
		}
		formulaChanged := before.Expression != req.Expression || before.FormulaEnabled != req.FormulaEnabled
		// 確度の段階・見通しの種類も、加重見込や楽観・悲観に影響するため理由が必須（docs/plan.md「2.7」）
		levelChanged := derefString(before.ConfidenceLevel) != req.ConfidenceLevel || before.Outlook != req.Outlook
		if formulaChanged || levelChanged {
			if err := requireReason(req.Reason); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx,
			"UPDATE activity_lines SET name = ?, expression = ?, formula_enabled = ?, confidence_level = ?, outlook = ?, sort_order = ? WHERE id = ?",
			req.Name, dbx.NullString(req.Expression), req.FormulaEnabled, dbx.NullString(req.ConfidenceLevel), req.Outlook, req.SortOrder, id)
		if dbx.ErrNo(err) == dbx.ErrDuplicateEntry {
			return httpx.Validation(map[string]string{"name": "この科目に同じ名前の内訳があります"})
		}
		if err != nil {
			return err
		}
		if updated, err = scanLine(tx.QueryRowContext(ctx, lineSelect+" WHERE id = ?", id)); err != nil {
			return err
		}
		if err := rec.Update(ctx, "activity_lines", id, before, updated); err != nil {
			return err
		}
		if formulaChanged && updated.FormulaEnabled {
			return calc.RecalculateActivity(ctx, tx, rec, activityID)
		}
		return nil
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, updated)
	return nil
}

// deleteLine は DELETE /api/activities/{id}/lines/{lid}。変更理由が必須。
// ロック済み・実績のシナリオに金額がある内訳は削除できない（確定した値を消さないため）。
// それ以外のシナリオの、この内訳の金額は合わせて削除する。
func (h *Handler) deleteLine(w http.ResponseWriter, r *http.Request) error {
	u, err := currentUser(r)
	if err != nil {
		return err
	}
	activityID, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	id, err := httpx.PathID(r, "lid")
	if err != nil {
		return err
	}
	var req reasonRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	if err := requireReason(req.Reason); err != nil {
		return err
	}

	ctx := r.Context()
	err = inTx(r, h.db, u, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		if _, err := lockEditable(ctx, tx, u, activityID); err != nil {
			return err
		}
		before, err := findLine(ctx, tx, activityID, id)
		if err != nil {
			return err
		}
		n, err := dbx.Count(ctx, tx, `
			SELECT COUNT(*) FROM budget_facts b JOIN scenarios s ON s.id = b.scenario_id
			WHERE b.line_id = ? AND s.is_locked`, id)
		if err != nil {
			return err
		}
		if n > 0 {
			return httpx.Conflict("ロックされたシナリオに金額がある内訳は削除できません。計算式で反映しない設定にしてください")
		}
		rows, err := tx.QueryContext(ctx, `
			SELECT id, scenario_id, subject_id, DATE_FORMAT(target_month, '%Y-%m'), amount, source, is_provisional, COALESCE(provisional_reason, '')
			FROM budget_facts WHERE line_id = ? FOR UPDATE`, id)
		if err != nil {
			return err
		}
		var facts []calc.Fact
		for rows.Next() {
			f := calc.Fact{ActivityID: activityID, LineID: &id}
			if err := rows.Scan(&f.ID, &f.ScenarioID, &f.SubjectID, &f.TargetMonth, &f.Amount, &f.Source, &f.IsProvisional, &f.ProvisionalReason); err != nil {
				rows.Close()
				return err
			}
			facts = append(facts, f)
		}
		rows.Close()
		for _, f := range facts {
			if _, err := tx.ExecContext(ctx, "DELETE FROM budget_facts WHERE id = ?", f.ID); err != nil {
				return err
			}
			if err := rec.Delete(ctx, "budget_facts", f.ID, f); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM activity_lines WHERE id = ?", id); err != nil {
			return err
		}
		return rec.Delete(ctx, "activity_lines", id, before)
	})
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
