package scenario

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/rnapyzz/f-panda-app/api/internal/actual"
	"github.com/rnapyzz/f-panda-app/api/internal/audit"
	"github.com/rnapyzz/f-panda-app/api/internal/calc"
	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
	"github.com/rnapyzz/f-panda-app/api/internal/target"
)

// シナリオの切り替え（docs/plan.md「2.16」）。
// 月次の見込を始める・新年度の期初計画を始める を、1つの変更セット（全部か無しか）で行う。
// dry_run なら保存せず、変わる内容と注意を返す。注意があっても実行できる。

type cycleRequest struct {
	Name           string `json:"name"`
	ActualThrough  string `json:"actual_through"` // 月次のみ（YYYY-MM）
	FiscalYear     int    `json:"fiscal_year"`    // 新年度のみ
	UpdateDeadline string `json:"update_deadline"`
	Reason         string `json:"reason"`
	DryRun         bool   `json:"dry_run"`
}

// cycleRef は変わる内容に出すシナリオ。
type cycleRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type cycleResult struct {
	DryRun bool `json:"dry_run"`
	// Lock はロックする（した）版。すでにロック済みなら nil
	Lock *cycleRef `json:"lock"`
	// Base は複製元（月次のみ）
	Base *cycleRef `json:"base"`
	// RoleFrom はエイリアスを付け替えられる版
	RoleFrom *cycleRef `json:"role_from"`
	// Scenario は新しい版（dry run では作った前提の内容）
	Scenario Scenario `json:"scenario"`
	Warnings []string `json:"warnings"`
}

func ref(s Scenario) *cycleRef { return &cycleRef{ID: s.ID, Name: s.Name} }

// activeScenario は作成中のシナリオを行ロック付きで返す（なければ nil）。
func activeScenario(ctx context.Context, tx *sql.Tx) (*Scenario, error) {
	s, err := scanScenario(tx.QueryRowContext(ctx, scenarioSelect+" WHERE is_active FOR UPDATE"))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &s, err
}

// roleHolder はその年度でエイリアス role を持つシナリオを返す（なければ nil）。
func roleHolder(ctx context.Context, tx *sql.Tx, fiscalYear int, role string) (*Scenario, error) {
	s, err := scanScenario(tx.QueryRowContext(ctx, scenarioSelect+" WHERE fiscal_year = ? AND plan_role = ? FOR UPDATE", fiscalYear, role))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &s, err
}

