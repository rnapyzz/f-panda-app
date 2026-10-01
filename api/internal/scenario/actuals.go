package scenario

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

const (
	maxImportBytes = 20 << 20 // 20MB（nginx の client_max_body_size と合わせる）
	maxImportRows  = 50_000
	maxRowErrors   = 100
)

var csvColumns = []string{"target_month", "activity_code", "subject_code", "amount"}

// csvRow は CSV の1データ行。
type csvRow struct {
	line         int
	month        string
	activityCode string
	subjectCode  string
	amount       *big.Int
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

// parseActualsCSV は実績 CSV の形式（ヘッダー・列数・年月・金額）を検証して行を返す。
// コードの存在確認や年度の範囲確認は resolveActuals で行う。
func parseActualsCSV(data []byte) ([]csvRow, error) {
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
	index := map[string]int{}
	for i, h := range header {
		index[strings.ToLower(strings.TrimSpace(h))] = i
	}
	for _, c := range csvColumns {
		if _, ok := index[c]; !ok || len(header) != len(csvColumns) {
			return nil, httpx.BadRequest("CSV のヘッダー行は " + strings.Join(csvColumns, ",") + " にしてください")
		}
	}
	r.FieldsPerRecord = len(csvColumns)

	var rows []csvRow
	errs := &rowErrors{}
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		var parseErr *csv.ParseError
		if errors.As(err, &parseErr) {
			if errors.Is(parseErr.Err, csv.ErrFieldCount) {
				errs.add(parseErr.Line, "列の数が %d ではありません", len(csvColumns))
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

		row := csvRow{
			line:         line,
			month:        strings.TrimSpace(rec[index["target_month"]]),
			activityCode: strings.TrimSpace(rec[index["activity_code"]]),
			subjectCode:  strings.TrimSpace(rec[index["subject_code"]]),
		}
		if !isYearMonth(row.month) {
			errs.add(line, "対象年月 %q は YYYY-MM 形式で入力してください", row.month)
		}
		if row.activityCode == "" {
			errs.add(line, "施策コードが空です")
		}
		if row.subjectCode == "" {
			errs.add(line, "科目コードが空です")
		}
		amount, ok := new(big.Int).SetString(strings.TrimSpace(rec[index["amount"]]), 10)
		switch {
		case !ok:
			errs.add(line, "金額 %q は円単位の整数で入力してください", rec[index["amount"]])
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

func isYearMonth(s string) bool {
	if len(s) != 7 || s[4] != '-' {
		return false
	}
	for i, c := range s {
		if i != 4 && (c < '0' || c > '9') {
			return false
		}
	}
	m := (s[5]-'0')*10 + (s[6] - '0')
	return m >= 1 && m <= 12
}

type actualKey struct {
	activityID, subjectID int64
	month                 string
}

// resolveActuals はコードを ID に変換し、同じキーの金額を合算する。
func resolveActuals(rows []csvRow, activities, subjects map[string]int64) (map[actualKey]*big.Int, error) {
	errs := &rowErrors{}
	out := map[actualKey]*big.Int{}
	for _, row := range rows {
		ok := true
		activityID, found := activities[row.activityCode]
		if !found {
			errs.add(row.line, "施策コード %q は登録されていません（施策コードまたは外部コードを指定してください）", row.activityCode)
			ok = false
		}
		subjectID, found := subjects[row.subjectCode]
		if !found {
			errs.add(row.line, "科目コード %q は登録されていません", row.subjectCode)
			ok = false
		}
		if !ok {
			continue
		}
		k := actualKey{activityID, subjectID, row.month}
		if out[k] == nil {
			out[k] = new(big.Int)
		}
		out[k].Add(out[k], row.amount)
	}
	if err := errs.err(); err != nil {
		return nil, err
	}
	for k, v := range out {
		if new(big.Int).Abs(v).Cmp(maxAmount) > 0 {
			return nil, httpx.Validation(map[string]string{"file": fmt.Sprintf("%s の合算後の金額が大きすぎます", k.month)})
		}
	}
	return out, nil
}

// importResult は取込結果。
type importResult struct {
	DryRun    bool          `json:"dry_run"`
	Months    []string      `json:"months"`
	Rows      int           `json:"rows"`  // CSV のデータ行数
	Facts     int           `json:"facts"` // 合算後の件数
	Inserted  int           `json:"inserted"`
	Updated   int           `json:"updated"`
	Deleted   int           `json:"deleted"`
	Unchanged int           `json:"unchanged"`
	Totals    []monthTotals `json:"totals"`
}

// monthTotals は月ごとの合計（会計システムとの突合用）。
type monthTotals struct {
	Month   string `json:"month"`
	Revenue string `json:"revenue"`
	Expense string `json:"expense"`
}

var errDryRun = errors.New("dry run")

// actualFact は実績データ（actual_facts）の1行。監査ログにも使う。
type actualFact struct {
	ID          int64  `json:"id"`
	ActivityID  int64  `json:"activity_id"`
	SubjectID   int64  `json:"subject_id"`
	TargetMonth string `json:"target_month"`
	Amount      string `json:"amount"`
}

// importActuals は POST /api/actuals/import。FP&A のみ。
//
// multipart/form-data で file（CSV）と reason（変更理由、必須）を送る。
// 実績データ（actual_facts）の、CSV に含まれる月の実績を CSV の内容で置き換える（差分だけを更新し、監査ログに残す）。
// ロックされていないシナリオは、決算確定月以前の月に実績データを参照するので、取込の内容がそのまま反映される。
// ロック済みのシナリオは、ロック時に保存した実績を使うため影響を受けない。
// クエリ dry_run=true を付けると、検証と件数の集計だけを行い保存しない。
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
			return &httpx.Error{Status: http.StatusRequestEntityTooLarge, Code: "too_large", Message: "ファイルは 20MB 以下にしてください"}
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
	rows, err := parseActualsCSV(data)
	if err != nil {
		return err
	}

	ctx := r.Context()
	result := importResult{DryRun: dryRun, Rows: len(rows)}
	err = audit.InTx(ctx, h.db, u.ID, nil, reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		// activity_code 列には、施策コードと外部コード（案件番号など）のどちらも使える
		activities, err := codeMap(ctx, tx, "SELECT code, id FROM activities")
		if err != nil {
			return err
		}
		externals, err := codeMap(ctx, tx, "SELECT code, activity_id FROM activity_external_codes")
		if err != nil {
			return err
		}
		for code, id := range externals {
			activities[code] = id
		}
		subjects, err := codeMap(ctx, tx, "SELECT code, id FROM subjects")
		if err != nil {
			return err
		}
		want, err := resolveActuals(rows, activities, subjects)
		if err != nil {
			return err
		}
		result.Facts = len(want)

		monthSet := map[string]bool{}
		for k := range want {
			monthSet[k.month] = true
		}
		for m := range monthSet {
			result.Months = append(result.Months, m)
		}
		sort.Strings(result.Months)

		existing, err := loadMonthActuals(ctx, tx, result.Months)
		if err != nil {
			return err
		}
		if err := applyActuals(ctx, tx, rec, existing, want, &result); err != nil {
			return err
		}
		if result.Totals, err = monthlyTotals(ctx, tx, result.Months); err != nil {
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

// applyActuals は既存の実績を CSV の内容に合わせて insert / update / delete する。
func applyActuals(ctx context.Context, tx *sql.Tx, rec *audit.Recorder,
	existing map[actualKey]actualFact, want map[actualKey]*big.Int, result *importResult) error {
	keys := make([]actualKey, 0, len(existing)+len(want))
	for k := range existing {
		keys = append(keys, k)
	}
	for k := range want {
		if _, ok := existing[k]; !ok {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.month != b.month {
			return a.month < b.month
		}
		if a.activityID != b.activityID {
			return a.activityID < b.activityID
		}
		return a.subjectID < b.subjectID
	})

	for _, k := range keys {
		before, had := existing[k]
		amount, has := want[k]
		switch {
		case had && !has:
			if _, err := tx.ExecContext(ctx, "DELETE FROM actual_facts WHERE id = ?", before.ID); err != nil {
				return err
			}
			if err := rec.Delete(ctx, "actual_facts", before.ID, before); err != nil {
				return err
			}
			result.Deleted++
		case !had && has:
			after := actualFact{ActivityID: k.activityID, SubjectID: k.subjectID, TargetMonth: k.month, Amount: amount.String()}
			res, err := tx.ExecContext(ctx,
				"INSERT INTO actual_facts (activity_id, subject_id, target_month, amount) VALUES (?, ?, ?, ?)",
				after.ActivityID, after.SubjectID, after.TargetMonth+"-01", after.Amount)
			if err != nil {
				return err
			}
			after.ID, _ = res.LastInsertId()
			if err := rec.Insert(ctx, "actual_facts", after.ID, after); err != nil {
				return err
			}
			result.Inserted++
		default:
			after := before
			after.Amount = amount.String()
			if after == before {
				result.Unchanged++
				continue
			}
			if _, err := tx.ExecContext(ctx, "UPDATE actual_facts SET amount = ? WHERE id = ?", after.Amount, after.ID); err != nil {
				return err
			}
			if err := rec.Update(ctx, "actual_facts", before.ID, before, after); err != nil {
				return err
			}
			result.Updated++
		}
	}
	return nil
}

// codeMap は、コードと ID を返すクエリから code → id の対応表を作る。
func codeMap(ctx context.Context, tx *sql.Tx, query string) (map[string]int64, error) {
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var code string
		var id int64
		if err := rows.Scan(&code, &id); err != nil {
			return nil, err
		}
		out[code] = id
	}
	return out, rows.Err()
}

// loadMonthActuals は指定月の実績を行ロック付きで読み込む。
func loadMonthActuals(ctx context.Context, tx *sql.Tx, months []string) (map[actualKey]actualFact, error) {
	out := map[actualKey]actualFact{}
	if len(months) == 0 {
		return out, nil
	}
	args := []any{}
	for _, m := range months {
		args = append(args, m+"-01")
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT id, activity_id, subject_id, DATE_FORMAT(target_month, '%Y-%m'), amount
		FROM actual_facts
		WHERE target_month IN (?`+strings.Repeat(", ?", len(months)-1)+`) FOR UPDATE`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var f actualFact
		if err := rows.Scan(&f.ID, &f.ActivityID, &f.SubjectID, &f.TargetMonth, &f.Amount); err != nil {
			return nil, err
		}
		out[actualKey{f.ActivityID, f.SubjectID, f.TargetMonth}] = f
	}
	return out, rows.Err()
}

// monthlyTotals は月ごとの実績の収益・費用の合計を返す。
func monthlyTotals(ctx context.Context, tx *sql.Tx, months []string) ([]monthTotals, error) {
	out := []monthTotals{}
	for _, m := range months {
		t := monthTotals{Month: m}
		err := tx.QueryRowContext(ctx, `
			SELECT CAST(COALESCE(SUM(CASE WHEN s.category = 'revenue' THEN f.amount END), 0) AS CHAR),
			       CAST(COALESCE(SUM(CASE WHEN s.category = 'expense' THEN f.amount END), 0) AS CHAR)
			FROM actual_facts f JOIN subjects s ON s.id = f.subject_id
			WHERE f.target_month = ?`, m+"-01",
		).Scan(&t.Revenue, &t.Expense)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
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
