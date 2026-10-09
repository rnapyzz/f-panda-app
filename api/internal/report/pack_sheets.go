package report

import (
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rnapyzz/f-panda-app/api/internal/xlsx"
)

// 報告資料（docs/plan.md「2.23」）のシートの組み立て。DB を読まず、集めたデータ（packInput）から作る。
// 集計（科目体系・組織の階層の合計、利益、差）は画面の予実比較（web/src/lib/aggregate.ts）と同じ考え方で、BigInt で計算する。

var measureLabels = map[string]string{"full": "満額", "weighted": "加重見込", "optimistic": "楽観", "pessimistic": "悲観"}

var grainLabels = map[string]string{"month": "月次", "quarter": "四半期", "half": "半期", "year": "通期"}

var sheetLabels = map[string]string{"pl": "全社 P/L", "units": "ユニット別 P/L", "notes": "変動の説明"}

var noteStatusLabels = map[string]string{"not_started": "未着手", "in_progress": "入力中", "completed": "完了"}

var noteCauseLabels = map[string]string{
	"timing":     "時期のずれ",
	"volume":     "数量・単価の増減",
	"new":        "新規",
	"lost":       "失注・解約",
	"assumption": "前提の変化",
	"other":      "その他",
}

// packSeries は報告資料の系列（シナリオ、または実績）。
type packSeries struct {
	label         string
	scenarioID    int64 // 実績は 0
	actualThrough *string
}

// subjectNode は科目体系の1つの科目。
type subjectNode struct {
	id       int64
	name     string
	category string // revenue / expense
	children []*subjectNode
}

type packUnit struct {
	id        int64
	code      string
	name      string
	segmentID int64
	archived  bool
}

type packNode struct {
	id, parentID int64 // ルートは parentID 0
	name         string
}

type packActivity struct {
	id, unitID int64
	code, name string
}

// noteRow は変動の説明の1行。差は年間の利益（今回 − 比較先）。比較先のシナリオがなければ nil。
type noteRow struct {
	code, name, unit, owner, status string
	current                         *big.Int
	baseDiff, previousDiff          *big.Int
	causes                          []string
	explanation                     string
}

// packNotes は変動の説明のシート。scenario が空なら、作成中のシナリオがない。
type packNotes struct {
	scenario, base, previous string
	rows                     []noteRow
}

// packInput は報告資料を作るのに使うデータ。
type packInput struct {
	fiscalYear     int
	measure, grain string
	months         []string
	series         []packSeries
	// values[i] は系列 i の、ユニット（施策）× 科目 × 月の金額。範囲で絞り込み済み。ユニット ID 0 は未割当の実績
	values           []map[rowKey]*big.Int
	subjects         []*subjectNode // ルートの科目（並び順）
	units            []packUnit     // 範囲のユニット（コード順）
	segments         []packNode     // セグメント（並び順）
	activities       []packActivity // 施策（コード順）。ユニット別 P/L に施策を含めるときだけ
	withActivities   bool
	allUnits         bool // 全社（未割当の実績を含む）
	scopeLabel       string
	sheets           []string
	restrictedHidden bool
	userName         string
	now              time.Time
	notes            *packNotes
}

type period struct {
	label  string
	months []string
}

// periodsOf は年度の月を期間に分ける。月次・四半期・半期のときは、最後に通期を付ける（web/src/lib/pl.ts の periodsOf と同じ）。
func periodsOf(months []string, grain string) []period {
	chunk := func(size int, label func(i int) string) []period {
		var out []period
		for i := 0; i*size < len(months); i++ {
			out = append(out, period{label(i), months[i*size : min((i+1)*size, len(months))]})
		}
		return out
	}
	total := period{"通期", months}
	switch grain {
	case "month":
		var out []period
		for _, m := range months {
			n, _ := strconv.Atoi(m[5:7])
			out = append(out, period{fmt.Sprintf("%d月", n), []string{m}})
		}
		return append(out, total)
	case "quarter":
		return append(chunk(3, func(i int) string { return fmt.Sprintf("Q%d", i+1) }), total)
	case "half":
		return append(chunk(6, func(i int) string { return map[int]string{0: "上期", 1: "下期"}[i] }), total)
	}
	return []period{total}
}

