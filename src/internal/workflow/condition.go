package workflow

import (
	"fmt"
	"strconv"
	"strings"
)

// EvaluateCondition evaluates a simple boolean/logic expression.
// Supported operators: ==, !=, >, >=, <, <=, contains, startswith, endswith
// Boolean operators: &&, ||, !
// Functions: empty(val), notEmpty(val)
// Values: string literals ("..."), numbers, true/false, variable references
func EvaluateCondition(expr string, ctx map[string]any) (bool, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return false, fmt.Errorf("empty expression")
	}

	p := &parser{input: expr, pos: 0, ctx: ctx}
	result, err := p.parseOr()
	if err != nil {
		return false, err
	}
	// All input should be consumed
	p.skipSpaces()
	if p.pos < len(p.input) {
		return false, fmt.Errorf("unexpected %q at position %d", string(p.input[p.pos]), p.pos)
	}
	return result, nil
}

// ─── Recursive-descent parser ────────────────────────────────────

type parser struct {
	input string
	pos   int
	ctx   map[string]any
}

func (p *parser) peek() byte {
	if p.pos >= len(p.input) {
		return 0
	}
	return p.input[p.pos]
}

func (p *parser) advance() { p.pos++ }

func (p *parser) skipSpaces() {
	for p.pos < len(p.input) && p.input[p.pos] == ' ' {
		p.pos++
	}
}

// parseOr handles || (lowest precedence).
func (p *parser) parseOr() (bool, error) {
	left, err := p.parseAnd()
	if err != nil {
		return false, err
	}
	for {
		p.skipSpaces()
		if p.pos+1 < len(p.input) && p.input[p.pos] == '|' && p.input[p.pos+1] == '|' {
			p.pos += 2
			right, err := p.parseAnd()
			if err != nil {
				return false, err
			}
			left = left || right
		} else {
			break
		}
	}
	return left, nil
}

// parseAnd handles &&.
func (p *parser) parseAnd() (bool, error) {
	left, err := p.parseNot()
	if err != nil {
		return false, err
	}
	for {
		p.skipSpaces()
		if p.pos+1 < len(p.input) && p.input[p.pos] == '&' && p.input[p.pos+1] == '&' {
			p.pos += 2
			right, err := p.parseNot()
			if err != nil {
				return false, err
			}
			left = left && right
		} else {
			break
		}
	}
	return left, nil
}

// parseNot handles ! prefix.
func (p *parser) parseNot() (bool, error) {
	p.skipSpaces()
	if p.peek() == '!' {
		p.advance()
		val, err := p.parseAtom()
		if err != nil {
			return false, err
		}
		return !val, nil
	}
	return p.parseAtom()
}

// parseAtom handles comparison, parenthesized expression, or function call.
func (p *parser) parseAtom() (bool, error) {
	p.skipSpaces()
	if p.peek() == '(' {
		p.advance()
		val, err := p.parseOr()
		if err != nil {
			return false, err
		}
		p.skipSpaces()
		if p.peek() != ')' {
			return false, fmt.Errorf("expected ) at position %d", p.pos)
		}
		p.advance()
		return val, nil
	}

	// Try function call: empty(val) or notEmpty(val)
	if p.matchFunc("empty") {
		return p.parseFuncCall(true) // empty → want empty
	}
	if p.matchFunc("notEmpty") {
		return p.parseFuncCall(false) // notEmpty → want not empty
	}

	// Otherwise parse a comparison
	return p.parseComparison()
}

func (p *parser) matchFunc(name string) bool {
	saved := p.pos
	p.skipSpaces()
	if p.pos+len(name) <= len(p.input) && p.input[p.pos:p.pos+len(name)] == name {
		after := p.pos + len(name)
		// Must be followed by (
		afterSp := after
		for afterSp < len(p.input) && p.input[afterSp] == ' ' {
			afterSp++
		}
		if afterSp < len(p.input) && p.input[afterSp] == '(' {
			p.pos = afterSp + 1 // skip name and (
			return true
		}
	}
	p.pos = saved
	return false
}

func (p *parser) parseFuncCall(expectEmpty bool) (bool, error) {
	// Parse the argument value
	val, err := p.parseValue()
	if err != nil {
		return false, err
	}
	p.skipSpaces()
	if p.peek() != ')' {
		return false, fmt.Errorf("expected ) after function argument")
	}
	p.advance()
	isEmpty := isValueEmpty(val)
	if expectEmpty {
		return isEmpty, nil
	}
	return !isEmpty, nil
}

// parseComparison parses value op value.
func (p *parser) parseComparison() (bool, error) {
	left, err := p.parseValue()
	if err != nil {
		return false, err
	}

	p.skipSpaces()
	op := p.readOp()
	if op == "" {
		// No operator — treat as truthiness check
		return isTruthy(left), nil
	}

	right, err := p.parseValue()
	if err != nil {
		return false, err
	}

	return compare(left, op, right)
}

// readOp reads a comparison operator.
func (p *parser) readOp() string {
	if p.pos+1 < len(p.input) {
		two := p.input[p.pos : p.pos+2]
		switch two {
		case "==", "!=", ">=", "<=":
			p.pos += 2
			return two
		case "||", "&&":
			return "" // these are handled at higher levels
		}
	}
	if p.peek() == '>' || p.peek() == '<' {
		op := string(p.peek())
		p.advance()
		return op
	}
	// Word operators
	saved := p.pos
	for _, w := range []string{"contains", "startswith", "endswith"} {
		if p.pos+len(w) <= len(p.input) && p.input[p.pos:p.pos+len(w)] == w {
			p.pos += len(w)
			return w
		}
	}
	p.pos = saved
	return ""
}