// incompleteCount は、シナリオの更新の対象のうち、更新を完了にしていない施策の数（docs/plan.md「2.10」の未着手・入力中）。
func incompleteCount(ctx context.Context, tx *sql.Tx, scenarioID int64) (int, error) {
	var n int
	err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM activities a
		LEFT JOIN activity_scenario_notes n ON n.activity_id = a.id AND n.scenario_id = ?
		WHERE n.completed_at IS NULL AND `+target.Condition("a"), scenarioID, scenarioID).Scan(&n)
	return n, err
}

func fiscalYearOf(month string) int {
	y, _ := strconv.Atoi(month[:4])
	m, _ := strconv.Atoi(month[5:7])
	if m < 4 {
		return y - 1
	}
	return y
}

// startMonthly は POST /api/scenarios/start-monthly。月次の見込を始める。
func (h *Handler) startMonthly(w http.ResponseWriter, r *http.Request) error {
	var req cycleRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := httpx.Validator{}
	name := v.Text("name", "シナリオ名", req.Name, 100)
	through := strings.TrimSpace(req.ActualThrough)
	if !isYearMonth(through) {
		v.Add("actual_through", "決算確定月は YYYY-MM の形式で指定してください")
	}
	deadline := validateDeadline(v, req.UpdateDeadline)
	if err := v.Err(); err != nil {
		return err
	}
	fy := fiscalYearOf(through)

	return h.runCycle(w, r, req, func(ctx context.Context, tx *sql.Tx, rec *audit.Recorder, userID int64, res *cycleResult) (*Scenario, error) {
		// 複製元: 作成中の版。なければその年度の「最新見込」の版
		base, err := activeScenario(ctx, tx)
		if err != nil {
			return nil, err
		}
		if base != nil && base.FiscalYear != fy {
			return nil, httpx.Validation(map[string]string{"actual_through": fmt.Sprintf("作成中の版（%d年度）と同じ年度の月を指定してください。年度の切り替えは「新年度の期初計画を始める」で行います", base.FiscalYear)})
		}
		if base == nil {
			if base, err = roleHolder(ctx, tx, fy, "latest"); err != nil {
				return nil, err
			}
			if base == nil {
				return nil, httpx.Validation(map[string]string{"actual_through": fmt.Sprintf("%d年度に、作成中の版も「最新見込」の版もありません。シナリオを作成してから始めてください", fy)})
			}
		}
		if base.ActualThrough != nil && through < *base.ActualThrough {
			return nil, httpx.Validation(map[string]string{"actual_through": fmt.Sprintf("決算確定月は、複製元の決算確定月（%s）以降にしてください", *base.ActualThrough)})
		}
		res.Base = ref(*base)

		// 注意
		months := calc.FiscalMonths(fy)
		var missing []string
		for _, m := range months {
			if m > through {
				break
			}
			var one int
			err := tx.QueryRowContext(ctx, "SELECT 1 FROM actual_facts WHERE target_month = ? LIMIT 1", m+"-01").Scan(&one)
			if errors.Is(err, sql.ErrNoRows) {
				missing = append(missing, m)
			} else if err != nil {
				return nil, err
			}
		}
		if len(missing) > 0 {
			res.Warnings = append(res.Warnings, "決算確定月までに、実績を取り込んでいない月があります: "+strings.Join(missing, "、"))
		}
		var unallocated int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM actual_facts WHERE activity_id IS NULL AND target_month BETWEEN ? AND ?",
			months[0]+"-01", months[len(months)-1]+"-01").Scan(&unallocated); err != nil {
			return nil, err
		}
		if unallocated > 0 {
			res.Warnings = append(res.Warnings, fmt.Sprintf("%d年度に未割当の実績があります（%d 件）。実績の割当で解消できます", fy, unallocated))
		}
		if base.IsActive {
			n, err := incompleteCount(ctx, tx, base.ID)
			if err != nil {
				return nil, err
			}
			if n > 0 {
				res.Warnings = append(res.Warnings, fmt.Sprintf("今の作成中の版「%s」で、更新を完了にしていない施策が %d 件あります", base.Name, n))
			}
		}
		drifted, err := actual.DriftedScenarios(ctx, tx, fy)
		if err != nil {
			return nil, err
		}
		if len(drifted) > 0 {
			res.Warnings = append(res.Warnings, "ロック済みの版に「実績の修正あり」があります: "+strings.Join(drifted, "、"))
		}

		// 1. 複製元をロックする
		if !base.IsLocked {
			res.Lock = ref(*base)
			locked, err := lockScenario(ctx, tx, rec, *base)
			if err != nil {
				return nil, err
			}
			*base = locked
		}
		// 4. エイリアス「最新見込」を付け替える
		if holder, err := roleHolder(ctx, tx, fy, "latest"); err != nil {
			return nil, err
		} else if holder != nil {
			res.RoleFrom = ref(*holder)
		}
		if err := takeRole(ctx, tx, rec, fy, "latest", 0); err != nil {
			return nil, err
		}
		// 2・3・5. 複製して決算確定月・前回見込・締切を設定する
		created, err := insertScenario(ctx, tx, rec, scenarioInsert{
			name: name, fiscalYear: fy, role: "latest", through: monthDate(through),
			base: &base.ID, previous: &base.ID, deadline: deadline, createdBy: userID,
		})
		if err != nil {
			return nil, err
		}
		if err := calc.RecalculateScenario(ctx, tx, rec, created.ID); err != nil {
			return nil, err
		}
		return &created, nil
	})
}

// startFiscalYear は POST /api/scenarios/start-fiscal-year。新年度の期初計画を始める。
func (h *Handler) startFiscalYear(w http.ResponseWriter, r *http.Request) error {
	var req cycleRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := httpx.Validator{}
	name := v.Text("name", "シナリオ名", req.Name, 100)
	if req.FiscalYear < 2000 || req.FiscalYear > 2100 {
		v.Add("fiscal_year", "年度を正しく入力してください（例: 2027）")
	}
	deadline := validateDeadline(v, req.UpdateDeadline)
	if err := v.Err(); err != nil {
		return err
	}
	fy := req.FiscalYear

	return h.runCycle(w, r, req, func(ctx context.Context, tx *sql.Tx, rec *audit.Recorder, userID int64, res *cycleResult) (*Scenario, error) {
		var closed int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM fiscal_year_closings WHERE fiscal_year = ?", fy-1).Scan(&closed); err != nil {
			return nil, err
		}
		if closed == 0 {
			res.Warnings = append(res.Warnings, fmt.Sprintf("前の年度（%d年度）を締めていません。会計の決算が確定したら、シナリオ管理で締めてください", fy-1))
		}
		// 1. 今の作成中の版をロックする
		cur, err := activeScenario(ctx, tx)
		if err != nil {
			return nil, err
		}
		if cur != nil {
			n, err := incompleteCount(ctx, tx, cur.ID)
			if err != nil {
				return nil, err
			}
			if n > 0 {
				res.Warnings = append(res.Warnings, fmt.Sprintf("今の作成中の版「%s」で、更新を完了にしていない施策が %d 件あります", cur.Name, n))
			}
			res.Lock = ref(*cur)
			if _, err := lockScenario(ctx, tx, rec, *cur); err != nil {
				return nil, err
			}
		}
		// 2. 空の版を「期初計画」として作る
		if holder, err := roleHolder(ctx, tx, fy, "initial"); err != nil {
			return nil, err
		} else if holder != nil {
			res.RoleFrom = ref(*holder)
			res.Warnings = append(res.Warnings, fmt.Sprintf("%d年度の「期初計画」のエイリアスを「%s」から付け替えます", fy, holder.Name))
		}
		if err := takeRole(ctx, tx, rec, fy, "initial", 0); err != nil {
			return nil, err
		}
		created, err := insertScenario(ctx, tx, rec, scenarioInsert{
			name: name, fiscalYear: fy, role: "initial", through: nil, deadline: deadline, createdBy: userID,
		})
		if err != nil {
			return nil, err
		}
		return &created, nil
	})
}

// runCycle は切り替えを1つの変更セットで行い、新しい版を作成中にする。締切があれば、保存の後に「更新の開始」を送る。
func (h *Handler) runCycle(w http.ResponseWriter, r *http.Request, req cycleRequest,
	fn func(ctx context.Context, tx *sql.Tx, rec *audit.Recorder, userID int64, res *cycleResult) (*Scenario, error)) error {
	u, err := currentUser(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		reason = "シナリオの切り替え（" + strings.TrimSpace(req.Name) + "）"
	}
	res := cycleResult{DryRun: req.DryRun, Warnings: []string{}}
	err = audit.InTx(ctx, h.db, u.ID, nil, reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		created, err := fn(ctx, tx, rec, u.ID, &res)
		if err != nil {
			return err
		}
		// 6. 新しい版を作成中に指定する（前の作成中はロックで外れている）
		if err := deactivateCurrent(ctx, tx, rec); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE scenarios SET is_active = TRUE WHERE id = ?", created.ID); err != nil {
			return err
		}
		active, err := findScenario(ctx, tx, created.ID, "")
		if err != nil {
			return err
		}
		if err := rec.Update(ctx, "scenarios", created.ID, *created, active); err != nil {
			return err
		}
		res.Scenario = active
		if req.DryRun {
			return errDryRun
		}
		return nil
	})
	if err != nil && !errors.Is(err, errDryRun) {
		return err
	}
	// 7. 締切があれば「更新の開始」を送る
	if !req.DryRun && h.notifier != nil && res.Scenario.UpdateDeadline != nil {
		h.notifier.UpdateStarted(ctx, res.Scenario.ID)
	}
	status := http.StatusOK
	if !req.DryRun {
		status = http.StatusCreated
	}
	httpx.WriteJSON(w, status, res)
	return nil
}

// isYearMonth は YYYY-MM の形式かを返す。
func isYearMonth(s string) bool {
	if len(s) != 7 || s[4] != '-' {
		return false
	}
	for i, c := range s {
		if i != 4 && (c < '0' || c > '9') {
			return false
		}
	}
	m, _ := strconv.Atoi(s[5:])
	return m >= 1 && m <= 12
}
