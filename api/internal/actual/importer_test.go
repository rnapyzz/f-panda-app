package actual

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
)

func TestParseEntriesCSV(t *testing.T) {
	data := "\xef\xbb\xbfTarget_Month, account_code,department_code,box_code,amount,description\r\n" +
		"2026-09,4110,D100,ACT-0001,1200000,A社 受託\r\n" +
		"\r\n" + // 空行は無視される
		"2026-09,8110,,,-350000,\r\n"
	rows, err := parseEntriesCSV([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if r := rows[0]; r.dept != "D100" || r.box != "ACT-0001" || r.description != "A社 受託" {
		t.Errorf("rows[0] = %+v", r)
	}
	if r := rows[1]; r.line != 4 || r.account != "8110" || r.dept != "" || r.amount.String() != "-350000" {
		t.Errorf("rows[1] = %+v", r)
	}
}

func TestParseEntriesCSVOptionalColumns(t *testing.T) {
	// 省略できる列はなくてもよく、列の順番は自由
	rows, err := parseEntriesCSV([]byte("amount,account_code,target_month\n100,4110,2026-10\n"))
	if err != nil {
		t.Fatal(err)
	}
	if r := rows[0]; r.month != "2026-10" || r.account != "4110" || r.box != "" || r.amount.String() != "100" {
		t.Errorf("rows[0] = %+v", r)
	}
}

func TestParseEntriesCSVErrors(t *testing.T) {
	tests := []struct {
		name string
		data string
		want []string // エラーメッセージに含まれる文字列（行ごと）
	}{
		{"空", "", []string{"空です"}},
		{"必須の列がない", "target_month,account_code\n", []string{"ヘッダー行"}},
		{"知らない列", "target_month,account_code,amount,memo\n", []string{"ヘッダー行"}},
		{"列の重複", "target_month,account_code,amount,amount\n", []string{"ヘッダー行"}},
		{"データなし", "target_month,account_code,amount\n", []string{"データ行がありません"}},
		{"Shift_JIS", "target_month,account_code,amount\n2026-09,\x83\x65,1\n", []string{"UTF-8"}},
		{
			"行ごとのエラー",
			"target_month,account_code,department_code,amount\n" +
				"2026/09,4110,,1\n" + // 2行目: 年月の形式
				"2026-09,,,1.5\n" + // 3行目: 会計科目なし・金額が小数
				"2026-09,4110,D 1,1\n" + // 4行目: 部門コードに空白
				"2026-09,4110\n" + // 5行目: 列数
				"2026-09,4110,,1000000000000000000\n", // 6行目: 桁あふれ
			[]string{"2:YYYY-MM", "3:会計科目コードが空", "3:円単位の整数", "4:部門コード", "5:列の数", "6:大きすぎ"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseEntriesCSV([]byte(tt.data))
			var apiErr *httpx.Error
			if !errors.As(err, &apiErr) {
				t.Fatalf("err = %v, want *httpx.Error", err)
			}
			if len(apiErr.Rows) == 0 {
				if !strings.Contains(apiErr.Message, tt.want[0]) {
					t.Errorf("message = %q, want %q を含む", apiErr.Message, tt.want[0])
				}
				return
			}
			var got []string
			for _, r := range apiErr.Rows {
				got = append(got, strconv.Itoa(r.Line)+":"+r.Message)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("rows = %v, want %d 件", got, len(tt.want))
			}
			for i, w := range tt.want {
				line, substr, _ := strings.Cut(w, ":")
				if !strings.HasPrefix(got[i], line+":") || !strings.Contains(got[i], substr) {
					t.Errorf("rows[%d] = %q, want 行 %s・%q を含む", i, got[i], line, substr)
				}
			}
		})
	}
}

func TestAllocate(t *testing.T) {
	a := &allocator{
		activities: map[string]int64{"ACT-1": 1},
		externals:  map[string]int64{"P-1": 2},
		rules: map[ruleKey]int64{
			{accountID: 10, dept: ""}:   3, // 会計科目 10 は全部門で施策 3
			{accountID: 10, dept: "D2"}: 4, // ただし D2 は施策 4
		},
	}
	tests := []struct {
		account    int64
		dept, box  string
		wantID     int64 // 0 は未割当
		wantReason string
	}{
		{10, "D2", "ACT-1", 1, byActivityCode}, // 箱の ID がルールより優先
		{10, "D2", "P-1", 2, byExternalCode},
		{10, "D2", "UNKNOWN", 4, byRule}, // 登録されていない箱の ID はルールへ
		{10, "D1", "", 3, byRule},        // 部門のルールがなければ全部門のルール
		{10, "", "", 3, byRule},
		{20, "D2", "", 0, byUnallocated},
	}
	for _, tt := range tests {
		id, by := a.allocate(tt.account, tt.dept, tt.box)
		got := int64(0)
		if id != nil {
			got = *id
		}
		if got != tt.wantID || by != tt.wantReason {
			t.Errorf("allocate(%d, %q, %q) = %d, %s; want %d, %s", tt.account, tt.dept, tt.box, got, by, tt.wantID, tt.wantReason)
		}
	}
}
