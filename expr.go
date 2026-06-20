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
	left, err := p.parseOtherOp()
	if err != nil {
		return nil, err
	}
	for {
		if op, ok := compOps[p.cur().Type]; ok {
			p.advance()
			if p.isKw(kwAny) || p.isKw(kwSome) || p.isKw(kwAll) {
				left, err = p.parseAnyAll(op, left)
				if err != nil {
					return nil, err
				}
				continue
			}
			right, err := p.parseOtherOp()
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
	case p.acceptKw(kwDistinct):
		if !p.acceptKw(kwFrom) {
			return nil, p.errf(p.cur(), "expected FROM after IS DISTINCT")
		}
		right, err := p.parseOtherOp()
		if err != nil {
			return nil, err
		}
		return &IsDistinctExpr{Left: left, Right: right, Not: not}, nil
	}
	return nil, p.errf(p.cur(), "expected NULL/TRUE/FALSE/DISTINCT after IS")
}

// parseArray parses ARRAY[...] or ARRAY(subquery), the ARRAY keyword consumed.
func (p *Parser) parseArray() (Expr, error) {
	p.advance() // ARRAY
	if p.cur().Type == TokenLBracket {
		return p.parseArrayBrackets()
	}
	if _, err := p.expectType(TokenLParen, "'[' or '(' after ARRAY"); err != nil {
		return nil, err
	}
	sub, err := p.parseSelect()
	if err != nil {
		return nil, err
	}
	if _, err := p.expectType(TokenRParen, "')'"); err != nil {
		return nil, err
	}
	return &ArrayExpr{Subquery: sub}, nil
}

// parseArrayBrackets parses a "[...]" array literal whose elements may be nested
// "[...]" sub-arrays (multidimensional arrays).
func (p *Parser) parseArrayBrackets() (Expr, error) {
	p.advance() // [
	a := &ArrayExpr{}
	if p.cur().Type != TokenRBracket {
		for {
			var el Expr
			var err error
			if p.cur().Type == TokenLBracket {
				el, err = p.parseArrayBrackets()
			} else {
				el, err = p.parseExpr()
			}
			if err != nil {
				return nil, err
			}
			a.Elements = append(a.Elements, el)
			if !p.acceptType(TokenComma) {
				break
			}
		}
	}
	if _, err := p.expectType(TokenRBracket, "']' to close array"); err != nil {
		return nil, err
	}
	return a, nil
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
	low, err := p.parseOtherOp()
	if err != nil {
		return nil, err
	}
	if !p.acceptKw(kwAnd) {
		return nil, p.errf(p.cur(), "expected AND in BETWEEN")
	}
	high, err := p.parseOtherOp()
	if err != nil {
		return nil, err
	}
	return &BetweenExpr{Expr: left, Not: not, Low: low, High: high}, nil
}

func (p *Parser) parseLikeTail(left Expr, not, ilike bool) (Expr, error) {
	pat, err := p.parseOtherOp()
	if err != nil {
		return nil, err
	}
	return &LikeExpr{Expr: left, Pattern: pat, Not: not, ILike: ilike}, nil
}

// parseOtherOp handles PostgreSQL's open operator class (JSON/array/bitwise/
// regex operators plus ||), which binds tighter than comparison but looser than
// arithmetic. All such operators are left-associative.
func (p *Parser) parseOtherOp() (Expr, error) {
	left, err := p.parseAdditive()
	if err != nil {
		return nil, err
	}
	for p.cur().Type == TokenOp || p.cur().Type == TokenConcat {
		op := p.advance().Val
		if p.isKw(kwAny) || p.isKw(kwSome) || p.isKw(kwAll) {
			left, err = p.parseAnyAll(op, left)
			if err != nil {
				return nil, err
			}
			continue
		}
		right, err := p.parseAdditive()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{Op: op, Left: left, Right: right}
	}
	return left, nil
}

