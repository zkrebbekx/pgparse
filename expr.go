package pgparse

import (
	"strconv"
	"strings"
)

// parseExpr is the expression entry point. Precedence is encoded by the call
// chain: OR → AND → NOT → comparison → additive → multiplicative → unary →
// postfix → primary (low to high binding).
func (p *Parser) parseExpr() (Expr, error) { return p.parseOr() }

func (p *Parser) parseOr() (Expr, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.acceptKw(kwOr) {
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{Op: "OR", Left: left, Right: right}
	}
	return left, nil
}

func (p *Parser) parseAnd() (Expr, error) {
	left, err := p.parseNot()
	if err != nil {
		return nil, err
	}
	for p.acceptKw(kwAnd) {
		right, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{Op: "AND", Left: left, Right: right}
	}
	return left, nil
}

func (p *Parser) parseNot() (Expr, error) {
	if p.isKw(kwNot) {
		p.advance()
		operand, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		return &UnaryExpr{Op: "NOT", Operand: operand}, nil
	}
	return p.parseComparison()
}

var compOps = map[TokenType]string{
	TokenEq: "=", TokenNeq: "<>", TokenLt: "<", TokenLte: "<=",
	TokenGt: ">", TokenGte: ">=",
}

func (p *Parser) parseComparison() (Expr, error) {
	left, err := p.parseAdditive()
	if err != nil {
		return nil, err
	}
	for {
		if op, ok := compOps[p.cur().Type]; ok {
			p.advance()
			right, err := p.parseAdditive()
			if err != nil {
				return nil, err
			}
			left = &BinaryExpr{Op: op, Left: left, Right: right}
			continue
		}
		if p.isKw(kwIs) {
			left, err = p.parseIsTail(left)
			if err != nil {
				return nil, err
			}
			continue
		}
		// optional NOT prefixing IN / BETWEEN / LIKE / ILIKE
		not := false
		if p.isKw(kwNot) {
			n := p.peekAt(1)
			if n.Type == TokenKeyword && (n.Kw == kwIn || n.Kw == kwBetween || n.Kw == kwLike || n.Kw == kwILike) {
				p.advance()
				not = true
			} else {
				break
			}
		}
		switch {
		case p.isKw(kwIn):
			left, err = p.parseInTail(left, not)
		case p.isKw(kwBetween):
			left, err = p.parseBetweenTail(left, not)
		case p.isKw(kwLike):
			p.advance()
			left, err = p.parseLikeTail(left, not, false)
		case p.isKw(kwILike):
			p.advance()
			left, err = p.parseLikeTail(left, not, true)
		default:
			if not {
				return nil, p.errf(p.cur(), "expected IN/BETWEEN/LIKE after NOT")
			}
			return left, nil
		}
		if err != nil {
			return nil, err
		}
	}
	return left, nil
}

func (p *Parser) parseIsTail(left Expr) (Expr, error) {
	p.advance() // IS
	not := p.acceptKw(kwNot)
	switch {
	case p.acceptKw(kwNull):
		return &IsExpr{Expr: left, Not: not, Kind: LitNull}, nil
	case p.acceptKw(kwTrue):
		return &IsExpr{Expr: left, Not: not, Kind: LitBool, Bool: true}, nil
	case p.acceptKw(kwFalse):
		return &IsExpr{Expr: left, Not: not, Kind: LitBool, Bool: false}, nil
	}
	return nil, p.errf(p.cur(), "expected NULL/TRUE/FALSE after IS")
}

func (p *Parser) parseInTail(left Expr, not bool) (Expr, error) {
	p.advance() // IN
	if _, err := p.expectType(TokenLParen, "'(' after IN"); err != nil {
		return nil, err
	}
	in := &InExpr{Expr: left, Not: not}
	if p.isKw(kwSelect) || p.isKw(kwWith) {
		sub, err := p.parseSelect()
		if err != nil {
			return nil, err
		}
		in.Subquery = sub
	} else {
		list, err := p.parseExprList()
		if err != nil {
			return nil, err
		}
		in.List = list
	}
	if _, err := p.expectType(TokenRParen, "')'"); err != nil {
		return nil, err
	}
	return in, nil
}

func (p *Parser) parseBetweenTail(left Expr, not bool) (Expr, error) {
	p.advance() // BETWEEN
	low, err := p.parseAdditive()
	if err != nil {
		return nil, err
	}
	if !p.acceptKw(kwAnd) {
		return nil, p.errf(p.cur(), "expected AND in BETWEEN")
	}
	high, err := p.parseAdditive()
	if err != nil {
		return nil, err
	}
	return &BetweenExpr{Expr: left, Not: not, Low: low, High: high}, nil
}

func (p *Parser) parseLikeTail(left Expr, not, ilike bool) (Expr, error) {
	pat, err := p.parseAdditive()
	if err != nil {
		return nil, err
	}
	return &LikeExpr{Expr: left, Pattern: pat, Not: not, ILike: ilike}, nil
}

