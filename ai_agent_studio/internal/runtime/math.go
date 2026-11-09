package runtime

import (
	"errors"
	"math"
	"strconv"
	"strings"
)

var errBadExpression = errors.New("expression must use numbers, + - * /, and parentheses")

// Calculate evaluates a bounded arithmetic expression without any code
// execution: numbers, + - * /, unary minus and parentheses only.
func Calculate(expression string) (float64, error) {
	if len(expression) == 0 || len(expression) > 200 {
		return 0, errBadExpression
	}
	p := &mathParser{input: strings.ReplaceAll(expression, " ", "")}
	value, err := p.expr(0)
	if err != nil {
		return 0, err
	}
	if p.pos != len(p.input) || math.IsInf(value, 0) || math.IsNaN(value) {
		return 0, errBadExpression
	}
	return value, nil
}

type mathParser struct {
	input string
	pos   int
}

func (p *mathParser) expr(depth int) (float64, error) {
	if depth > 32 {
		return 0, errBadExpression
	}
	left, err := p.term(depth)
	if err != nil {
		return 0, err
	}
	for p.pos < len(p.input) && (p.input[p.pos] == '+' || p.input[p.pos] == '-') {
		op := p.input[p.pos]
		p.pos++
		right, err := p.term(depth)
		if err != nil {
			return 0, err
		}
		if op == '+' {
			left += right
		} else {
			left -= right
		}
	}
	return left, nil
}

func (p *mathParser) term(depth int) (float64, error) {
	left, err := p.factor(depth)
	if err != nil {
		return 0, err
	}
	for p.pos < len(p.input) && (p.input[p.pos] == '*' || p.input[p.pos] == '/') {
		op := p.input[p.pos]
		p.pos++
		right, err := p.factor(depth)
		if err != nil {
			return 0, err
		}
		if op == '*' {
			left *= right
		} else {
			if right == 0 {
				return 0, errors.New("division by zero")
			}
			left /= right
		}
	}
	return left, nil
}

func (p *mathParser) factor(depth int) (float64, error) {
	if p.pos >= len(p.input) {
		return 0, errBadExpression
	}
	switch c := p.input[p.pos]; {
	case c == '-':
		p.pos++
		v, err := p.factor(depth + 1)
		return -v, err
	case c == '(':
		p.pos++
		v, err := p.expr(depth + 1)
		if err != nil {
			return 0, err
		}
		if p.pos >= len(p.input) || p.input[p.pos] != ')' {
			return 0, errBadExpression
		}
		p.pos++
		return v, nil
	case (c >= '0' && c <= '9') || c == '.':
		start := p.pos
		for p.pos < len(p.input) && ((p.input[p.pos] >= '0' && p.input[p.pos] <= '9') || p.input[p.pos] == '.') {
			p.pos++
		}
		return strconv.ParseFloat(p.input[start:p.pos], 64)
	default:
		return 0, errBadExpression
	}
}