// buildPack はシートを組み立てる。
func buildPack(in *packInput) []xlsx.Sheet {
	sheets := []xlsx.Sheet{conditionsSheet(in)}
	for _, s := range in.sheets {
		switch s {
		case "pl":
			sheets = append(sheets, plSheet(in))
		case "units":
			sheets = append(sheets, unitsSheet(in))
		case "notes":
			sheets = append(sheets, notesSheet(in))
		}
	}
	return sheets
}

func conditionsSheet(in *packInput) xlsx.Sheet {
	rows := [][]xlsx.Cell{{xlsx.T("報告資料の条件", xlsx.Title)}, {}}
	add := func(label, value string) {
		rows = append(rows, []xlsx.Cell{xlsx.T(label, xlsx.Bold), xlsx.T(value, xlsx.Plain)})
	}
	add("年度", fmt.Sprintf("%d年度", in.fiscalYear))
	for i, s := range in.series {
		label := ""
		if i == 0 {
			label = "系列"
		}
		v := fmt.Sprintf("%d. %s", i+1, s.label)
		if i == 0 {
			v += "（比較元）"
		}
		if s.actualThrough != nil {
			v += fmt.Sprintf("（%s まで実績）", *s.actualThrough)
		}
		add(label, v)
	}
	add("金額", measureLabels[in.measure])
	add("期間", grainLabels[in.grain])
	add("範囲", in.scopeLabel)
	var names []string
	for _, s := range in.sheets {
		names = append(names, sheetLabels[s])
	}
	add("シート", strings.Join(names, "・"))
	if contains(in.sheets, "units") {
		add("ユニット別 P/L の施策", map[bool]string{true: "含める", false: "含めない"}[in.withActivities])
	}
	add("単位", "円")
	add("出力した日時", in.now.Format("2006-01-02 15:04"))
	add("出力した人", in.userName)
	if in.restrictedHidden {
		rows = append(rows, []xlsx.Cell{}, []xlsx.Cell{xlsx.T("閲覧制限のある科目を除いた金額です", xlsx.Muted)})
	}
	return xlsx.Sheet{Name: "条件", Rows: rows, Widths: []float64{22, 60}}
}

// subjectMonths は系列ごとの、科目 × 月の金額（ユニット・施策を合算）。
func subjectMonths(in *packInput) []map[int64]map[string]*big.Int {
	out := make([]map[int64]map[string]*big.Int, len(in.series))
	for i, vals := range in.values {
		m := map[int64]map[string]*big.Int{}
		for k, v := range vals {
			if m[k.subjectID] == nil {
				m[k.subjectID] = map[string]*big.Int{}
			}
			addTo(m[k.subjectID], k.month, v)
		}
		out[i] = m
	}
	return out
}

// categories は科目 ID → 区分（revenue / expense）。
func categories(nodes []*subjectNode) map[int64]string {
	out := map[int64]string{}
	var walk func(nodes []*subjectNode)
	walk = func(nodes []*subjectNode) {
		for _, s := range nodes {
			out[s.id] = s.category
			walk(s.children)
		}
	}
	walk(nodes)
	return out
}

func addTo[K comparable](m map[K]*big.Int, k K, v *big.Int) {
	if cur, ok := m[k]; ok {
		cur.Add(cur, v)
		return
	}
	m[k] = new(big.Int).Set(v)
}

