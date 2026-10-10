package report

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rnapyzz/f-panda-app/api/internal/auth"
	"github.com/rnapyzz/f-panda-app/api/internal/calc"
	"github.com/rnapyzz/f-panda-app/api/internal/formula"
	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
	"github.com/rnapyzz/f-panda-app/api/internal/visibility"
)

// リスク画面（docs/plan.md「2.8 確度とリスク」「2.9 リスク画面」）。
// 施策ごとに、楽観・基準（加重見込）・悲観・満額、段階別の売上、内訳、比較シナリオとの差、客観的なシグナル（警告）を返す。

const (
	// upcomingDays は「期日が近い」とみなすマイルストーンの日数
	upcomingDays = 30
	// 警告のしきい値（docs/plan.md「2.8」の固定値）
	downwardRatePct  = 10      // 下方修正: 比較シナリオからの低下の率（%）
	downwardMinYen   = 100_000 // 下方修正: 低下の金額の下限（円）
	accuracyRatePct  = 20      // 見込の当たり具合: 差の率（%）
	accuracyMonths   = 3       // 見込の当たり具合: 直近の決算確定済みの月数
	consecutiveDepth = 3       // 連続の下方修正: 複製元をたどる版の数
)

// pl は収益と費用の合計（円、文字列）。
type pl struct {
	Revenue string `json:"revenue"`
	Expense string `json:"expense"`
}

// plSum は収益と費用の合計（有理数。加重見込の端数を最後に丸める）。
type plSum struct {
	revenue, expense *big.Rat
}

func newPLSum() plSum { return plSum{new(big.Rat), new(big.Rat)} }

func (p plSum) add(category string, v *big.Rat) {
	if category == "revenue" {
		p.revenue.Add(p.revenue, v)
	} else {
		p.expense.Add(p.expense, v)
	}
}

func (p plSum) profit() *big.Rat { return new(big.Rat).Sub(p.revenue, p.expense) }

func (p plSum) pl() pl { return pl{Revenue: roundYen(p.revenue), Expense: roundYen(p.expense)} }

// roundYen は円未満を四捨五入した金額を返す。
func roundYen(r *big.Rat) string {
	return formula.RoundHalfUp(r).String()
}

type riskMilestone struct {
	Name    string `json:"name"`
	DueDate string `json:"due_date"`
	Status  string `json:"status"`
	// Risk は overdue（期日超過）/ delayed（遅延）/ upcoming（期日が近い。注意で、警告には数えない）
	Risk string `json:"risk"`
}

// riskLine は内訳（line_id が nil は科目への直接入力）の、期間の満額。
type riskLine struct {
	LineID          *int64 `json:"line_id"`
	SubjectID       int64  `json:"subject_id"`
	Name            string `json:"name"`
	ConfidenceLevel string `json:"confidence_level"` // 実際に使う段階（内訳の上書き、なければ施策）
	Outlook         string `json:"outlook"`
	Amount          string `json:"amount"`
}

// riskWarnings は客観的なシグナル（docs/plan.md「2.8」）。
type riskWarnings struct {
	Milestones []riskMilestone `json:"milestones"`
	// Postponed は比較シナリオの作成以降に、期日を後ろにずらした回数と日数
	Postponed struct {
		Count int `json:"count"`
		Days  int `json:"days"`
	} `json:"postponed"`
	// Downward は下方修正（年間の加重見込の利益の、比較シナリオからの低下）。しきい値を超えたときだけ
	Downward *struct {
		Diff string  `json:"diff"`
		Rate float64 `json:"rate"`
	} `json:"downward"`
	// Consecutive は、複製元をたどった直近の見込で、加重見込の利益が2回続けて下がった
	Consecutive bool `json:"consecutive"`
	// Accuracy は見込の当たり具合（直近の決算確定済みの月の、比較シナリオの計画値と実績の差の率）。しきい値を超えたときだけ
	Accuracy *float64 `json:"accuracy"`
}

