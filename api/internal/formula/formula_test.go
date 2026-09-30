package formula

import (
	"math/big"
	"reflect"
	"strings"
	"testing"
)

func rat(s string) *big.Rat {
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		panic(s)
	}
	return r
}

func TestEval(t *testing.T) {
	vars := map[string]*big.Rat{
		"unit_price":  rat("1200"),
		"volume":      rat("3"),
		"probability": rat("0.7"),
		"a":           rat("0.1"),
		"b":           rat("0.2"),
	}
	tests := []struct {
		src  string
		want string
	}{
		{"unit_price * volume * probability", "2520"},
		{"1 + 2 * 3", "7"},
		{"(1 + 2) * 3", "9"},
		{"10 - 4 - 3", "3"},  // 左結合
		{"100 / 4 / 5", "5"}, // 左結合
		{"-3 * -2", "6"},     // 単項マイナス
		{"-(1 + 2)", "-3"},
		{"a + b", "3/10"},    // 浮動小数点なら 0.30000000000000004
		{"10 / 3 * 3", "10"}, // 有理数で誤差なし
		{" .5 * 4 ", "2"},
	}
	for _, tt := range tests {
		e, err := Parse(tt.src)
		if err != nil {
			t.Errorf("Parse(%q): %v", tt.src, err)
			continue
		}
		got, err := e.Eval(vars)
		if err != nil {
			t.Errorf("Eval(%q): %v", tt.src, err)
			continue
		}
		if got.Cmp(rat(tt.want)) != 0 {
			t.Errorf("Eval(%q) = %s, want %s", tt.src, got.RatString(), tt.want)
		}
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct{ src, wantSubstr string }{
		{"", "空"},
		{"   ", "空"},
		{"1 +", "途中で終わって"},
		{"(1 + 2", ")"},
		{"1 + 2)", "不要な"},
		{"a b", "不要な"},
		{"1 ^ 2", "不要な"},
		{"売上 * 2", "使えません"},
		{"1.2.3", "正しくありません"},
		{"* 2", "使えません"},
	}
	for _, tt := range tests {
		_, err := Parse(tt.src)
		if err == nil {
			t.Errorf("Parse(%q): エラーにならなかった", tt.src)
			continue
		}
		if !strings.Contains(err.Error(), tt.wantSubstr) {
			t.Errorf("Parse(%q) err = %q, want %q を含む", tt.src, err, tt.wantSubstr)
		}
	}
}

func TestIdents(t *testing.T) {
	e, err := Parse("unit_price * volume + unit_price * 0.1 - fixed_cost")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := e.Idents(), []string{"fixed_cost", "unit_price", "volume"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Idents = %v, want %v", got, want)
	}
}

func TestEvalErrors(t *testing.T) {
	e, _ := Parse("a / b")
	if _, err := e.Eval(map[string]*big.Rat{"a": rat("1"), "b": rat("0")}); err != ErrDivisionByZero {
		t.Errorf("0除算: err = %v", err)
	}
	if _, err := e.Eval(map[string]*big.Rat{"a": rat("1")}); err == nil || !strings.Contains(err.Error(), "b") {
		t.Errorf("値なし: err = %v", err)
	}
}

func TestRoundHalfUp(t *testing.T) {
	tests := map[string]string{
		"0": "0", "100.4": "100", "100.5": "101", "100.6": "101",
		"-100.4": "-100", "-100.5": "-101", "2/3": "1", "1/3": "0",
	}
	for in, want := range tests {
		if got := RoundHalfUp(rat(in)).String(); got != want {
			t.Errorf("RoundHalfUp(%s) = %s, want %s", in, got, want)
		}
	}
}