func plSheet(in *packInput) xlsx.Sheet {
	periods := periodsOf(in.months, in.grain)
	n := len(in.series)
	per := 2*n - 1 // 期間ごとの列: 系列と、2つ目以降の系列の比較元との差
	bySubject := subjectMonths(in)

	title := fmt.Sprintf("P/L（%s・%s・単位: 円）", in.scopeLabel, measureLabels[in.measure])
	h1 := []xlsx.Cell{xlsx.T("科目", xlsx.Header)}
	h2 := []xlsx.Cell{xlsx.Empty(xlsx.Header)}
	merges := []xlsx.Merge{{Row: 1, Col: 0, Rows: 2, Cols: 1}}
	widths := []float64{30}
	for pi, p := range periods {
		h1 = append(h1, xlsx.T(p.label, xlsx.Header))
		for range per - 1 {
			h1 = append(h1, xlsx.Empty(xlsx.Header))
		}
		if per > 1 {
			merges = append(merges, xlsx.Merge{Row: 1, Col: 1 + pi*per, Rows: 1, Cols: per})
		}
		for _, s := range in.series {
			h2 = append(h2, xlsx.T(s.label, xlsx.Header))
		}
		for _, s := range in.series[1:] {
			h2 = append(h2, xlsx.T("差（"+s.label+"）", xlsx.Header))
		}
		for range per {
			widths = append(widths, 15)
		}
	}
	rows := [][]xlsx.Cell{{xlsx.T(title, xlsx.Title)}, h1, h2}

	// 金額の行。value(i, months) は系列 i の期間の金額
	line := func(label xlsx.Cell, style xlsx.Style, value func(i int, months []string) *big.Int) []xlsx.Cell {
		row := []xlsx.Cell{label}
		for _, p := range periods {
			vs := make([]*big.Int, n)
			for i := range n {
				vs[i] = value(i, p.months)
				row = append(row, xlsx.N(vs[i], style))
			}
			for i := 1; i < n; i++ {
				row = append(row, xlsx.N(new(big.Int).Sub(vs[i], vs[0]), style))
			}
		}
		return row
	}
	var subtree func(node *subjectNode, i int, months []string) *big.Int
	subtree = func(node *subjectNode, i int, months []string) *big.Int {
		sum := new(big.Int)
		for _, m := range months {
			if v := bySubject[i][node.id][m]; v != nil {
				sum.Add(sum, v)
			}
		}
		for _, c := range node.children {
			sum.Add(sum, subtree(c, i, months))
		}
		return sum
	}
	// 区分の合計は、科目ごとの区分で合計する（子の科目の区分が親と違っても、画面の集計と同じになるように）
	categoryOf := categories(in.subjects)
	categoryTotal := func(category string) func(i int, months []string) *big.Int {
		return func(i int, months []string) *big.Int {
			sum := new(big.Int)
			for id, byMonth := range bySubject[i] {
				if categoryOf[id] != category {
					continue
				}
				for _, m := range months {
					if v := byMonth[m]; v != nil {
						sum.Add(sum, v)
					}
				}
			}
			return sum
		}
	}

	var emit func(node *subjectNode, level int)
	emit = func(node *subjectNode, level int) {
		rows = append(rows, line(xlsx.Indent(node.name, level), xlsx.Number, func(i int, months []string) *big.Int { return subtree(node, i, months) }))
		for _, c := range node.children {
			emit(c, level+1)
		}
	}
	for _, c := range []struct{ category, label, total string }{{"revenue", "収益", "収益計"}, {"expense", "費用", "費用計"}} {
		rows = append(rows, []xlsx.Cell{xlsx.T(c.label, xlsx.Bold)})
		for _, s := range in.subjects {
			if s.category == c.category {
				emit(s, 1)
			}
		}
		rows = append(rows, line(xlsx.T(c.total, xlsx.LabelSum), xlsx.NumberSum, categoryTotal(c.category)), []xlsx.Cell{})
	}
	revenue, expense := categoryTotal("revenue"), categoryTotal("expense")
	rows = append(rows, line(xlsx.T("利益", xlsx.LabelSum), xlsx.NumberSum, func(i int, months []string) *big.Int {
		return new(big.Int).Sub(revenue(i, months), expense(i, months))
	}))
	return xlsx.Sheet{Name: "全社 P／L", Rows: rows, Widths: widths, FreezeRows: 3, FreezeCols: 1, Merges: merges}
}

// packPL は収益と費用。
type packPL struct{ revenue, expense *big.Int }

func (p packPL) profit() *big.Int { return new(big.Int).Sub(p.revenue, p.expense) }

func newPackPL() packPL { return packPL{new(big.Int), new(big.Int)} }

func (p packPL) add(o packPL) {
	p.revenue.Add(p.revenue, o.revenue)
	p.expense.Add(p.expense, o.expense)
}

func addPL(m map[int64]packPL, id int64, category string, v *big.Int) {
	p, ok := m[id]
	if !ok {
		p = newPackPL()
		m[id] = p
	}
	if category == "revenue" {
		p.revenue.Add(p.revenue, v)
	} else {
		p.expense.Add(p.expense, v)
	}
}