// RiskActivity は施策ごとのリスク情報。
type RiskActivity struct {
	ID              int64  `json:"id"`
	Code            string `json:"code"`
	Name            string `json:"name"`
	UnitID          int64  `json:"unit_id"`
	OwnerUserID     *int64 `json:"owner_user_id"`
	ActivityType    string `json:"activity_type"`
	Status          string `json:"status"`
	ConfidenceLevel string `json:"confidence_level"`
	Assumptions     string `json:"assumptions"`

	// 期間の楽観・基準（加重見込）・悲観・満額
	Full        pl `json:"full"`
	Weighted    pl `json:"weighted"`
	Optimistic  pl `json:"optimistic"`
	Pessimistic pl `json:"pessimistic"`
	// Actual は期間のうち、実績の月の収益・費用
	Actual pl `json:"actual"`
	// RevenueByLevel は期間の売上（満額）を「実績・段階のコード・ダウンサイド」に分けたもの
	RevenueByLevel map[string]string `json:"revenue_by_level"`
	// RevenueByMonth は月ごとの売上（満額）を、RevenueByLevel と同じキーに分けたもの（施策の一覧の「確度の推移」。docs/plan.md「2.22」）
	RevenueByMonth map[string]map[string]string `json:"revenue_by_month"`
	// Compare は比較シナリオの、期間の加重見込
	Compare *pl        `json:"compare"`
	Lines   []riskLine `json:"lines"`

	Warnings riskWarnings `json:"warnings"`
	// WarningCount は警告の数（マイルストーンの期日が近いは数えない）
	WarningCount int `json:"warning_count"`
	// BadForLevel は「段階に対して状況が悪い」（確度の高い段階で、警告が1つ以上）
	BadForLevel bool `json:"bad_for_level"`
	// Conditions はシナリオごとの今回の見込の説明（scenario / compare）
	Conditions map[string]string `json:"conditions"`
	// CommentCount は基準シナリオの説明へのコメントの件数（消したものを除く、docs/plan.md「2.24」）
	CommentCount int `json:"comment_count"`

	sums map[string]plSum // 指標ごとの期間の合計
	year map[string]plSum // 指標ごとの年間の合計（シグナルに使う）
	high bool             // 施策か内訳に確度の高い段階がある
}

type riskScenario struct {
	ID            int64   `json:"id"`
	Name          string  `json:"name"`
	PlanRole      *string `json:"plan_role"`
	ActualThrough *string `json:"actual_through"`
	BaseID        *int64  `json:"base_scenario_id"`
	fiscalYear    int
	createdAt     time.Time
}

type riskLevel struct {
	Code string      `json:"code"`
	Name string      `json:"name"`
	Rate json.Number `json:"rate"`
	High bool        `json:"high"` // 確度の高い段階（悲観で計上する）
}

type riskResponse struct {
	FiscalYear int            `json:"fiscal_year"`
	Today      string         `json:"today"`
	Period     string         `json:"period"`
	Months     []string       `json:"months"`
	Scenario   riskScenario   `json:"scenario"`
	Compare    *riskScenario  `json:"compare"`
	Levels     []riskLevel    `json:"levels"`
	Activities []RiskActivity `json:"activities"`
	// RestrictedHidden は、閲覧制限のある科目を除いた金額か（docs/plan.md「2.17」）
	RestrictedHidden bool `json:"restricted_hidden"`
}

func findRiskScenario(ctx context.Context, db *sql.DB, id int64, field string) (riskScenario, error) {
	var s riskScenario
	var role, through sql.NullString
	var base sql.NullInt64
	err := db.QueryRowContext(ctx, "SELECT id, name, plan_role, DATE_FORMAT(actual_through, '%Y-%m'), base_scenario_id, fiscal_year, created_at FROM scenarios WHERE id = ?", id).
		Scan(&s.ID, &s.Name, &role, &through, &base, &s.fiscalYear, &s.createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return s, httpx.Validation(map[string]string{field: fmt.Sprintf("シナリオ %d が見つかりません", id)})
	}
	if role.Valid {
		s.PlanRole = &role.String
	}
	if through.Valid {
		s.ActualThrough = &through.String
	}
	if base.Valid {
		s.BaseID = &base.Int64
	}
	return s, err
}