func (p *Parser) parseAdditive() (Expr, error) {
	left, err := p.parseMultiplicative()
	if err != nil {
		return nil, err
	}
	for {
		var op string
		switch p.cur().Type {
		case TokenPlus:
			op = "+"
		case TokenMinus:
			op = "-"
		case TokenConcat:
			op = "||"
		default:
			return left, nil
		}
		p.advance()
		right, err := p.parseMultiplicative()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{Op: op, Left: left, Right: right}
	}
}

func (p *Parser) parseMultiplicative() (Expr, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for {
		var op string
		switch p.cur().Type {
		case TokenStar:
			op = "*"
		case TokenSlash:
			op = "/"
		case TokenPercent:
			op = "%"
		case TokenCaret:
			op = "^"
		default:
			return left, nil
		}
		p.advance()
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{Op: op, Left: left, Right: right}
	}
}

func (p *Parser) parseUnary() (Expr, error) {
	switch p.cur().Type {
	case TokenMinus:
		p.advance()
		operand, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &UnaryExpr{Op: "-", Operand: operand}, nil
	case TokenPlus:
		p.advance()
		return p.parseUnary()
	}
	return p.parsePostfix()
}

// parsePostfix handles trailing :: casts (left-associative, highest binding).
func (p *Parser) parsePostfix() (Expr, error) {
	e, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	for p.cur().Type == TokenCast {
		p.advance()
		typ, err := p.parseTypeName()
		if err != nil {
			return nil, err
		}
		e = &CastExpr{Expr: e, Type: typ}
	}
	return e, nil
}

func (p *Parser) parsePrimary() (Expr, error) {
	t := p.cur()
	switch t.Type {
	case TokenNumber:
		p.advance()
		return numberLiteral(t.Val), nil
	case TokenString:
		p.advance()
		return &Literal{Kind: LitString, Val: unquoteString(t.Val)}, nil
	case TokenParam:
		p.advance()
		n, _ := strconv.Atoi(t.Val[1:])
		return &Param{Num: n}, nil
	case TokenStar:
		p.advance()
		return &Star{}, nil
	case TokenLParen:
		return p.parseParenOrSubquery()
	case TokenKeyword:
		return p.parseKeywordPrimary(t)
	case TokenIdent:
		return p.parseNameOrCall()
	}
	return nil, p.errf(t, "unexpected token in expression")
}

func (p *Parser) parseParenOrSubquery() (Expr, error) {
	p.advance() // (
	if p.isKw(kwSelect) || p.isKw(kwWith) {
		sub, err := p.parseSelect()
		if err != nil {
			return nil, err
		}
		if _, err := p.expectType(TokenRParen, "')'"); err != nil {
			return nil, err
		}
		return &SubqueryExpr{Select: sub}, nil
	}
	inner, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if _, err := p.expectType(TokenRParen, "')'"); err != nil {
		return nil, err
	}
	return &ParenExpr{Expr: inner}, nil
}

func (p *Parser) parseKeywordPrimary(t Token) (Expr, error) {
	switch t.Kw {
	case kwNull:
		p.advance()
		return &Literal{Kind: LitNull, Val: "NULL"}, nil
	case kwTrue:
		p.advance()
		return &Literal{Kind: LitBool, Val: "true"}, nil
	case kwFalse:
		p.advance()
		return &Literal{Kind: LitBool, Val: "false"}, nil
	case kwCase:
		return p.parseCase()
	case kwCast:
		return p.parseCastFunc()
	case kwExists:
		p.advance()
		if _, err := p.expectType(TokenLParen, "'(' after EXISTS"); err != nil {
			return nil, err
		}
		sub, err := p.parseSelect()
		if err != nil {
			return nil, err
		}
		if _, err := p.expectType(TokenRParen, "')'"); err != nil {
			return nil, err
		}
		return &ExistsExpr{Select: sub}, nil
	case kwInterval:
		p.advance()
		s := p.cur()
		if s.Type != TokenString {
			return nil, p.errf(s, "expected string after INTERVAL")
		}
		p.advance()
		return &CastExpr{Expr: &Literal{Kind: LitString, Val: unquoteString(s.Val)}, Type: "interval"}, nil
	}
	return nil, p.errf(t, "unexpected keyword %q in expression", t.Val)
}

// parseCase parses both simple and searched CASE expressions.
func (p *Parser) parseCase() (Expr, error) {
	p.advance() // CASE
	ce := &CaseExpr{}
	if !p.isKw(kwWhen) {
		op, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		ce.Operand = op
	}
	for p.acceptKw(kwWhen) {
		cond, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if !p.acceptKw(kwThen) {
			return nil, p.errf(p.cur(), "expected THEN in CASE")
		}
		res, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		ce.Whens = append(ce.Whens, CaseWhen{Cond: cond, Result: res})
	}
	if p.acceptKw(kwElse) {
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		ce.Else = e
	}
	if !p.acceptKw(kwEnd) {
		return nil, p.errf(p.cur(), "expected END to close CASE")
	}
	return ce, nil
}