// parseAnyAll parses the right-hand "ANY/SOME/ALL (array | subquery)" of a
// quantified comparison, the operator already consumed.
func (p *Parser) parseAnyAll(op string, left Expr) (Expr, error) {
	any := p.isKw(kwAny) || p.isKw(kwSome)
	p.advance() // ANY/SOME/ALL
	if _, err := p.expectType(TokenLParen, "'(' after ANY/ALL"); err != nil {
		return nil, err
	}
	var right Expr
	if p.isKw(kwSelect) || p.isKw(kwWith) || p.cur().Type == TokenLParen {
		sub, err := p.parseSelect()
		if err != nil {
			return nil, err
		}
		right = &SubqueryExpr{Select: sub}
	} else {
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		right = e
	}
	if _, err := p.expectType(TokenRParen, "')'"); err != nil {
		return nil, err
	}
	return &AnyAllExpr{Op: op, Any: any, Left: left, Right: right}, nil
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
	switch {
	case p.cur().Type == TokenMinus:
		p.advance()
		operand, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &UnaryExpr{Op: "-", Operand: operand}, nil
	case p.cur().Type == TokenPlus:
		p.advance()
		return p.parseUnary()
	case p.cur().Type == TokenOp && p.cur().Val == "~":
		// Prefix bitwise NOT (the same token is the infix regex-match operator,
		// disambiguated by position).
		p.advance()
		operand, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &UnaryExpr{Op: "~", Operand: operand}, nil
	}
	return p.parsePostfix()
}

// parsePostfix handles trailing :: casts and array subscripts a[i] / a[lo:hi]
// (both left-associative, highest binding).
func (p *Parser) parsePostfix() (Expr, error) {
	e, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	for {
		if p.isWord("collate") {
			p.advance()
			coll, err := p.parseCollationName()
			if err != nil {
				return nil, err
			}
			e = &CollateExpr{Expr: e, Collation: coll}
			continue
		}
		switch p.cur().Type {
		case TokenCast:
			p.advance()
			typ, err := p.parseTypeName()
			if err != nil {
				return nil, err
			}
			e = &CastExpr{Expr: e, Type: typ}
		case TokenLBracket:
			e, err = p.parseSubscript(e)
			if err != nil {
				return nil, err
			}
		default:
			return e, nil
		}
	}
}

// parseCollationName reads a (possibly schema-qualified, possibly quoted)
// collation name, returning it as written.
func (p *Parser) parseCollationName() (string, error) {
	t := p.cur()
	if t.Type != TokenIdent {
		return "", p.errf(t, "expected a collation name")
	}
	p.advance()
	name := t.Val
	for p.cur().Type == TokenDot {
		p.advance()
		name += "." + p.advance().Val
	}
	return name, nil
}