// risk は GET /api/reports/risk?scenario_id=&compare_id=&period=year|remaining。
func (h *Handler) risk(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	q := r.URL.Query()
	u, ok := auth.UserFrom(ctx)
	if !ok {
		return httpx.Unauthorized("ログインしてください")
	}
	// 閲覧制限のある科目（docs/plan.md「2.17」）を除く条件
	filter := func(col string) string { return visibility.SubjectFilter(u, col) }
	parseID := func(p string) (*int64, error) {
		s := q.Get(p)
		if s == "" {
			return nil, nil
		}
		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, httpx.BadRequest(p + " は数値で指定してください")
		}
		return &id, nil
	}
	sid, err := parseID("scenario_id")
	if err != nil {
		return err
	}
	if sid == nil {
		return httpx.Validation(map[string]string{"scenario_id": "基準のシナリオを選んでください"})
	}
	cid, err := parseID("compare_id")
	if err != nil {
		return err
	}
	period := q.Get("period")
	if period == "" {
		period = "year"
	}
	if period != "year" && period != "remaining" {
		return httpx.BadRequest("period は year / remaining のいずれかを指定してください")
	}

	s, err := findRiskScenario(ctx, h.db, *sid, "scenario_id")
	if err != nil {
		return err
	}
	resp := riskResponse{FiscalYear: s.fiscalYear, Period: period, Scenario: s, Activities: []RiskActivity{}}
	if resp.RestrictedHidden, err = visibility.Hidden(ctx, h.db, u); err != nil {
		return err
	}
	var compare *riskScenario
	if cid != nil {
		c, err := findRiskScenario(ctx, h.db, *cid, "compare_id")
		if err != nil {
			return err
		}
		if c.fiscalYear != s.fiscalYear {
			return httpx.Validation(map[string]string{"compare_id": "基準と同じ年度のシナリオを選んでください"})
		}
		compare = &c
		resp.Compare = compare
	}
	jst, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		jst = time.FixedZone("Asia/Tokyo", 9*60*60)
	}
	today := time.Now().In(jst)
	resp.Today = today.Format("2006-01-02")

	allMonths := calc.FiscalMonths(s.fiscalYear)
	actualThrough := ""
	if s.ActualThrough != nil {
		actualThrough = *s.ActualThrough
	}
	resp.Months = allMonths
	if period == "remaining" {
		resp.Months = slices.DeleteFunc(slices.Clone(allMonths), func(m string) bool { return m <= actualThrough })
	}
	inPeriod := map[string]bool{}
	for _, m := range resp.Months {
		inPeriod[m] = true
	}

	if resp.Levels, err = loadLevels(ctx, h.db); err != nil {
		return err
	}
	acts, index, err := loadRiskActivities(ctx, h.db, resp.Levels)
	if err != nil {
		return err
	}
	// 集計は互いに関係しない（施策の別々の項目に書く）ので、並行して読む（I-15）。比較との差と連続の下方修正は、そろってから判定する
	var cmpIndex map[int64]*RiskActivity
	var chain []map[int64]*big.Rat
	tasks := []func() error{
		func() error { return accumulate(ctx, h.db, filter, s.ID, index, inPeriod) },
		func() error { return fillLines(ctx, h.db, filter, s.ID, resp.Months, index) },
		func() error { return fillRiskMilestones(ctx, h.db, index, today) },
		func() error { return fillConditions(ctx, h.db, s.ID, compare, index) },
		func() (err error) { chain, err = consecutiveChain(ctx, h.db, filter, s, resp.Levels); return err },
	}
	// 比較シナリオ: 期間の加重見込、下方修正、当たり具合、後ろ倒し
	if compare != nil {
		tasks = append(tasks,
			func() error {
				cmp, _, err := loadRiskActivities(ctx, h.db, resp.Levels)
				if err != nil {
					return err
				}
				cmpIndex = map[int64]*RiskActivity{}
				for i := range cmp {
					cmpIndex[cmp[i].ID] = &cmp[i]
				}
				return accumulate(ctx, h.db, filter, compare.ID, cmpIndex, inPeriod)
			},
			func() error { return fillAccuracy(ctx, h.db, filter, s, *compare, index) },
			func() error { return fillPostponed(ctx, h.db, compare.createdAt, index) },
		)
	}
	if err := runParallel(tasks); err != nil {
		return err
	}
	if compare != nil {
		for _, a := range index {
			c := cmpIndex[a.ID]
			v := c.sums["weighted"].pl()
			a.Compare = &v
			a.Warnings.Downward = downward(a.year["weighted"].profit(), c.year["weighted"].profit())
		}
	}
	applyConsecutive(index, chain)

	for i := range acts {
		a := &acts[i]
		a.Full, a.Weighted = a.sums["full"].pl(), a.sums["weighted"].pl()
		a.Optimistic, a.Pessimistic, a.Actual = a.sums["optimistic"].pl(), a.sums["pessimistic"].pl(), a.sums["actual"].pl()
		n := 0
		for _, m := range a.Warnings.Milestones {
			if m.Risk != "upcoming" {
				n++
				break
			}
		}
		if a.Warnings.Postponed.Count > 0 {
			n++
		}
		if a.Warnings.Downward != nil {
			n++
		}
		if a.Warnings.Consecutive {
			n++
		}
		if a.Warnings.Accuracy != nil {
			n++
		}
		a.WarningCount = n
		a.BadForLevel = a.high && n > 0
	}
	resp.Activities = acts
	httpx.WriteJSON(w, http.StatusOK, resp)
	return nil
}

