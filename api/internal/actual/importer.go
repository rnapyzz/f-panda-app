package actual

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/rnapyzz/f-panda-app/api/internal/audit"
	"github.com/rnapyzz/f-panda-app/api/internal/calc"
	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
)

// 実績 CSV（会計の明細）の取込（docs/plan.md「6.1」）。

const (
	maxImportBytes = 50 << 20 // 50MB（nginx の client_max_body_size と合わせる）
	maxImportRows  = 200_000
	maxRowErrors   = 100
	insertBatch    = 2000 // 1回の INSERT の行数（11列 × 2,000 = 22,000 個のプレースホルダー。上限は 65,535）
)

var (
	requiredColumns = []string{"target_month", "account_code", "amount"}
	optionalColumns = []string{"department_code", "box_code", "description"}
)

// entryRow は CSV の1データ行。
type entryRow struct {
	line        int
	month       string
	account     string
	dept        string
	box         string
	description string
	amount      *big.Int
}

// rowErrors は行エラーを上限まで集める。
type rowErrors struct {
	items []httpx.RowError
	total int
}

func (e *rowErrors) add(line int, format string, args ...any) {
	e.total++
	if len(e.items) < maxRowErrors {
		e.items = append(e.items, httpx.RowError{Line: line, Message: fmt.Sprintf(format, args...)})
	}
}

func (e *rowErrors) err() error {
	if e.total == 0 {
		return nil
	}
	msg := fmt.Sprintf("CSV に %d 件のエラーがあるため取り込みませんでした", e.total)
	if e.total > len(e.items) {
		msg += fmt.Sprintf("（先頭の %d 件を表示）", len(e.items))
	}
	return &httpx.Error{Status: http.StatusUnprocessableEntity, Code: "invalid_csv", Message: msg, Rows: e.items}
}

// parseEntriesCSV は実績 CSV の形式（ヘッダー・列数・年月・金額・長さ）を検証して行を返す。
// target_month・account_code・amount は必須の列。department_code・box_code・description は列ごと省略できる。
func parseEntriesCSV(data []byte) ([]entryRow, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")) // UTF-8 の BOM
	if !utf8.Valid(data) {
		return nil, httpx.BadRequest("CSV の文字コードが UTF-8 ではありません。UTF-8 で保存してください")
	}
	r := csv.NewReader(bytes.NewReader(data))
	r.TrimLeadingSpace = true
	r.ReuseRecord = true

	header, err := r.Read()
	if errors.Is(err, io.EOF) {
		return nil, httpx.BadRequest("CSV が空です")
	}
	if err != nil {
		return nil, httpx.BadRequest("CSV のヘッダー行を読み込めません: " + err.Error())
	}
	known := map[string]bool{}
	for _, c := range append(append([]string{}, requiredColumns...), optionalColumns...) {
		known[c] = true
	}
	headerError := httpx.BadRequest("CSV のヘッダー行は " + strings.Join(requiredColumns, ",") +
		"（必須）と " + strings.Join(optionalColumns, ",") + "（省略可）にしてください（順番は自由）")
	index := map[string]int{}
	for i, h := range header {
		h = strings.ToLower(strings.TrimSpace(h))
		if _, dup := index[h]; dup || !known[h] {
			return nil, headerError
		}
		index[h] = i
	}
	for _, c := range requiredColumns {
		if _, ok := index[c]; !ok {
			return nil, headerError
		}
	}
	r.FieldsPerRecord = len(header)
	get := func(rec []string, col string) string {
		if i, ok := index[col]; ok {
			return strings.TrimSpace(rec[i])
		}
		return ""
	}

	var rows []entryRow
	errs := &rowErrors{}
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		var parseErr *csv.ParseError
		if errors.As(err, &parseErr) {
			if errors.Is(parseErr.Err, csv.ErrFieldCount) {
				errs.add(parseErr.Line, "列の数が %d ではありません", len(header))
				continue
			}
			errs.add(parseErr.Line, "CSV の形式が正しくありません: %v", parseErr.Err)
			// 引用符の対応が崩れた場合などは以降を正しく読めないので打ち切る
			break
		}
		if err != nil {
			return nil, err
		}
		line, _ := r.FieldPos(0)
		if len(rows) >= maxImportRows {
			errs.add(line, "データ行は %d 行までです", maxImportRows)
			break
		}
		row := entryRow{
			line:        line,
			month:       get(rec, "target_month"),
			account:     get(rec, "account_code"),
			dept:        get(rec, "department_code"),
			box:         get(rec, "box_code"),
			description: get(rec, "description"),
		}
		if !isYearMonth(row.month) {
			errs.add(line, "対象年月 %q は YYYY-MM 形式で入力してください", row.month)
		}
		if row.account == "" {
			errs.add(line, "会計科目コードが空です")
		}
		if row.dept != "" && !departmentPattern.MatchString(row.dept) {
			errs.add(line, "部門コード %q は空白・カンマ・引用符を含まない50文字以内にしてください", row.dept)
		}
		if utf8.RuneCountInString(row.box) > 100 {
			errs.add(line, "箱の ID は100文字以内にしてください")
		}
		if utf8.RuneCountInString(row.description) > 200 {
			errs.add(line, "摘要は200文字以内にしてください")
		}
		raw := get(rec, "amount")
		amount, ok := new(big.Int).SetString(raw, 10)
		switch {
		case !ok:
			errs.add(line, "金額 %q は円単位の整数で入力してください", raw)
		case new(big.Int).Abs(amount).Cmp(maxAmount) > 0:
			errs.add(line, "金額が大きすぎます")
		default:
			row.amount = amount
		}
		rows = append(rows, row)
	}
	if err := errs.err(); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, httpx.BadRequest("CSV にデータ行がありません")
	}
	return rows, nil
}