func (p *Parser) parseCastFunc() (Expr, error) {
	p.advance() // CAST
	if _, err := p.expectType(TokenLParen, "'(' after CAST"); err != nil {
		return nil, err
	}
	e, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if !p.acceptKw(kwAs) {
		return nil, p.errf(p.cur(), "expected AS in CAST")
	}
	typ, err := p.parseTypeName()
	if err != nil {
		return nil, err
	}
	if _, err := p.expectType(TokenRParen, "')'"); err != nil {
		return nil, err
	}
	return &CastExpr{Expr: e, Type: typ}, nil
}

// parseNameOrCall parses a qualified name, a "table.*", or a function call.
func (p *Parser) parseNameOrCall() (Expr, error) {
	parts := []string{identText(p.advance())}
	for p.cur().Type == TokenDot {
		p.advance()
		if p.cur().Type == TokenStar {
			p.advance()
			parts = append(parts, "*")
			return &ColumnRef{Parts: parts}, nil
		}
		id, err := p.parseIdent("name after '.'")
		if err != nil {
			return nil, err
		}
		parts = append(parts, id)
	}
	if p.cur().Type == TokenLParen {
		return p.parseCallTail(parts)
	}
	return &ColumnRef{Parts: parts}, nil
}

// parseCallTail parses the "( ... )" of a function call, given its name parts.
func (p *Parser) parseCallTail(parts []string) (Expr, error) {
	fc := &FuncCall{}
	switch len(parts) {
	case 1:
		fc.Name = parts[0]
	case 2:
		fc.Schema, fc.Name = parts[0], parts[1]
	default:
		return nil, p.errf(p.cur(), "function name has too many qualifiers")
	}
	p.advance() // (
	if p.cur().Type == TokenStar {
		p.advance()
		fc.Star = true
	} else if p.cur().Type != TokenRParen {
		fc.Distinct = p.acceptKw(kwDistinct)
		args, err := p.parseExprList()
		if err != nil {
			return nil, err
		}
		fc.Args = args
	}
	if _, err := p.expectType(TokenRParen, "')' after function arguments"); err != nil {
		return nil, err
	}
	if p.acceptKw(kwOver) {
		w, err := p.parseWindowSpec()
		if err != nil {
			return nil, err
		}
		fc.Over = w
	}
	return fc, nil
}

func (p *Parser) parseWindowSpec() (*WindowDef, error) {
	if _, err := p.expectType(TokenLParen, "'(' after OVER"); err != nil {
		return nil, err
	}
	w := &WindowDef{}
	if p.isKw(kwPartition) {
		p.advance()
		if !p.acceptKw(kwBy) {
			return nil, p.errf(p.cur(), "expected BY after PARTITION")
		}
		parts, err := p.parseExprList()
		if err != nil {
			return nil, err
		}
		w.PartitionBy = parts
	}
	if p.isKw(kwOrder) {
		p.advance()
		if !p.acceptKw(kwBy) {
			return nil, p.errf(p.cur(), "expected BY after ORDER")
		}
		ord, err := p.parseOrderList()
		if err != nil {
			return nil, err
		}
		w.OrderBy = ord
	}
	if _, err := p.expectType(TokenRParen, "')' to close OVER"); err != nil {
		return nil, err
	}
	return w, nil
}

// parseTypeName parses a single-token type name plus optional precision and
// array suffix, e.g. int, varchar(255), numeric(10,2), text[].
func (p *Parser) parseTypeName() (string, error) {
	t := p.cur()
	if t.Type != TokenIdent && t.Type != TokenKeyword {
		return "", p.errf(t, "expected a type name")
	}
	p.advance()
	var b strings.Builder
	b.WriteString(strings.ToLower(t.Val))
	if p.cur().Type == TokenLParen {
		b.WriteByte('(')
		p.advance()
		first := true
		for p.cur().Type != TokenRParen && !p.atEOF() {
			if !first {
				b.WriteByte(',')
			}
			first = false
			b.WriteString(p.advance().Val)
			p.acceptType(TokenComma)
		}
		if _, err := p.expectType(TokenRParen, "')' in type modifier"); err != nil {
			return "", err
		}
		b.WriteByte(')')
	}
	for p.cur().Type == TokenLBracket {
		p.advance()
		p.acceptType(TokenRBracket)
		b.WriteString("[]")
	}
	return b.String(), nil
}

// numberLiteral classifies a numeric literal as int or float.
func numberLiteral(raw string) *Literal {
	if strings.ContainsAny(raw, ".eE") {
		return &Literal{Kind: LitFloat, Val: raw}
	}
	return &Literal{Kind: LitInt, Val: raw}
}

// unquoteString strips the surrounding quotes from a SQL string literal and
// collapses doubled quotes. Dollar-quoted bodies are returned verbatim.
func unquoteString(raw string) string {
	if len(raw) >= 2 && raw[0] == '\'' {
		return strings.ReplaceAll(raw[1:len(raw)-1], "''", "'")
	}
	if len(raw) >= 2 && raw[0] == '$' {
		// $tag$ body $tag$ — strip both tags
		if i := strings.IndexByte(raw[1:], '$'); i >= 0 {
			tag := raw[:i+2]
			return raw[len(tag) : len(raw)-len(tag)]
		}
	}
	return raw
}
