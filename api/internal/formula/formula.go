// Package formula はドライバーから金額を算出する計算式を解析・評価する。
//
// 使える要素は、数値リテラル（例: 1000, 0.25）、識別子（ドライバーの code）、
// 四則演算（+ - * /）、単項マイナス、括弧のみ。
// 計算は math/big.Rat で行い、浮動小数点の誤差を出さない。
//
//	unit_price * volume
//	(headcount * monthly_cost) + fixed_cost
package formula

import (
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"
)

// Expr は解析済みの計算式。
type Expr struct {
	root node
}

type node interface {
	eval(vars map[string]*big.Rat) (*big.Rat, error)
	idents(set map[string]bool)
}

type numNode struct{ v *big.Rat }
type identNode struct{ name string }
type unaryNode struct{ x node }
type binNode struct {
	op   byte
	l, r node
}

func (n numNode) eval(map[string]*big.Rat) (*big.Rat, error) { return new(big.Rat).Set(n.v), nil }
func (n numNode) idents(map[string]bool)                     {}

func (n identNode) eval(vars map[string]*big.Rat) (*big.Rat, error) {
	v, ok := vars[n.name]
	if !ok || v == nil {
		return nil, fmt.Errorf("%s の値がありません", n.name)
	}
	return new(big.Rat).Set(v), nil
}
func (n identNode) idents(set map[string]bool) { set[n.name] = true }

func (n unaryNode) eval(vars map[string]*big.Rat) (*big.Rat, error) {
	x, err := n.x.eval(vars)
	if err != nil {
		return nil, err
	}
	return x.Neg(x), nil
}
func (n unaryNode) idents(set map[string]bool) { n.x.idents(set) }

func (n binNode) eval(vars map[string]*big.Rat) (*big.Rat, error) {
	l, err := n.l.eval(vars)
	if err != nil {
		return nil, err
	}
	r, err := n.r.eval(vars)
	if err != nil {
		return nil, err
	}
	switch n.op {
	case '+':
		return l.Add(l, r), nil
	case '-':
		return l.Sub(l, r), nil
	case '*':
		return l.Mul(l, r), nil
	default: // '/'
		if r.Sign() == 0 {
			return nil, ErrDivisionByZero
		}
		return l.Quo(l, r), nil
	}
}
func (n binNode) idents(set map[string]bool) { n.l.idents(set); n.r.idents(set) }

// ErrDivisionByZero は0で割ったときに返る。
var ErrDivisionByZero = errors.New("0 で割ることはできません")

// Parse は計算式を解析する。構文エラーは位置を含むメッセージで返す。
func Parse(src string) (*Expr, error) {
	p := &parser{src: src}
	p.next()
	if p.tok.kind == tokEOF {
		return nil, errors.New("計算式が空です")
	}
	root, err := p.expr()
	if err != nil {
		return nil, err
	}
	if p.tok.kind != tokEOF {
		return nil, p.errorf("不要な %q があります", p.tok.text)
	}
	return &Expr{root: root}, nil
}