func unitsSheet(in *packInput) xlsx.Sheet {
	n := len(in.series)
	categoryOf := categories(in.subjects)

	// 系列ごとの、ユニット・施策の年間（通期）の収益・費用
	byUnit := make([]map[int64]packPL, n)
	byActivity := make([]map[int64]packPL, n)
	hasData := map[int64]bool{} // ユニット・施策（ID は別の表なので、施策は負の ID で持つ）
	for i, vals := range in.values {
		byUnit[i], byActivity[i] = map[int64]packPL{}, map[int64]packPL{}
		for k, v := range vals {
			c := categoryOf[k.subjectID]
			if c == "" {
				continue
			}
			addPL(byUnit[i], k.unitID, c, v)
			hasData[k.unitID] = true
			if k.activityID != 0 {
				addPL(byActivity[i], k.activityID, c, v)
				hasData[-k.activityID] = true
			}
		}
	}
	plOf := func(m map[int64]packPL, id int64) packPL {
		if p, ok := m[id]; ok {
			return p
		}
		return newPackPL()
	}

	title := fmt.Sprintf("ユニット別 P/L（%s・%s・通期・単位: 円）", in.scopeLabel, measureLabels[in.measure])
	h1 := []xlsx.Cell{xlsx.T("セグメント / ユニット / 施策", xlsx.Header)}
	h2 := []xlsx.Cell{xlsx.Empty(xlsx.Header)}
	merges := []xlsx.Merge{{Row: 1, Col: 0, Rows: 2, Cols: 1}}
	widths := []float64{36}
	for i, s := range in.series {
		h1 = append(h1, xlsx.T(s.label, xlsx.Header), xlsx.Empty(xlsx.Header))
		h2 = append(h2, xlsx.T("売上", xlsx.Header), xlsx.T("利益", xlsx.Header))
		merges = append(merges, xlsx.Merge{Row: 1, Col: 1 + 2*i, Rows: 1, Cols: 2})
		widths = append(widths, 15, 15)
	}
	if n > 1 {
		h1 = append(h1, xlsx.T("利益の差（比較元: "+in.series[0].label+"）", xlsx.Header))
		for range n - 2 {
			h1 = append(h1, xlsx.Empty(xlsx.Header))
		}
		if n > 2 {
			merges = append(merges, xlsx.Merge{Row: 1, Col: 1 + 2*n, Rows: 1, Cols: n - 1})
		}
		for _, s := range in.series[1:] {
			h2 = append(h2, xlsx.T(s.label, xlsx.Header))
			widths = append(widths, 15)
		}
	}
	rows := [][]xlsx.Cell{{xlsx.T(title, xlsx.Title)}, h1, h2}
	line := func(label xlsx.Cell, style xlsx.Style, value func(i int) packPL) []xlsx.Cell {
		row := []xlsx.Cell{label}
		ps := make([]packPL, n)
		for i := range n {
			ps[i] = value(i)
			row = append(row, xlsx.N(ps[i].revenue, style), xlsx.N(ps[i].profit(), style))
		}
		for i := 1; i < n; i++ {
			row = append(row, xlsx.N(new(big.Int).Sub(ps[i].profit(), ps[0].profit()), style))
		}
		return row
	}

	// 出すユニット: 範囲のユニットのうち、廃止していないか金額のあるもの
	unitsBySegment := map[int64][]packUnit{}
	for _, u := range in.units {
		if !u.archived || hasData[u.id] {
			unitsBySegment[u.segmentID] = append(unitsBySegment[u.segmentID], u)
		}
	}
	activitiesByUnit := map[int64][]packActivity{}
	for _, a := range in.activities {
		if hasData[-a.id] {
			activitiesByUnit[a.unitID] = append(activitiesByUnit[a.unitID], a)
		}
	}
	children := map[int64][]packNode{}
	for _, s := range in.segments {
		children[s.parentID] = append(children[s.parentID], s)
	}
	// セグメントの下のユニット（子孫を含む）
	var unitsUnder func(id int64) []packUnit
	unitsUnder = func(id int64) []packUnit {
		out := append([]packUnit{}, unitsBySegment[id]...)
		for _, c := range children[id] {
			out = append(out, unitsUnder(c.id)...)
		}
		return out
	}
	var emitSegment func(s packNode, level int)
	emitSegment = func(s packNode, level int) {
		units := unitsUnder(s.id)
		if len(units) == 0 {
			return
		}
		rows = append(rows, line(xlsx.BoldIndent(s.name, level), xlsx.NumberBold, func(i int) packPL {
			sum := newPackPL()
			for _, u := range units {
				sum.add(plOf(byUnit[i], u.id))
			}
			return sum
		}))
		for _, c := range children[s.id] {
			emitSegment(c, level+1)
		}
		for _, u := range unitsBySegment[s.id] {
			rows = append(rows, line(xlsx.Indent(u.name, level+1), xlsx.Number, func(i int) packPL { return plOf(byUnit[i], u.id) }))
			if in.withActivities {
				for _, a := range activitiesByUnit[u.id] {
					rows = append(rows, line(xlsx.Indent(a.code+" "+a.name, level+2), xlsx.Number, func(i int) packPL { return plOf(byActivity[i], a.id) }))
				}
			}
		}
	}
	for _, s := range children[0] {
		emitSegment(s, 0)
	}
	if in.allUnits && hasData[0] {
		rows = append(rows, line(xlsx.T("未割当の実績", xlsx.Bold), xlsx.NumberBold, func(i int) packPL { return plOf(byUnit[i], 0) }))
	}
	rows = append(rows, line(xlsx.T("合計", xlsx.LabelSum), xlsx.NumberSum, func(i int) packPL {
		sum := newPackPL()
		for _, p := range byUnit[i] {
			sum.add(p)
		}
		return sum
	}))
	return xlsx.Sheet{Name: "ユニット別 P／L", Rows: rows, Widths: widths, FreezeRows: 3, FreezeCols: 1, Merges: merges}
}