// entry は保存する明細。
type entry struct {
	row        entryRow
	accountID  int64
	subjectID  int64
	activityID *int64
	by         string
}

// resolveEntries は会計科目を確かめ、各行を施策に割り当てる。対象外の会計科目の行は除く（件数を返す）。
// 未登録の会計科目は、コードごとにまとめてエラーにする。
func resolveEntries(rows []entryRow, accounts map[string]glAccount, al *allocator) ([]entry, int, error) {
	type unknown struct{ first, count int }
	missing := map[string]*unknown{}
	var order []string
	var out []entry
	excluded := 0
	for _, row := range rows {
		a, ok := accounts[row.account]
		if !ok {
			if missing[row.account] == nil {
				missing[row.account] = &unknown{first: row.line}
				order = append(order, row.account)
			}
			missing[row.account].count++
			continue
		}
		if a.IsExcluded {
			excluded++
			continue
		}
		activityID, by := al.allocate(a.ID, row.dept, row.box)
		out = append(out, entry{row: row, accountID: a.ID, subjectID: *a.SubjectID, activityID: activityID, by: by})
	}
	if len(order) > 0 {
		errs := &rowErrors{}
		for _, code := range order {
			m := missing[code]
			errs.add(m.first, "会計科目 %q は登録されていません（%d 行）。会計科目のマスタに登録してから取り込んでください", code, m.count)
		}
		return nil, 0, errs.err()
	}
	return out, excluded, nil
}

// importResult は取込の結果。
type importResult struct {
	DryRun     bool           `json:"dry_run"`
	Months     []string       `json:"months"`
	Rows       int            `json:"rows"`     // CSV のデータ行数
	Excluded   int            `json:"excluded"` // 対象外の会計科目の行数
	Allocation map[string]int `json:"allocation"`
	factCounts
	Totals []monthTotals `json:"totals"`
	// LockedDrift は、取り込むとロック済みのシナリオの実績と食い違う月（docs/plan.md「2.14」）
	LockedDrift []scenarioDrift `json:"locked_drift"`
}

// importActuals は POST /api/actuals/import。FP&A のみ。
//
// multipart/form-data で file（CSV）と reason（変更理由、必須）を送る。
// CSV に含まれる月の明細を置き換え、各行を施策に割り当て、施策 × 科目 × 月の合計（actual_facts）を差分で更新する。
// ロック済みのシナリオは、ロック時に保存した実績を使うため影響を受けない。
// クエリ dry_run=true を付けると、検証と集計だけを行い保存しない。
func (h *Handler) importActuals(w http.ResponseWriter, r *http.Request) error {
	u, err := currentUser(r)
	if err != nil {
		return err
	}
	dryRun := r.URL.Query().Get("dry_run") == "true"

	r.Body = http.MaxBytesReader(w, r.Body, maxImportBytes)
	if err := r.ParseMultipartForm(maxImportBytes); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return &httpx.Error{Status: http.StatusRequestEntityTooLarge, Code: "too_large", Message: "ファイルは 50MB 以下にしてください"}
		}
		return httpx.BadRequest("multipart/form-data で file と reason を送ってください")
	}
	defer r.MultipartForm.RemoveAll()

	reason := strings.TrimSpace(r.FormValue("reason"))
	if reason == "" && !dryRun {
		return httpx.Validation(map[string]string{"reason": "変更理由を入力してください（例: 2026-09 実績取込）"})
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		return httpx.Validation(map[string]string{"file": "CSV ファイルを選択してください"})
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		return err
	}
	rows, err := parseEntriesCSV(data)
	if err != nil {
		return err
	}

	ctx := r.Context()
	result := importResult{DryRun: dryRun, Rows: len(rows), Allocation: map[string]int{}, LockedDrift: []scenarioDrift{}}
	for _, k := range allocationKinds {
		result.Allocation[k] = 0
	}
	err = audit.InTx(ctx, h.db, u.ID, nil, reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		if err := checkClosedRows(ctx, tx, rows); err != nil {
			return err
		}
		byID, err := loadAccounts(ctx, tx)
		if err != nil {
			return err
		}
		accounts := map[string]glAccount{}
		for _, a := range byID {
			accounts[a.Code] = a
		}
		al, err := loadAllocator(ctx, tx)
		if err != nil {
			return err
		}
		entries, excluded, err := resolveEntries(rows, accounts, al)
		if err != nil {
			return err
		}
		result.Excluded = excluded
		for _, e := range entries {
			result.Allocation[e.by]++
		}

		// 対象の月は CSV に含まれる月（対象外の行だけの月も、その月の明細を置き換える）
		monthSet := map[string]bool{}
		for _, row := range rows {
			monthSet[row.month] = true
		}
		for m := range monthSet {
			result.Months = append(result.Months, m)
		}
		sort.Strings(result.Months)

		if _, err := tx.ExecContext(ctx,
			"DELETE FROM actual_entries WHERE target_month IN ("+placeholders(len(result.Months))+")", monthArgs(result.Months)...); err != nil {
			return err
		}
		if err := insertEntries(ctx, tx, rec.ChangeSetID(), entries); err != nil {
			return err
		}
		if err := syncFacts(ctx, tx, rec, result.Months, &result.factCounts); err != nil {
			return err
		}
		if result.Totals, err = monthlyTotals(ctx, tx, result.Months); err != nil {
			return err
		}
		if result.LockedDrift, err = loadDrift(ctx, tx, 0, result.Months); err != nil {
			return err
		}
		if dryRun {
			return errDryRun // 集計まで行ってロールバックする
		}
		return nil
	})
	if err != nil && !errors.Is(err, errDryRun) {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, result)
	return nil
}

