package scenario

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/rnapyzz/f-panda-app/api/internal/audit"
	"github.com/rnapyzz/f-panda-app/api/internal/dbx"
	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
)

// シナリオの説明（docs/plan.md「2.31」）。計画の骨子・成長の柱・主な前提・主なリスクと機会を、シナリオごとに FP&A が書く。

// narrativeMaxLen は各欄の文字数の上限。
const narrativeMaxLen = 2000

// narrative はシナリオの説明。書いていなければ欄は空で、UpdatedAt は nil。
type narrative struct {
	ScenarioID    int64      `json:"scenario_id"`
	Outline       string     `json:"outline"`
	Growth        string     `json:"growth"`
	Premises      string     `json:"premises"`
	Risks         string     `json:"risks"`
	UpdatedBy     *int64     `json:"updated_by"`
	UpdatedByName string     `json:"updated_by_name"`
	UpdatedAt     *time.Time `json:"updated_at"`
}

// narrativeRecord は監査ログに残す欄。
type narrativeRecord struct {
	ScenarioID int64  `json:"scenario_id"`
	Outline    string `json:"outline"`
	Growth     string `json:"growth"`
	Premises   string `json:"premises"`
	Risks      string `json:"risks"`
}

func (n narrative) record() narrativeRecord {
	return narrativeRecord{n.ScenarioID, n.Outline, n.Growth, n.Premises, n.Risks}
}

// loadNarrative はシナリオの説明を読む。なければ空の説明と false を返す。lock は " FOR UPDATE" など。
func loadNarrative(ctx context.Context, q dbx.Querier, scenarioID int64, lock string) (narrative, bool, error) {
	n := narrative{ScenarioID: scenarioID}
	var by sql.NullInt64
	var byName sql.NullString
	var at time.Time
	err := q.QueryRowContext(ctx, `
		SELECT n.outline, n.growth, n.premises, n.risks, n.updated_by, u.name, n.updated_at
		FROM scenario_narratives n LEFT JOIN users u ON u.id = n.updated_by
		WHERE n.scenario_id = ?`+lock, scenarioID,
	).Scan(&n.Outline, &n.Growth, &n.Premises, &n.Risks, &by, &byName, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return n, false, nil
	}
	if err != nil {
		return n, false, err
	}
	if by.Valid {
		n.UpdatedBy = &by.Int64
	}
	n.UpdatedByName = byName.String
	n.UpdatedAt = &at
	return n, true, nil
}

// getNarrative は GET /api/scenarios/{id}/narrative。
func (h *Handler) getNarrative(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	ctx := r.Context()
	if _, err := findScenario(ctx, h.db, id, ""); err != nil {
		return err
	}
	n, _, err := loadNarrative(ctx, h.db, id, "")
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, n)
	return nil
}

// putNarrative は PUT /api/scenarios/{id}/narrative（FP&A のみ）。ロック済みのシナリオは変更できない。
// 変更理由は任意で、空なら「<シナリオ名>の説明を更新」を記録する。
func (h *Handler) putNarrative(w http.ResponseWriter, r *http.Request) error {
	u, err := currentUser(r)
	if err != nil {
		return err
	}
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var req struct {
		Outline  string `json:"outline"`
		Growth   string `json:"growth"`
		Premises string `json:"premises"`
		Risks    string `json:"risks"`
		Reason   string `json:"reason"`
	}
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := httpx.Validator{}
	in := narrativeRecord{
		ScenarioID: id,
		Outline:    v.OptionalText("outline", "計画の骨子", req.Outline, narrativeMaxLen),
		Growth:     v.OptionalText("growth", "成長の柱", req.Growth, narrativeMaxLen),
		Premises:   v.OptionalText("premises", "主な前提", req.Premises, narrativeMaxLen),
		Risks:      v.OptionalText("risks", "主なリスクと機会", req.Risks, narrativeMaxLen),
	}
	if err := v.Err(); err != nil {
		return err
	}

	ctx := r.Context()
	s, err := findScenario(ctx, h.db, id, "")
	if err != nil {
		return err
	}
	reason := req.Reason
	if strings.TrimSpace(reason) == "" {
		reason = s.Name + "の説明を更新"
	}
	err = audit.InTx(ctx, h.db, u.ID, &id, reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		s, err := findScenario(ctx, tx, id, " FOR SHARE")
		if err != nil {
			return err
		}
		if s.IsLocked {
			return httpx.Conflict("ロックされたシナリオの説明は変更できません")
		}
		before, found, err := loadNarrative(ctx, tx, id, " FOR UPDATE")
		if err != nil {
			return err
		}
		if found && before.record() == in {
			return nil
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO scenario_narratives (scenario_id, outline, growth, premises, risks, updated_by)
			VALUES (?, ?, ?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE outline = VALUES(outline), growth = VALUES(growth), premises = VALUES(premises),
				risks = VALUES(risks), updated_by = VALUES(updated_by)`,
			id, in.Outline, in.Growth, in.Premises, in.Risks, u.ID); err != nil {
			return err
		}
		if !found {
			return rec.Insert(ctx, "scenario_narratives", id, in)
		}
		return rec.Update(ctx, "scenario_narratives", id, before.record(), in)
	})
	if err != nil {
		return err
	}
	n, _, err := loadNarrative(ctx, h.db, id, "")
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, n)
	return nil
}