// parseSubscript parses one [i] or [lo:hi] suffix. Either slice bound may be
// omitted (a[:hi], a[lo:]).
func (p *Parser) parseSubscript(base Expr) (Expr, error) {
	p.advance() // [
	sub := &SubscriptExpr{Base: base}
	if p.cur().Type != TokenColon && p.cur().Type != TokenRBracket {
		lo, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		sub.Lower = lo
	}
	if p.acceptType(TokenColon) {
		sub.Slice = true
		if p.cur().Type != TokenRBracket {
			hi, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			sub.Upper = hi
		}
	}
	if _, err := p.expectType(TokenRBracket, "']' to close subscript"); err != nil {
		return nil, err
	}
	return sub, nil
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
	// A comma turns the parenthesised group into a row/tuple constructor.
	if p.cur().Type == TokenComma {
		elems := []Expr{inner}
		for p.acceptType(TokenComma) {
			e, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			elems = append(elems, e)
		}
		if _, err := p.expectType(TokenRParen, "')'"); err != nil {
			return nil, err
		}
		return &RowExpr{Elements: elems}, nil
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
		// Optional unit qualifier: INTERVAL '90' day, INTERVAL '1-2' year to month.
		for isIntervalUnit(p.cur()) {
			p.advance()
		}
		return &CastExpr{Expr: &Literal{Kind: LitString, Val: unquoteString(s.Val)}, Type: "interval"}, nil
	case kwArray:
		return p.parseArray()
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
	// Typed string literal: TYPE 'literal' (e.g. date '1998-12-01').
	if len(parts) == 1 && p.cur().Type == TokenString && isTypeWord(parts[0]) {
		s := p.advance()
		return &CastExpr{
			Expr: &Literal{Kind: LitString, Val: unquoteString(s.Val)},
			Type: strings.ToLower(parts[0]),
		}, nil
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
	if fc.Schema == "" && isSpecialFunc(fc.Name) {
		if err := p.parseSpecialArgs(fc); err != nil {
			return nil, err
		}
	} else if p.cur().Type == TokenStar {
		p.advance()
		fc.Star = true
	} else if p.cur().Type != TokenRParen {
		fc.Distinct = p.acceptKw(kwDistinct)
		p.acceptWord("variadic")
		args, err := p.parseVariadicArgs()
		if err != nil {
			return nil, err
		}
		fc.Args = args
		// Aggregate ORDER BY: array_agg(x ORDER BY y DESC).
		if p.isKw(kwOrder) {
			p.advance()
			if !p.acceptKw(kwBy) {
				return nil, p.errf(p.cur(), "expected BY after ORDER")
			}
			fc.OrderBy, err = p.parseOrderList()
			if err != nil {
				return nil, err
			}
		}
	}
	if _, err := p.expectType(TokenRParen, "')' after function arguments"); err != nil {
		return nil, err
	}
	// WITHIN GROUP (ORDER BY ...) for ordered-set aggregates.
	if identIs(p.cur(), "within") {
		p.advance()
		if !p.acceptKw(kwGroup) {
			return nil, p.errf(p.cur(), "expected GROUP after WITHIN")
		}
		if _, err := p.expectType(TokenLParen, "'(' after WITHIN GROUP"); err != nil {
			return nil, err
		}
		if !(p.isKw(kwOrder)) {
			return nil, p.errf(p.cur(), "expected ORDER BY in WITHIN GROUP")
		}
		p.advance()
		if !p.acceptKw(kwBy) {
			return nil, p.errf(p.cur(), "expected BY after ORDER")
		}
		og, err := p.parseOrderList()
		if err != nil {
			return nil, err
		}
		fc.WithinGroup = og
		if _, err := p.expectType(TokenRParen, "')' to close WITHIN GROUP"); err != nil {
			return nil, err
		}
	}
	// FILTER (WHERE predicate).
	if identIs(p.cur(), "filter") {
		p.advance()
		if _, err := p.expectType(TokenLParen, "'(' after FILTER"); err != nil {
			return nil, err
		}
		if !p.acceptKw(kwWhere) {
			return nil, p.errf(p.cur(), "expected WHERE in FILTER")
		}
		pred, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		fc.Filter = pred
		if _, err := p.expectType(TokenRParen, "')' to close FILTER"); err != nil {
			return nil, err
		}
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

// parseVariadicArgs parses a function argument list allowing a VARIADIC marker
// before any argument and named-argument syntax (name => value).
func (p *Parser) parseVariadicArgs() ([]Expr, error) {
	var list []Expr
	for {
		p.acceptWord("variadic")
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if p.cur().Type == TokenOp && p.cur().Val == "=>" {
			p.advance()
			e, err = p.parseExpr()
			if err != nil {
				return nil, err
			}
		}
		list = append(list, e)
		if !p.acceptType(TokenComma) {
			break
		}
	}
	return list, nil
}

func (p *Parser) parseWindowSpec() (*WindowDef, error) {
	// OVER window_name — a reference to a named WINDOW definition.
	if p.cur().Type == TokenIdent {
		return &WindowDef{Ref: identText(p.advance())}, nil
	}
	if _, err := p.expectType(TokenLParen, "'(' or window name after OVER"); err != nil {
		return nil, err
	}
	w := &WindowDef{}
	// Optional base window name: OVER (w ORDER BY ...).
	if _, isFrame := frameMode(p.cur()); p.cur().Type == TokenIdent && !isFrame {
		w.Ref = identText(p.advance())
	}
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
	// Optional frame clause: ROWS|RANGE|GROUPS ...
	if mode, ok := frameMode(p.cur()); ok {
		p.advance()
		frame, err := p.parseFrame(mode)
		if err != nil {
			return nil, err
		}
		w.Frame = frame
	}
	if _, err := p.expectType(TokenRParen, "')' to close OVER"); err != nil {
		return nil, err
	}
	return w, nil
}

// frameMode reports whether t introduces a frame clause (ROWS/RANGE/GROUPS),
// which are non-reserved words matched by text.
func frameMode(t Token) (string, bool) {
	if t.Type != TokenIdent {
		return "", false
	}
	switch strings.ToLower(t.Val) {
	case "rows":
		return "ROWS", true
	case "range":
		return "RANGE", true
	case "groups":
		return "GROUPS", true
	}
	return "", false
}

// parseFrame parses the body of a frame clause after the mode word, supporting
// both the single-bound form and BETWEEN start AND end.
func (p *Parser) parseFrame(mode string) (*WindowFrame, error) {
	f := &WindowFrame{Mode: mode}
	if p.acceptKw(kwBetween) {
		start, err := p.parseFrameBound()
		if err != nil {
			return nil, err
		}
		if !p.acceptKw(kwAnd) {
			return nil, p.errf(p.cur(), "expected AND in frame BETWEEN")
		}
		end, err := p.parseFrameBound()
		if err != nil {
			return nil, err
		}
		f.Start, f.End = start, &end
	} else {
		start, err := p.parseFrameBound()
		if err != nil {
			return nil, err
		}
		f.Start = start
	}
	// Optional EXCLUDE clause — consume and ignore the variants we don't model.
	if identIs(p.cur(), "exclude") {
		p.advance()
		switch {
		case identIs(p.cur(), "current"):
			p.advance()
			if identIs(p.cur(), "row") {
				p.advance()
			}
		case identIs(p.cur(), "no"):
			p.advance()
			p.acceptWord("others")
		case identIs(p.cur(), "ties"), identIs(p.cur(), "others"), p.isKw(kwGroup):
			p.advance()
		}
	}
	return f, nil
}

// parseFrameBound parses one frame endpoint: UNBOUNDED PRECEDING/FOLLOWING,
// CURRENT ROW, or "N PRECEDING/FOLLOWING".
func (p *Parser) parseFrameBound() (FrameBound, error) {
	if identIs(p.cur(), "unbounded") {
		p.advance()
		if identIs(p.cur(), "preceding") {
			p.advance()
			return FrameBound{Kind: FrameUnboundedPreceding}, nil
		}
		if identIs(p.cur(), "following") {
			p.advance()
			return FrameBound{Kind: FrameUnboundedFollowing}, nil
		}
		return FrameBound{}, p.errf(p.cur(), "expected PRECEDING or FOLLOWING after UNBOUNDED")
	}
	if identIs(p.cur(), "current") {
		p.advance()
		if !identIs(p.cur(), "row") {
			return FrameBound{}, p.errf(p.cur(), "expected ROW after CURRENT")
		}
		p.advance()
		return FrameBound{Kind: FrameCurrentRow}, nil
	}
	// N PRECEDING / N FOLLOWING
	off, err := p.parseAdditive()
	if err != nil {
		return FrameBound{}, err
	}
	if identIs(p.cur(), "preceding") {
		p.advance()
		return FrameBound{Kind: FramePreceding, Offset: off}, nil
	}
	if identIs(p.cur(), "following") {
		p.advance()
		return FrameBound{Kind: FrameFollowing, Offset: off}, nil
	}
	return FrameBound{}, p.errf(p.cur(), "expected PRECEDING or FOLLOWING in frame bound")
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
	// Strip a string-literal prefix (E'...', B'...', X'...').
	if len(raw) >= 2 && isStringPrefix(raw[0]) && raw[1] == '\'' {
		raw = raw[1:]
	}
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