// parseValue parses a literal or variable reference.
func (p *parser) parseValue() (any, error) {
	p.skipSpaces()
	if p.peek() == '"' || p.peek() == '\'' {
		return p.parseString()
	}
	if p.peek() >= '0' && p.peek() <= '9' || p.peek() == '-' {
		return p.parseNumber()
	}
	if p.matchWord("true") {
		return true, nil
	}
	if p.matchWord("false") {
		return false, nil
	}
	if p.matchWord("null") || p.matchWord("nil") {
		return nil, nil
	}
	// Variable reference
	return p.parseVariable()
}

func (p *parser) matchWord(w string) bool {
	saved := p.pos
	p.skipSpaces()
	if p.pos+len(w) <= len(p.input) && p.input[p.pos:p.pos+len(w)] == w {
		// must be followed by space, operator, or end
		after := p.pos + len(w)
		if after >= len(p.input) || isDelimiter(p.input[after]) {
			p.pos = after
			return true
		}
	}
	p.pos = saved
	return false
}

func isDelimiter(b byte) bool {
	return b == ' ' || b == ')' || b == '!' || b == '&' || b == '|' ||
		b == '>' || b == '<' || b == '=' || b == 0
}

func (p *parser) parseString() (string, error) {
	quote := p.peek()
	p.advance()
	var buf strings.Builder
	for p.pos < len(p.input) && p.peek() != quote {
		if p.peek() == '\\' {
			p.advance()
		}
		buf.WriteByte(p.peek())
		p.advance()
	}
	if p.peek() != quote {
		return "", fmt.Errorf("unterminated string at position %d", p.pos)
	}
	p.advance()
	return buf.String(), nil
}

func (p *parser) parseNumber() (float64, error) {
	start := p.pos
	if p.peek() == '-' {
		p.advance()
	}
	for p.pos < len(p.input) && p.input[p.pos] >= '0' && p.input[p.pos] <= '9' {
		p.advance()
	}
	if p.pos < len(p.input) && p.input[p.pos] == '.' {
		p.advance()
		for p.pos < len(p.input) && p.input[p.pos] >= '0' && p.input[p.pos] <= '9' {
			p.advance()
		}
	}
	return strconv.ParseFloat(p.input[start:p.pos], 64)
}

func (p *parser) parseVariable() (any, error) {
	start := p.pos
	p.skipSpaces()
	for p.pos < len(p.input) && !isDelimiter(p.input[p.pos]) {
		p.advance()
	}
	if p.pos == start {
		return nil, fmt.Errorf("expected value at position %d", p.pos)
	}
	name := strings.TrimSpace(p.input[start:p.pos])
	val, err := resolvePath(name, p.ctx)
	if err != nil {
		return nil, err
	}
	return val, nil
}

// ─── Comparison logic ────────────────────────────────────────────

func compare(left any, op string, right any) (bool, error) {
	// String comparison
	ls, lok := toString(left)
	rs, rok := toString(right)
	if lok && rok {
		switch op {
		case "==":
			return ls == rs, nil
		case "!=":
			return ls != rs, nil
		case "contains":
			return strings.Contains(ls, rs), nil
		case "startswith":
			return strings.HasPrefix(ls, rs), nil
		case "endswith":
			return strings.HasSuffix(ls, rs), nil
		}
	}

	// Numeric comparison
	ln, lerr := toFloat(left)
	rn, rerr := toFloat(right)
	if lerr == nil && rerr == nil {
		switch op {
		case "==":
			return ln == rn, nil
		case "!=":
			return ln != rn, nil
		case ">":
			return ln > rn, nil
		case ">=":
			return ln >= rn, nil
		case "<":
			return ln < rn, nil
		case "<=":
			return ln <= rn, nil
		}
	}

	// Bool comparison
	lb, lbok := left.(bool)
	rb, rbok := right.(bool)
	if lbok && rbok {
		switch op {
		case "==":
			return lb == rb, nil
		case "!=":
			return lb != rb, nil
		}
	}

	return false, fmt.Errorf("cannot compare %T %s %T", left, op, right)
}

// ─── Helpers ─────────────────────────────────────────────────────

func toString(v any) (string, bool) {
	switch val := v.(type) {
	case string:
		return val, true
	case fmt.Stringer:
		return val.String(), true
	}
	return "", false
}

func toFloat(v any) (float64, error) {
	switch val := v.(type) {
	case float64:
		return val, nil
	case float32:
		return float64(val), nil
	case int:
		return float64(val), nil
	case int64:
		return float64(val), nil
	case string:
		return strconv.ParseFloat(val, 64)
	}
	return 0, fmt.Errorf("%T is not a number", v)
}

func isValueEmpty(v any) bool {
	if v == nil {
		return true
	}
	switch val := v.(type) {
	case string:
		return val == "" || val == "null" || val == "nil"
	case []any:
		return len(val) == 0
	case map[string]any:
		return len(val) == 0
	case bool:
		return !val
	}
	return false
}

func isTruthy(v any) bool {
	if v == nil {
		return false
	}
	switch val := v.(type) {
	case bool:
		return val
	case string:
		return val != "" && val != "false" && val != "null" && val != "nil"
	case float64:
		return val != 0
	case int:
		return val != 0
	}
	return true
}