// checkClosedRows は、締めた年度の月の行があれば、月ごとにまとめてエラーにする。
func checkClosedRows(ctx context.Context, tx *sql.Tx, rows []entryRow) error {
	closed, err := closedYears(ctx, tx)
	if err != nil || len(closed) == 0 {
		return err
	}
	type found struct{ first, count int }
	byMonth := map[string]*found{}
	var order []string
	for _, row := range rows {
		if !closed[fiscalYearOf(row.month)] {
			continue
		}
		if byMonth[row.month] == nil {
			byMonth[row.month] = &found{first: row.line}
			order = append(order, row.month)
		}
		byMonth[row.month].count++
	}
	errs := &rowErrors{}
	for _, m := range order {
		errs.add(byMonth[m].first, "%s は締めた年度（%d年度）の月なので取り込めません（%d 行）。シナリオ管理で締めを解除してから取り込んでください", m, fiscalYearOf(m), byMonth[m].count)
	}
	return errs.err()
}

// insertEntries は明細をまとめて登録する。
func insertEntries(ctx context.Context, tx *sql.Tx, changeSetID int64, entries []entry) error {
	const cols = 11
	for start := 0; start < len(entries); start += insertBatch {
		end := min(start+insertBatch, len(entries))
		var sb strings.Builder
		sb.WriteString(`INSERT INTO actual_entries (target_month, line_no, gl_account_id, subject_id, department_code, box_code,
			amount, description, activity_id, allocated_by, change_set_id) VALUES `)
		args := make([]any, 0, (end-start)*cols)
		for i, e := range entries[start:end] {
			if i > 0 {
				sb.WriteString(",")
			}
			sb.WriteString("(" + placeholders(cols) + ")")
			args = append(args, e.row.month+"-01", e.row.line, e.accountID, e.subjectID, nullIfEmpty(e.row.dept), nullIfEmpty(e.row.box),
				e.row.amount.String(), nullIfEmpty(e.row.description), activityArg(e.activityID), e.by, changeSetID)
		}
		if _, err := tx.ExecContext(ctx, sb.String(), args...); err != nil {
			return err
		}
	}
	return nil
}

func nullIfEmpty(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

func activityArg(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

// actualMonths は GET /api/actuals/months?fiscal_year=。年度のうち、実績を取り込み済みの月を返す。
// シナリオを作成するときの決算確定月の既定値（取込済みの最終月）に使う。
func (h *Handler) actualMonths(w http.ResponseWriter, r *http.Request) error {
	fy, err := strconv.Atoi(r.URL.Query().Get("fiscal_year"))
	if err != nil {
		return httpx.BadRequest("fiscal_year は数値で指定してください")
	}
	months := calc.FiscalMonths(fy)
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT DISTINCT DATE_FORMAT(target_month, '%Y-%m') FROM actual_facts
		WHERE target_month BETWEEN ? AND ? ORDER BY 1`, months[0]+"-01", months[len(months)-1]+"-01")
	if err != nil {
		return err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			return err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"fiscal_year": fy, "months": out})
	return nil
}