func notesSheet(in *packInput) xlsx.Sheet {
	sheet := xlsx.Sheet{Name: "変動の説明", Widths: []float64{12, 28, 18, 14, 10, 15, 15, 15, 22, 60}}
	notes := in.notes
	if notes == nil || notes.scenario == "" {
		sheet.Rows = [][]xlsx.Cell{{xlsx.T("変動の説明", xlsx.Title)}, {xlsx.T(fmt.Sprintf("%d年度の作成中のシナリオ（今回の見込）がありません", in.fiscalYear), xlsx.Muted)}}
		return sheet
	}
	orNone := func(s string) string {
		if s == "" {
			return "なし"
		}
		return s
	}
	sub := fmt.Sprintf("目標: %s／前回の見込: %s／金額は年間の利益（満額、円）／差の大きい順", orNone(notes.base), orNone(notes.previous))
	header := []xlsx.Cell{}
	for _, h := range []string{"施策コード", "施策", "ユニット", "担当者", "状態", "今回の見込", "目標との差", "前回の見込との差", "要因", "説明"} {
		header = append(header, xlsx.T(h, xlsx.Header))
	}
	rows := [][]xlsx.Cell{{xlsx.T("変動の説明（今回の見込: "+notes.scenario+"）", xlsx.Title)}, {xlsx.T(sub, xlsx.Muted)}, header}
	num := func(v *big.Int) xlsx.Cell {
		if v == nil {
			return xlsx.Empty(xlsx.Number)
		}
		return xlsx.N(v, xlsx.Number)
	}
	for _, r := range notes.rows {
		var causes []string
		for _, c := range r.causes {
			causes = append(causes, noteCauseLabels[c])
		}
		rows = append(rows, []xlsx.Cell{
			xlsx.T(r.code, xlsx.Wrap), xlsx.T(r.name, xlsx.Wrap), xlsx.T(r.unit, xlsx.Wrap), xlsx.T(r.owner, xlsx.Wrap), xlsx.T(noteStatusLabels[r.status], xlsx.Wrap),
			num(r.current), num(r.baseDiff), num(r.previousDiff),
			xlsx.T(strings.Join(causes, "・"), xlsx.Wrap), xlsx.T(r.explanation, xlsx.Wrap),
		})
	}
	sheet.Rows = rows
	sheet.FreezeRows = 3
	sheet.FreezeCols = 2
	return sheet
}

// sortNotes は差の大きい順（目標との差、次に前回の見込との差の絶対値）に並べる。
func sortNotes(rows []noteRow) {
	abs := func(v *big.Int) *big.Int {
		if v == nil {
			return new(big.Int)
		}
		return new(big.Int).Abs(v)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if c := abs(rows[i].baseDiff).Cmp(abs(rows[j].baseDiff)); c != 0 {
			return c > 0
		}
		if c := abs(rows[i].previousDiff).Cmp(abs(rows[j].previousDiff)); c != 0 {
			return c > 0
		}
		return rows[i].code < rows[j].code
	})
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
