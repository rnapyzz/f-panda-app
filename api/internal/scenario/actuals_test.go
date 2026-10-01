package scenario

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
)

func TestParseActualsCSV(t *testing.T) {
	data := "\xef\xbb\xbfTarget_Month, activity_code,subject_code,amount\r\n" +
		"2026-09,ACT-0001,4110,1200000\r\n" +
		"\r\n" + // 空行は無視される
		"2026-09,ACT-0001,8110,-350000\r\n"
	rows, err := parseActualsCSV([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if r := rows[1]; r.line != 4 || r.month != "2026-09" || r.subjectCode != "8110" || r.amount.String() != "-350000" {
		t.Errorf("rows[1] = %+v", r)
	}
}

func TestParseActualsCSVColumnOrder(t *testing.T) {
	rows, err := parseActualsCSV([]byte("amount,subject_code,activity_code,target_month\n100,4110,A,2026-10\n"))
	if err != nil {
		t.Fatal(err)
	}
	if r := rows[0]; r.month != "2026-10" || r.activityCode != "A" || r.amount.String() != "100" {
		t.Errorf("列の順番を入れ替えた CSV = %+v", r)
	}
}

func TestParseActualsCSVErrors(t *testing.T) {
	tests := []struct {
		name string
		data string
		want []string // エラーメッセージに含まれる文字列（行ごと）
	}{
		{"空", "", []string{"空です"}},
		{"ヘッダー違い", "month,activity,subject,amount\n", []string{"ヘッダー行"}},
		{"列が多いヘッダー", "target_month,activity_code,subject_code,amount,memo\n", []string{"ヘッダー行"}},
		{"データなし", "target_month,activity_code,subject_code,amount\n", []string{"データ行がありません"}},
		{"Shift_JIS", "target_month,activity_code,subject_code,amount\n2026-09,\x83\x65,4110,1\n", []string{"UTF-8"}},
		{
			"行ごとのエラー",
			"target_month,activity_code,subject_code,amount\n" +
				"2026/09,A,4110,1\n" + // 2行目: 年月の形式
				"2026-13,A,4110,1\n" + // 3行目: 月が不正
				"2026-09,,4110,1.5\n" + // 4行目: 施策コードなし・金額が小数
				"2026-09,A,4110\n" + // 5行目: 列数
				"2026-09,A,4110,1000000000000000000\n", // 6行目: 桁あふれ
			[]string{"2:YYYY-MM", "3:YYYY-MM", "4:施策コードが空", "4:円単位の整数", "5:列の数", "6:大きすぎ"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseActualsCSV([]byte(tt.data))
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

func TestResolveActuals(t *testing.T) {
	rows, err := parseActualsCSV([]byte("target_month,activity_code,subject_code,amount\n" +
		"2026-09,A,4110,100\n" +
		"2026-09,A,4110,250\n" + // 同じキーは合算
		"2026-09,B,4110,1\n" +
		"2027-04,A,4110,1\n" + // 年度をまたいでも取り込める
		"2026-10,X,9999,1\n")) // 未登録のコード
	if err != nil {
		t.Fatal(err)
	}
	activities := map[string]int64{"A": 1, "B": 2}
	subjects := map[string]int64{"4110": 10}

	_, err = resolveActuals(rows, activities, subjects)
	var apiErr *httpx.Error
	if !errors.As(err, &apiErr) || len(apiErr.Rows) != 2 {
		t.Fatalf("err = %v, rows = %+v, want 2 件（未登録の施策・未登録の科目）", err, apiErr)
	}

	got, err := resolveActuals(rows[:4], activities, subjects)
	if err != nil {
		t.Fatal(err)
	}
	if v := got[actualKey{1, 10, "2026-09"}]; v == nil || v.String() != "350" {
		t.Errorf("合算 = %v, want 350", v)
	}
	if len(got) != 3 {
		t.Errorf("件数 = %d, want 3", len(got))
	}
}
