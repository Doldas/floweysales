package server

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// A screen rule is a small boolean expression over row fields, for example
//
//	change_1d_pct <= -3 and (rsi14 < 30 or from_high_pct < -20)
//	rsi_state == "oversold" and not above_sma200
//	title contains "guidance"
//
// Comparisons: < <= > >= == != and contains. Combine with and, or, not and
// parentheses. Fields may use dots for nested objects. A row without the
// field never matches a comparison: absent data is not zero.
type screenExpr interface{ eval(row map[string]any) bool }

var screenFieldPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,47}(\.[A-Za-z_][A-Za-z0-9_]{0,47}){0,3}$`)

type screenAnd struct{ left, right screenExpr }
type screenOr struct{ left, right screenExpr }
type screenNot struct{ inner screenExpr }
type screenTruthy struct{ field string }
type screenCompare struct {
	field, op string
	literal   any // float64, string or bool
}

func (x screenAnd) eval(r map[string]any) bool { return x.left.eval(r) && x.right.eval(r) }
func (x screenOr) eval(r map[string]any) bool  { return x.left.eval(r) || x.right.eval(r) }
func (x screenNot) eval(r map[string]any) bool { return !x.inner.eval(r) }
func (x screenTruthy) eval(r map[string]any) bool {
	v, ok := rowField(r, x.field)
	if !ok {
		return false
	}
	switch b := v.(type) {
	case bool:
		return b
	case string:
		return b != ""
	}
	n, isNum := numberOf(v)
	return isNum && n != 0
}

func (x screenCompare) eval(r map[string]any) bool {
	v, ok := rowField(r, x.field)
	if !ok {
		return false
	}
	switch lit := x.literal.(type) {
	case float64:
		n, isNum := numberOf(v)
		if !isNum {
			return false
		}
		switch x.op {
		case "<":
			return n < lit
		case "<=":
			return n <= lit
		case ">":
			return n > lit
		case ">=":
			return n >= lit
		case "==":
			return n == lit
		case "!=":
			return n != lit
		}
	case bool:
		b, isBool := v.(bool)
		if !isBool {
			return false
		}
		if x.op == "==" {
			return b == lit
		}
		return x.op == "!=" && b != lit
	case string:
		s := textOf(v)
		switch x.op {
		case "==":
			return strings.EqualFold(s, lit)
		case "!=":
			return !strings.EqualFold(s, lit)
		case "contains":
			return strings.Contains(strings.ToLower(s), strings.ToLower(lit))
		}
	}
	return false
}

type screenToken struct {
	kind  string // ident, number, string, op, lparen, rparen
	value string
}

func lexScreenRule(rule string) ([]screenToken, error) {
	var tokens []screenToken
	for i := 0; i < len(rule); {
		c := rule[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '(':
			tokens = append(tokens, screenToken{"lparen", "("})
			i++
		case c == ')':
			tokens = append(tokens, screenToken{"rparen", ")"})
			i++
		case c == '<' || c == '>' || c == '=' || c == '!':
			op := string(c)
			if i+1 < len(rule) && rule[i+1] == '=' {
				op += "="
				i++
			}
			i++
			switch op {
			case "=":
				op = "=="
			case "!":
				return nil, errors.New("use 'not' or '!=' in screen rules")
			}
			tokens = append(tokens, screenToken{"op", op})
		case c == '"' || c == '\'':
			end := strings.IndexByte(rule[i+1:], c)
			if end < 0 {
				return nil, errors.New("unterminated text in screen rule")
			}
			tokens = append(tokens, screenToken{"string", rule[i+1 : i+1+end]})
			i += end + 2
		case c == '-' || c == '+' || c == '.' || (c >= '0' && c <= '9'):
			j := i + 1
			for j < len(rule) && (rule[j] == '.' || (rule[j] >= '0' && rule[j] <= '9')) {
				j++
			}
			tokens = append(tokens, screenToken{"number", rule[i:j]})
			i = j
		case c == '_' || (c|0x20 >= 'a' && c|0x20 <= 'z'):
			j := i + 1
			for j < len(rule) && (rule[j] == '_' || rule[j] == '.' || (rule[j] >= '0' && rule[j] <= '9') || (rule[j]|0x20 >= 'a' && rule[j]|0x20 <= 'z')) {
				j++
			}
			word := rule[i:j]
			switch strings.ToLower(word) {
			case "and", "or", "not", "contains":
				tokens = append(tokens, screenToken{"op", strings.ToLower(word)})
			default:
				tokens = append(tokens, screenToken{"ident", word})
			}
			i = j
		default:
			return nil, fmt.Errorf("unexpected character %q in screen rule", c)
		}
	}
	return tokens, nil
}

type screenParser struct {
	tokens []screenToken
	pos    int
	depth  int
}

func (p *screenParser) peek() (screenToken, bool) {
	if p.pos >= len(p.tokens) {
		return screenToken{}, false
	}
	return p.tokens[p.pos], true
}

func (p *screenParser) isOp(op string) bool {
	t, ok := p.peek()
	return ok && t.kind == "op" && t.value == op
}

func (p *screenParser) or() (screenExpr, error) {
	left, err := p.and()
	for err == nil && p.isOp("or") {
		p.pos++
		var right screenExpr
		if right, err = p.and(); err == nil {
			left = screenOr{left, right}
		}
	}
	return left, err
}

func (p *screenParser) and() (screenExpr, error) {
	left, err := p.unary()
	for err == nil && p.isOp("and") {
		p.pos++
		var right screenExpr
		if right, err = p.unary(); err == nil {
			left = screenAnd{left, right}
		}
	}
	return left, err
}

func (p *screenParser) unary() (screenExpr, error) {
	if p.depth++; p.depth > 32 {
		return nil, errors.New("screen rule is nested too deeply")
	}
	defer func() { p.depth-- }()
	if p.isOp("not") {
		p.pos++
		inner, err := p.unary()
		return screenNot{inner}, err
	}
	t, ok := p.peek()
	if !ok {
		return nil, errors.New("screen rule ends unexpectedly")
	}
	if t.kind == "lparen" {
		p.pos++
		inner, err := p.or()
		if err != nil {
			return nil, err
		}
		if t, ok := p.peek(); !ok || t.kind != "rparen" {
			return nil, errors.New("missing ) in screen rule")
		}
		p.pos++
		return inner, nil
	}
	if t.kind != "ident" || !screenFieldPattern.MatchString(t.value) {
		return nil, fmt.Errorf("expected a field name, found %q", t.value)
	}
	p.pos++
	op, ok := p.peek()
	if !ok || op.kind != "op" || op.value == "and" || op.value == "or" || op.value == "not" {
		return screenTruthy{t.value}, nil
	}
	p.pos++
	lit, ok := p.peek()
	if !ok {
		return nil, fmt.Errorf("missing value after %s", op.value)
	}
	p.pos++
	cmp := screenCompare{field: t.value, op: op.value}
	switch {
	case lit.kind == "number":
		n, err := strconv.ParseFloat(lit.value, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid number %q", lit.value)
		}
		cmp.literal = n
	case lit.kind == "string":
		cmp.literal = lit.value
	case lit.kind == "ident" && (strings.EqualFold(lit.value, "true") || strings.EqualFold(lit.value, "false")):
		cmp.literal = strings.EqualFold(lit.value, "true")
	case lit.kind == "ident":
		cmp.literal = lit.value // bare words compare as text: rsi_state == oversold
	default:
		return nil, fmt.Errorf("expected a value after %s", op.value)
	}
	_, isText := cmp.literal.(string)
	_, isBool := cmp.literal.(bool)
	switch {
	case op.value == "contains" && !isText:
		return nil, errors.New("contains needs text, for example title contains \"guidance\"")
	case (isText || isBool) && op.value != "==" && op.value != "!=" && op.value != "contains":
		return nil, fmt.Errorf("%s compares numbers only", op.value)
	}
	return cmp, nil
}

func parseScreenRule(rule string) (screenExpr, error) {
	if len(rule) > 500 {
		return nil, errors.New("screen rule exceeds 500 characters")
	}
	tokens, err := lexScreenRule(rule)
	if err != nil {
		return nil, err
	}
	if len(tokens) == 0 {
		return nil, errors.New("screen rule is empty")
	}
	p := &screenParser{tokens: tokens}
	expr, err := p.or()
	if err != nil {
		return nil, err
	}
	if p.pos != len(p.tokens) {
		return nil, fmt.Errorf("unexpected %q in screen rule", p.tokens[p.pos].value)
	}
	return expr, nil
}