// Idents は式に含まれる識別子を名前順で返す。
func (e *Expr) Idents() []string {
	set := map[string]bool{}
	e.root.idents(set)
	names := make([]string, 0, len(set))
	for n := range set {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Eval は識別子に値を割り当てて式を評価する。
func (e *Expr) Eval(vars map[string]*big.Rat) (*big.Rat, error) {
	return e.root.eval(vars)
}

// RoundHalfUp は x を小数点以下 0 桁に四捨五入（0 から遠い方へ）した整数を返す。
func RoundHalfUp(x *big.Rat) *big.Int {
	num := new(big.Int).Abs(x.Num())
	den := x.Denom()
	// (|num| * 2 + den) / (den * 2) で四捨五入
	q := new(big.Int).Mul(num, big.NewInt(2))
	q.Add(q, den)
	q.Quo(q, new(big.Int).Mul(den, big.NewInt(2)))
	if x.Sign() < 0 {
		q.Neg(q)
	}
	return q
}

// --- 字句解析・構文解析 ---

type tokKind int

const (
	tokEOF tokKind = iota
	tokNum
	tokIdent
	tokOp
)

type token struct {
	kind tokKind
	text string
	pos  int
}

type parser struct {
	src string
	pos int
	tok token
	err error
}

func (p *parser) errorf(format string, args ...any) error {
	return fmt.Errorf("%d文字目: %s", p.tok.pos+1, fmt.Sprintf(format, args...))
}

func (p *parser) next() {
	for p.pos < len(p.src) && strings.ContainsRune(" \t\r\n", rune(p.src[p.pos])) {
		p.pos++
	}
	start := p.pos
	if p.pos >= len(p.src) {
		p.tok = token{kind: tokEOF, pos: start}
		return
	}
	c := p.src[p.pos]
	switch {
	case isDigit(c) || (c == '.' && p.pos+1 < len(p.src) && isDigit(p.src[p.pos+1])):
		for p.pos < len(p.src) && (isDigit(p.src[p.pos]) || p.src[p.pos] == '.') {
			p.pos++
		}
		p.tok = token{kind: tokNum, text: p.src[start:p.pos], pos: start}
	case isIdentStart(c):
		for p.pos < len(p.src) && isIdentPart(p.src[p.pos]) {
			p.pos++
		}
		p.tok = token{kind: tokIdent, text: p.src[start:p.pos], pos: start}
	case strings.IndexByte("+-*/()", c) >= 0:
		p.pos++
		p.tok = token{kind: tokOp, text: string(c), pos: start}
	default:
		// 不正な文字（マルチバイト文字を含む）はそのまま1文字として扱いエラーにする
		r := []rune(p.src[p.pos:])[0]
		p.pos += len(string(r))
		p.tok = token{kind: tokOp, text: string(r), pos: start}
	}
}

// expr := term (('+' | '-') term)*
func (p *parser) expr() (node, error) {
	l, err := p.term()
	if err != nil {
		return nil, err
	}
	for p.tok.kind == tokOp && (p.tok.text == "+" || p.tok.text == "-") {
		op := p.tok.text[0]
		p.next()
		r, err := p.term()
		if err != nil {
			return nil, err
		}
		l = binNode{op: op, l: l, r: r}
	}
	return l, nil
}

// term := unary (('*' | '/') unary)*
func (p *parser) term() (node, error) {
	l, err := p.unary()
	if err != nil {
		return nil, err
	}
	for p.tok.kind == tokOp && (p.tok.text == "*" || p.tok.text == "/") {
		op := p.tok.text[0]
		p.next()
		r, err := p.unary()
		if err != nil {
			return nil, err
		}
		l = binNode{op: op, l: l, r: r}
	}
	return l, nil
}

// unary := '-' unary | primary
func (p *parser) unary() (node, error) {
	if p.tok.kind == tokOp && p.tok.text == "-" {
		p.next()
		x, err := p.unary()
		if err != nil {
			return nil, err
		}
		return unaryNode{x: x}, nil
	}
	return p.primary()
}

// primary := number | ident | '(' expr ')'
func (p *parser) primary() (node, error) {
	switch p.tok.kind {
	case tokNum:
		v, ok := new(big.Rat).SetString(p.tok.text)
		if !ok || strings.Count(p.tok.text, ".") > 1 {
			return nil, p.errorf("数値 %q が正しくありません", p.tok.text)
		}
		p.next()
		return numNode{v: v}, nil
	case tokIdent:
		name := p.tok.text
		p.next()
		return identNode{name: name}, nil
	case tokOp:
		if p.tok.text == "(" {
			p.next()
			x, err := p.expr()
			if err != nil {
				return nil, err
			}
			if p.tok.kind != tokOp || p.tok.text != ")" {
				return nil, p.errorf("「)」が必要です")
			}
			p.next()
			return x, nil
		}
		return nil, p.errorf("%q は使えません", p.tok.text)
	default:
		return nil, p.errorf("式が途中で終わっています")
	}
}

func isDigit(c byte) bool      { return c >= '0' && c <= '9' }
func isIdentStart(c byte) bool { return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }
func isIdentPart(c byte) bool  { return isIdentStart(c) || isDigit(c) }