func loadLevels(ctx context.Context, db *sql.DB) ([]riskLevel, error) {
	rows, err := db.QueryContext(ctx, "SELECT code, name, rate, rate >= "+highRate+" FROM confidence_levels ORDER BY sort_order, code")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []riskLevel{}
	for rows.Next() {
		var l riskLevel
		var rate string
		if err := rows.Scan(&l.Code, &l.Name, &rate, &l.High); err != nil {
			return nil, err
		}
		if strings.Contains(rate, ".") {
			rate = strings.TrimRight(strings.TrimRight(rate, "0"), ".")
		}
		l.Rate = json.Number(rate)
		out = append(out, l)
	}
	return out, rows.Err()
}

func loadRiskActivities(ctx context.Context, db *sql.DB, levels []riskLevel) ([]RiskActivity, map[int64]*RiskActivity, error) {
	high := map[string]bool{}
	for _, l := range levels {
		high[l.Code] = l.High
	}
	rows, err := db.QueryContext(ctx, `
		SELECT id, code, name, unit_id, owner_user_id, activity_type, status, confidence_level, COALESCE(assumptions, '')
		FROM activities ORDER BY code`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	out := []RiskActivity{}
	for rows.Next() {
		var a RiskActivity
		var owner sql.NullInt64
		if err := rows.Scan(&a.ID, &a.Code, &a.Name, &a.UnitID, &owner, &a.ActivityType, &a.Status, &a.ConfidenceLevel, &a.Assumptions); err != nil {
			return nil, nil, err
		}
		if owner.Valid {
			o := owner.Int64
			a.OwnerUserID = &o
		}
		a.high = high[a.ConfidenceLevel]
		a.sums, a.year = map[string]plSum{}, map[string]plSum{}
		for _, k := range []string{"full", "weighted", "optimistic", "pessimistic", "actual"} {
			a.sums[k], a.year[k] = newPLSum(), newPLSum()
		}
		a.RevenueByLevel = map[string]string{}
		a.RevenueByMonth = map[string]map[string]string{}
		a.Lines = []riskLine{}
		a.Warnings.Milestones = []riskMilestone{}
		a.Conditions = map[string]string{}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	index := map[int64]*RiskActivity{}
	for i := range out {
		index[out[i].ID] = &out[i]
	}
	return out, index, nil
}

// accumulate はシナリオの金額を、内訳の段階・見通しの種類ごとに読み、施策ごとの指標（docs/plan.md「2.8」）を合計する。
func accumulate(ctx context.Context, db *sql.DB, filter func(string) string, scenarioID int64, index map[int64]*RiskActivity, inPeriod map[string]bool) error {
	rows, err := db.QueryContext(ctx, `
		SELECT b.activity_id, s.category, b.kind, cl.code, cl.rate, cl.rate >= `+highRate+`, COALESCE(l.outlook, 'base'),
		       DATE_FORMAT(b.target_month, '%Y-%m'), CAST(SUM(b.amount) AS CHAR)
		FROM scenario_amounts b JOIN subjects s ON s.id = b.subject_id`+measureJoins+`
		WHERE b.scenario_id = ?`+filter("b.subject_id")+`
		GROUP BY b.activity_id, s.category, b.kind, cl.code, cl.rate, l.outlook, b.target_month`, scenarioID)
	if err != nil {
		return err
	}
	defer rows.Close()
	byLevel := map[int64]map[string]*big.Int{}
	type monthKey struct {
		aid        int64
		month, key string
	}
	byMonth := map[monthKey]*big.Int{}
	for rows.Next() {
		var aid int64
		var category, kind, level, rateStr, outlook, month, amount string
		var high bool
		if err := rows.Scan(&aid, &category, &kind, &level, &rateStr, &high, &outlook, &month, &amount); err != nil {
			return err
		}
		a := index[aid]
		if a == nil {
			continue
		}
		v, _ := new(big.Rat).SetString(amount)
		rate, _ := new(big.Rat).SetString(rateStr)
		zero := new(big.Rat)
		values := map[string]*big.Rat{"full": v, "weighted": v, "optimistic": v, "pessimistic": v, "actual": zero}
		if kind == "actual" {
			values["actual"] = v
		} else {
			values["weighted"] = new(big.Rat).Mul(v, rate)
			if outlook == "downside" {
				values["optimistic"] = zero
			} else if !high {
				values["pessimistic"] = zero
			}
			if high {
				a.high = true
			}
		}
		for k, x := range values {
			a.year[k].add(category, x)
			if inPeriod[month] {
				a.sums[k].add(category, x)
			}
		}
		if inPeriod[month] && category == "revenue" {
			key := level
			if kind == "actual" {
				key = "actual"
			} else if outlook == "downside" {
				key = "downside"
			}
			if byLevel[aid] == nil {
				byLevel[aid] = map[string]*big.Int{}
			}
			if byLevel[aid][key] == nil {
				byLevel[aid][key] = new(big.Int)
			}
			n, _ := new(big.Int).SetString(amount, 10)
			byLevel[aid][key].Add(byLevel[aid][key], n)
			mk := monthKey{aid, month, key}
			if byMonth[mk] == nil {
				byMonth[mk] = new(big.Int)
			}
			byMonth[mk].Add(byMonth[mk], n)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for aid, m := range byLevel {
		for k, v := range m {
			index[aid].RevenueByLevel[k] = v.String()
		}
	}
	for k, v := range byMonth {
		a := index[k.aid]
		if a.RevenueByMonth[k.month] == nil {
			a.RevenueByMonth[k.month] = map[string]string{}
		}
		a.RevenueByMonth[k.month][k.key] = v.String()
	}
	return nil
}

// fillLines は内訳ごとの段階・見通しの種類と、期間の計画値の月の満額を加える。科目への直接入力は、科目ごとに1行（line_id が nil）にまとめる。
func fillLines(ctx context.Context, db *sql.DB, filter func(string) string, scenarioID int64, months []string, index map[int64]*RiskActivity) error {
	type key struct {
		activity, subject int64
		line              int64 // 0 は直接入力
	}
	amounts := map[key]string{}
	if len(months) > 0 {
		args := []any{scenarioID}
		for _, m := range months {
			args = append(args, m+"-01")
		}
		rows, err := db.QueryContext(ctx, `
			SELECT activity_id, subject_id, COALESCE(line_id, 0), CAST(SUM(amount) AS CHAR)
			FROM scenario_amounts WHERE scenario_id = ? AND kind = 'plan' AND target_month IN (`+strings.TrimSuffix(strings.Repeat("?,", len(months)), ",")+`)`+filter("subject_id")+`
			GROUP BY activity_id, subject_id, line_id`, args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var k key
			var v string
			if err := rows.Scan(&k.activity, &k.subject, &k.line, &v); err != nil {
				rows.Close()
				return err
			}
			amounts[k] = v
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
	}
	rows, err := db.QueryContext(ctx, `
		SELECT l.id, l.activity_id, l.subject_id, l.name, COALESCE(l.confidence_level, a.confidence_level), l.outlook
		FROM activity_lines l JOIN activities a ON a.id = l.activity_id WHERE 1 = 1`+filter("l.subject_id")+` ORDER BY l.activity_id, l.subject_id, l.id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var x riskLine
		var id, aid int64
		if err := rows.Scan(&id, &aid, &x.SubjectID, &x.Name, &x.ConfidenceLevel, &x.Outlook); err != nil {
			return err
		}
		x.LineID = &id
		x.Amount = "0"
		if v, ok := amounts[key{aid, x.SubjectID, id}]; ok {
			x.Amount = v
		}
		if a := index[aid]; a != nil {
			a.Lines = append(a.Lines, x)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	// 科目への直接入力（施策の段階・ベース）
	for k, v := range amounts {
		if k.line != 0 {
			continue
		}
		if a := index[k.activity]; a != nil {
			a.Lines = append(a.Lines, riskLine{SubjectID: k.subject, Name: "科目への直接入力", ConfidenceLevel: a.ConfidenceLevel, Outlook: "base", Amount: v})
		}
	}
	// 科目の順、内訳の順（直接入力は最後）
	for _, a := range index {
		slices.SortStableFunc(a.Lines, func(x, y riskLine) int {
			if x.SubjectID != y.SubjectID {
				return int(x.SubjectID - y.SubjectID)
			}
			if (x.LineID == nil) != (y.LineID == nil) {
				if x.LineID == nil {
					return 1
				}
				return -1
			}
			if x.LineID == nil {
				return 0
			}
			return int(*x.LineID - *y.LineID)
		})
	}
	return nil
}

// downward は、加重見込の利益が比較シナリオから 10% 以上かつ 10万円以上下がったら、その幅と率を返す。
func downward(cur, cmp *big.Rat) *struct {
	Diff string  `json:"diff"`
	Rate float64 `json:"rate"`
} {
	diff := new(big.Rat).Sub(cur, cmp)
	if diff.Sign() >= 0 {
		return nil
	}
	drop := new(big.Rat).Neg(diff)
	if drop.Cmp(big.NewRat(downwardMinYen, 1)) < 0 {
		return nil
	}
	base := new(big.Rat).Abs(cmp)
	var rate float64
	if base.Sign() != 0 {
		rate, _ = new(big.Rat).Quo(new(big.Rat).Mul(drop, big.NewRat(100, 1)), base).Float64()
		if rate < downwardRatePct {
			return nil
		}
	} else {
		rate = 100
	}
	return &struct {
		Diff string  `json:"diff"`
		Rate float64 `json:"rate"`
	}{roundYen(diff), float64(int64(rate*10+0.5)) / 10}
}

// fillAccuracy は見込の当たり具合を加える: 基準の決算確定済みの直近3か月で、比較シナリオの計画値と実績の差の率が 20% 以上。
func fillAccuracy(ctx context.Context, db *sql.DB, filter func(string) string, s, compare riskScenario, index map[int64]*RiskActivity) error {
	if s.ActualThrough == nil {
		return nil
	}
	var months []string
	for _, m := range calc.FiscalMonths(s.fiscalYear) {
		if m <= *s.ActualThrough {
			months = append(months, m)
		}
	}
	if len(months) > accuracyMonths {
		months = months[len(months)-accuracyMonths:]
	}
	if len(months) == 0 {
		return nil
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(months)), ",")
	args := []any{compare.ID}
	for _, m := range months {
		args = append(args, m+"-01")
	}
	args = append(args, s.ID)
	for _, m := range months {
		args = append(args, m+"-01")
	}
	// 比較シナリオの計画値（計画値の月だけ）と、基準の実績を、施策 × 科目 × 月で比べる
	rows, err := db.QueryContext(ctx, `
		SELECT activity_id, CAST(SUM(ABS(diff)) AS CHAR), CAST(SUM(ABS(plan)) AS CHAR)
		FROM (
			SELECT activity_id, subject_id, target_month, SUM(actual) - SUM(plan) AS diff, SUM(plan) AS plan
			FROM (
				SELECT activity_id, subject_id, target_month, amount AS plan, 0 AS actual
				FROM scenario_amounts WHERE scenario_id = ? AND kind = 'plan' AND target_month IN (`+ph+`)`+filter("subject_id")+`
				UNION ALL
				SELECT activity_id, subject_id, target_month, 0, amount
				FROM scenario_amounts WHERE scenario_id = ? AND kind = 'actual' AND target_month IN (`+ph+`)`+filter("subject_id")+`
			) x GROUP BY activity_id, subject_id, target_month
		) y GROUP BY activity_id`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var aid int64
		var d, p string
		if err := rows.Scan(&aid, &d, &p); err != nil {
			return err
		}
		a := index[aid]
		dv, _ := new(big.Rat).SetString(d)
		pv, _ := new(big.Rat).SetString(p)
		if a == nil || pv.Sign() == 0 {
			continue
		}
		rate, _ := new(big.Rat).Quo(new(big.Rat).Mul(dv, big.NewRat(100, 1)), pv).Float64()
		if rate >= accuracyRatePct {
			r := float64(int64(rate*10+0.5)) / 10
			a.Warnings.Accuracy = &r
		}
	}
	return rows.Err()
}

// fillPostponed は、比較シナリオの作成以降に期日を後ろにずらしたマイルストーンの変更の回数と日数を加える。
func fillPostponed(ctx context.Context, db *sql.DB, since time.Time, index map[int64]*RiskActivity) error {
	rows, err := db.QueryContext(ctx, `
		SELECT m.activity_id, COUNT(*),
		       COALESCE(SUM(DATEDIFF(JSON_UNQUOTE(JSON_EXTRACT(a.after_json, '$.due_date')), JSON_UNQUOTE(JSON_EXTRACT(a.before_json, '$.due_date')))), 0)
		FROM audit_logs a JOIN activity_milestones m ON m.id = a.record_id
		WHERE a.table_name = 'activity_milestones' AND a.action = 'update' AND a.created_at >= ?
		  AND JSON_UNQUOTE(JSON_EXTRACT(a.after_json, '$.due_date')) > JSON_UNQUOTE(JSON_EXTRACT(a.before_json, '$.due_date'))
		GROUP BY m.activity_id`, since)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var aid int64
		var count, days int
		if err := rows.Scan(&aid, &count, &days); err != nil {
			return err
		}
		if a := index[aid]; a != nil {
			a.Warnings.Postponed.Count, a.Warnings.Postponed.Days = count, days
		}
	}
	return rows.Err()
}

// consecutiveChain は、複製元をたどった直近の版（今回を除く、同じ年度）の、施策ごとの年間の加重見込の利益を返す。
// 版ごとの集計は並行して読む。
func consecutiveChain(ctx context.Context, db *sql.DB, filter func(string) string, s riskScenario, levels []riskLevel) ([]map[int64]*big.Rat, error) {
	var prevs []riskScenario
	cur := s
	for len(prevs) < consecutiveDepth-1 && cur.BaseID != nil {
		prev, err := findRiskScenario(ctx, db, *cur.BaseID, "scenario_id")
		if err != nil {
			return nil, err
		}
		if prev.fiscalYear != s.fiscalYear {
			break
		}
		prevs = append(prevs, prev)
		cur = prev
	}
	chain := make([]map[int64]*big.Rat, len(prevs))
	tasks := make([]func() error, len(prevs))
	for i, prev := range prevs {
		tasks[i] = func() error {
			_, idx, err := loadRiskActivities(ctx, db, levels)
			if err != nil {
				return err
			}
			if err := accumulate(ctx, db, filter, prev.ID, idx, map[string]bool{}); err != nil {
				return err
			}
			chain[i] = profits(idx)
			return nil
		}
	}
	return chain, runParallel(tasks)
}

// applyConsecutive は、今回と直近の版で、加重見込の利益が続けて下がった施策に「連続の下方修正」を付ける。
func applyConsecutive(index map[int64]*RiskActivity, prevs []map[int64]*big.Rat) {
	if len(prevs) < consecutiveDepth-1 {
		return
	}
	current := profits(index)
	for id, a := range index {
		p0, p1, p2 := current[id], prevs[0][id], prevs[1][id]
		if p0 != nil && p1 != nil && p2 != nil && p0.Cmp(p1) < 0 && p1.Cmp(p2) < 0 {
			a.Warnings.Consecutive = true
		}
	}
}

// runParallel は処理を並行して実行し、最初のエラーを返す（すべての完了を待つ）。
func runParallel(tasks []func() error) error {
	errs := make([]error, len(tasks))
	var wg sync.WaitGroup
	for i, t := range tasks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = t()
		}()
	}
	wg.Wait()
	return errors.Join(errs...)
}

func profits(index map[int64]*RiskActivity) map[int64]*big.Rat {
	out := map[int64]*big.Rat{}
	for id, a := range index {
		out[id] = a.year["weighted"].profit()
	}
	return out
}

// fillRiskMilestones は完了していないマイルストーンのうち、期日超過・遅延・期日が近いものを施策に加える。
func fillRiskMilestones(ctx context.Context, db *sql.DB, index map[int64]*RiskActivity, today time.Time) error {
	todayStr := today.Format("2006-01-02")
	soon := today.AddDate(0, 0, upcomingDays).Format("2006-01-02")
	rows, err := db.QueryContext(ctx, `
		SELECT activity_id, name, DATE_FORMAT(due_date, '%Y-%m-%d'), status
		FROM activity_milestones
		WHERE status <> 'completed' AND (status = 'delayed' OR due_date <= ?)
		ORDER BY due_date, id`, soon)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var aid int64
		var m riskMilestone
		if err := rows.Scan(&aid, &m.Name, &m.DueDate, &m.Status); err != nil {
			return err
		}
		switch {
		case m.DueDate < todayStr:
			m.Risk = "overdue"
		case m.Status == "delayed":
			m.Risk = "delayed"
		default:
			m.Risk = "upcoming"
		}
		if a, ok := index[aid]; ok {
			a.Warnings.Milestones = append(a.Warnings.Milestones, m)
		}
	}
	return rows.Err()
}

// fillConditions は基準・比較シナリオの、今回の見込の説明（docs/plan.md「2.18」で想定条件を統合）と、基準シナリオのコメントの件数を加える。
func fillConditions(ctx context.Context, db *sql.DB, scenarioID int64, compare *riskScenario, index map[int64]*RiskActivity) error {
	targets := map[string]int64{"scenario": scenarioID}
	if compare != nil {
		targets["compare"] = compare.ID
	}
	for key, id := range targets {
		rows, err := db.QueryContext(ctx, "SELECT activity_id, explanation FROM activity_scenario_notes WHERE scenario_id = ? AND COALESCE(explanation, '') <> ''", id)
		if err != nil {
			return err
		}
		for rows.Next() {
			var aid int64
			var desc string
			if err := rows.Scan(&aid, &desc); err != nil {
				rows.Close()
				return err
			}
			if a, ok := index[aid]; ok {
				a.Conditions[key] = desc
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
	}
	// 説明へのコメントの件数
	rows, err := db.QueryContext(ctx, "SELECT activity_id, COUNT(*) FROM activity_scenario_comments WHERE scenario_id = ? AND deleted_at IS NULL GROUP BY activity_id", scenarioID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var aid int64
		var n int
		if err := rows.Scan(&aid, &n); err != nil {
			return err
		}
		if a, ok := index[aid]; ok {
			a.CommentCount = n
		}
	}
	return rows.Err()
}
