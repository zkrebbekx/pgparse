package pgparse

import (
	"fmt"
	"strconv"
	"strings"
)

// Parser turns a token stream into an AST. One Parser handles one input; reuse
// is not supported (cheap to allocate).
type Parser struct {
	toks []Token
	pos  int
}

// newParser constructs a Parser over an already-lexed token slice.
func newParser(toks []Token) *Parser { return &Parser{toks: toks} }

func (p *Parser) cur() Token  { return p.toks[p.pos] }
func (p *Parser) peek() Token { return p.toks[p.pos] }

func (p *Parser) peekAt(n int) Token {
	i := p.pos + n
	if i >= len(p.toks) {
		return p.toks[len(p.toks)-1] // EOF
	}
	return p.toks[i]
}

func (p *Parser) advance() Token {
	t := p.toks[p.pos]
	if p.pos < len(p.toks)-1 {
		p.pos++
	}
	return t
}

func (p *Parser) atEOF() bool { return p.cur().Type == TokenEOF }

func (p *Parser) isKw(kw Keyword) bool {
	t := p.cur()
	return t.Type == TokenKeyword && t.Kw == kw
}

func (p *Parser) acceptKw(kw Keyword) bool {
	if p.isKw(kw) {
		p.advance()
		return true
	}
	return false
}

func (p *Parser) acceptType(tt TokenType) bool {
	if p.cur().Type == tt {
		p.advance()
		return true
	}
	return false
}

func (p *Parser) expectType(tt TokenType, what string) (Token, error) {
	t := p.cur()
	if t.Type != tt {
		return t, p.errf(t, "expected %s", what)
	}
	p.advance()
	return t, nil
}

func (p *Parser) errf(t Token, format string, args ...interface{}) error {
	return &SyntaxError{Pos: t.Pos, Msg: sprintf(format, args...) + " near " + describe(t)}
}

// ---------------------------------------------------------------------------
// Statement dispatch
// ---------------------------------------------------------------------------

// parseStatements reads semicolon-separated statements until EOF.
func (p *Parser) parseStatements() ([]Stmt, error) {
	var stmts []Stmt
	for !p.atEOF() {
		if p.acceptType(TokenSemicolon) {
			continue
		}
		s, err := p.parseStatement()
		if err != nil {
			return stmts, err
		}
		stmts = append(stmts, s)
		if !p.atEOF() {
			if _, err := p.expectType(TokenSemicolon, "';' between statements"); err != nil {
				return stmts, err
			}
		}
	}
	return stmts, nil
}

func (p *Parser) parseStatement() (Stmt, error) {
	var with []*CTE
	if p.isKw(kwWith) {
		var err error
		with, err = p.parseWith()
		if err != nil {
			return nil, err
		}
	}
	switch {
	case p.isKw(kwSelect):
		s, err := p.parseSelect()
		if err != nil {
			return nil, err
		}
		s.With = with
		return s, nil
	case p.isKw(kwInsert):
		return p.parseInsert(with)
	case p.isKw(kwUpdate):
		return p.parseUpdate(with)
	case p.isKw(kwDelete):
		return p.parseDelete(with)
	}
	return nil, p.errf(p.cur(), "expected a statement (SELECT/INSERT/UPDATE/DELETE)")
}

// parseWith parses a WITH [RECURSIVE] cte [, cte]* clause.
func (p *Parser) parseWith() ([]*CTE, error) {
	p.advance() // WITH
	recursive := p.acceptKw(kwRecursive)
	var ctes []*CTE
	for {
		name, err := p.parseIdent("CTE name")
		if err != nil {
			return nil, err
		}
		cte := &CTE{Name: name, Recursive: recursive}
		if p.cur().Type == TokenLParen && p.isColumnListAhead() {
			cte.Columns, err = p.parseNameList()
			if err != nil {
				return nil, err
			}
		}
		if !p.acceptKw(kwAs) {
			return nil, p.errf(p.cur(), "expected AS in WITH")
		}
		if _, err := p.expectType(TokenLParen, "'(' before CTE body"); err != nil {
			return nil, err
		}
		body, err := p.parseStatement()
		if err != nil {
			return nil, err
		}
		cte.Stmt = body
		if _, err := p.expectType(TokenRParen, "')' after CTE body"); err != nil {
			return nil, err
		}
		ctes = append(ctes, cte)
		if !p.acceptType(TokenComma) {
			break
		}
	}
	return ctes, nil
}

// isColumnListAhead reports whether "( ident [, ident]* )" starts here, used to
// disambiguate an optional CTE/insert column list from a subquery.
func (p *Parser) isColumnListAhead() bool {
	if p.cur().Type != TokenLParen {
		return false
	}
	return p.peekAt(1).Type == TokenIdent
}

// ---------------------------------------------------------------------------
// SELECT
// ---------------------------------------------------------------------------

// parseSelect parses a full select expression: set operations with correct
// precedence (INTERSECT binds tighter than UNION/EXCEPT) followed by a single
// trailing ORDER BY / LIMIT / OFFSET that applies to the whole expression.
func (p *Parser) parseSelect() (*SelectStmt, error) {
	node, err := p.parseUnionExpr()
	if err != nil {
		return nil, err
	}
	// The tail binds to the entire (possibly set-op) expression, not to the
	// last operand, matching PostgreSQL.
	if err := p.parseTail(node); err != nil {
		return nil, err
	}
	return node, nil
}

// parseUnionExpr handles the lowest-precedence set operators, UNION and EXCEPT,
// left-associatively.
func (p *Parser) parseUnionExpr() (*SelectStmt, error) {
	left, err := p.parseIntersectExpr()
	if err != nil {
		return nil, err
	}
	for {
		var op SetOpKind
		switch {
		case p.isKw(kwUnion):
			op = SetOpUnion
		case p.isKw(kwExcept):
			op = SetOpExcept
		default:
			return left, nil
		}
		p.advance()
		all := p.acceptKw(kwAll)
		if !all {
			p.acceptKw(kwDistinct)
		}
		right, err := p.parseIntersectExpr()
		if err != nil {
			return nil, err
		}
		left = &SelectStmt{SetOp: op, SetAll: all, Left: left, Right: right}
	}
}

// parseIntersectExpr handles INTERSECT, which binds tighter than UNION/EXCEPT.
func (p *Parser) parseIntersectExpr() (*SelectStmt, error) {
	left, err := p.parseSelectPrimary()
	if err != nil {
		return nil, err
	}
	for p.isKw(kwIntersect) {
		p.advance()
		all := p.acceptKw(kwAll)
		if !all {
			p.acceptKw(kwDistinct)
		}
		right, err := p.parseSelectPrimary()
		if err != nil {
			return nil, err
		}
		left = &SelectStmt{SetOp: SetOpIntersect, SetAll: all, Left: left, Right: right}
	}
	return left, nil
}

// parseSelectPrimary parses one set-operation operand: either a parenthesised
// select expression (which may carry its own tail inside the parens) or a bare
// SELECT body.
func (p *Parser) parseSelectPrimary() (*SelectStmt, error) {
	if p.cur().Type == TokenLParen {
		p.advance()
		inner, err := p.parseSelect()
		if err != nil {
			return nil, err
		}
		if _, err := p.expectType(TokenRParen, "')'"); err != nil {
			return nil, err
		}
		return inner, nil
	}
	return p.parseSelectBody()
}

// parseSelectBody parses a single SELECT ... block up to but excluding the
// trailing ORDER BY / LIMIT / OFFSET, which the caller attaches.
func (p *Parser) parseSelectBody() (*SelectStmt, error) {
	if !p.acceptKw(kwSelect) {
		return nil, p.errf(p.cur(), "expected SELECT")
	}
	s := &SelectStmt{}
	if p.acceptKw(kwDistinct) {
		s.Distinct = true
		if p.acceptKw(kwOn) {
			if _, err := p.expectType(TokenLParen, "'(' after DISTINCT ON"); err != nil {
				return nil, err
			}
			on, err := p.parseExprList()
			if err != nil {
				return nil, err
			}
			s.DistinctOn = on
			if _, err := p.expectType(TokenRParen, "')'"); err != nil {
				return nil, err
			}
		}
	} else {
		p.acceptKw(kwAll)
	}

	cols, err := p.parseSelectList()
	if err != nil {
		return nil, err
	}
	s.Columns = cols

	if p.acceptKw(kwFrom) {
		from, err := p.parseFromList()
		if err != nil {
			return nil, err
		}
		s.From = from
	}
	if p.acceptKw(kwWhere) {
		s.Where, err = p.parseExpr()
		if err != nil {
			return nil, err
		}
	}
	if p.isKw(kwGroup) {
		p.advance()
		if !p.acceptKw(kwBy) {
			return nil, p.errf(p.cur(), "expected BY after GROUP")
		}
		s.GroupBy, err = p.parseExprList()
		if err != nil {
			return nil, err
		}
	}
	if p.acceptKw(kwHaving) {
		s.Having, err = p.parseExpr()
		if err != nil {
			return nil, err
		}
	}
	return s, nil
}

// parseTail parses the trailing ORDER BY / LIMIT / OFFSET shared by selects.
func (p *Parser) parseTail(s *SelectStmt) error {
	if p.isKw(kwOrder) {
		p.advance()
		if !p.acceptKw(kwBy) {
			return p.errf(p.cur(), "expected BY after ORDER")
		}
		ord, err := p.parseOrderList()
		if err != nil {
			return err
		}
		s.OrderBy = ord
	}
	if p.acceptKw(kwLimit) {
		if p.acceptKw(kwAll) {
			// LIMIT ALL == no limit
		} else {
			e, err := p.parseExpr()
			if err != nil {
				return err
			}
			s.Limit = e
		}
	}
	if p.acceptKw(kwOffset) {
		e, err := p.parseExpr()
		if err != nil {
			return err
		}
		s.Offset = e
	}
	return nil
}

func (p *Parser) parseSelectList() ([]SelectItem, error) {
	var items []SelectItem
	for {
		var it SelectItem
		if p.cur().Type == TokenStar {
			p.advance()
			it.Star = true
		} else {
			e, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			// table.* lands here as a ColumnRef ending in "*"
			it.Expr = e
			if alias, ok, err := p.parseOptionalAlias(); err != nil {
				return nil, err
			} else if ok {
				it.Alias = alias
			}
		}
		items = append(items, it)
		if !p.acceptType(TokenComma) {
			break
		}
	}
	return items, nil
}

// parseOptionalAlias parses an optional output alias: [AS] name.
func (p *Parser) parseOptionalAlias() (string, bool, error) {
	if p.acceptKw(kwAs) {
		name, err := p.parseIdent("alias")
		return name, err == nil, err
	}
	// bare identifier that is not a reserved clause keyword acts as an alias
	if p.cur().Type == TokenIdent {
		return identText(p.advance()), true, nil
	}
	return "", false, nil
}

// ---------------------------------------------------------------------------
// FROM
// ---------------------------------------------------------------------------

func (p *Parser) parseFromList() ([]TableExpr, error) {
	var items []TableExpr
	for {
		te, err := p.parseTableExpr()
		if err != nil {
			return nil, err
		}
		items = append(items, te)
		if !p.acceptType(TokenComma) {
			break
		}
	}
	return items, nil
}

// parseTableExpr parses a table primary followed by any join chain.
func (p *Parser) parseTableExpr() (TableExpr, error) {
	left, err := p.parseTablePrimary()
	if err != nil {
		return nil, err
	}
	for {
		jk, ok := p.peekJoinKind()
		if !ok {
			return left, nil
		}
		right, on, using, err := p.parseJoinRHS(jk)
		if err != nil {
			return nil, err
		}
		left = &JoinExpr{Kind: jk, Left: left, Right: right, On: on, Using: using}
	}
}

// peekJoinKind detects and consumes a join keyword prefix, returning its kind.
func (p *Parser) peekJoinKind() (JoinKind, bool) {
	switch {
	case p.isKw(kwJoin):
		p.advance()
		return JoinInner, true
	case p.isKw(kwInner):
		p.advance()
		p.acceptKw(kwJoin)
		return JoinInner, true
	case p.isKw(kwCross):
		p.advance()
		p.acceptKw(kwJoin)
		return JoinCross, true
	case p.isKw(kwLeft):
		p.advance()
		p.acceptKw(kwOuter)
		p.acceptKw(kwJoin)
		return JoinLeft, true
	case p.isKw(kwRight):
		p.advance()
		p.acceptKw(kwOuter)
		p.acceptKw(kwJoin)
		return JoinRight, true
	case p.isKw(kwFull):
		p.advance()
		p.acceptKw(kwOuter)
		p.acceptKw(kwJoin)
		return JoinFull, true
	}
	return 0, false
}

func (p *Parser) parseJoinRHS(jk JoinKind) (TableExpr, Expr, []string, error) {
	right, err := p.parseTablePrimary()
	if err != nil {
		return nil, nil, nil, err
	}
	if jk == JoinCross {
		return right, nil, nil, nil
	}
	if p.acceptKw(kwOn) {
		on, err := p.parseExpr()
		if err != nil {
			return nil, nil, nil, err
		}
		return right, on, nil, nil
	}
	if p.acceptKw(kwUsing) {
		cols, err := p.parseNameList()
		if err != nil {
			return nil, nil, nil, err
		}
		return right, nil, cols, nil
	}
	return nil, nil, nil, p.errf(p.cur(), "expected ON or USING in join")
}

// parseTablePrimary parses a single relation, subquery, or parenthesised join.
func (p *Parser) parseTablePrimary() (TableExpr, error) {
	// Optional LATERAL prefix before a subquery (LATERAL is non-reserved).
	lateral := false
	if identIs(p.cur(), "lateral") {
		p.advance()
		lateral = true
	}
	if p.cur().Type == TokenLParen {
		// Could be a subquery or a parenthesised join.
		if p.peekAt(1).Type == TokenKeyword && (p.peekAt(1).Kw == kwSelect || p.peekAt(1).Kw == kwWith) {
			p.advance()
			sub, err := p.parseSelect()
			if err != nil {
				return nil, err
			}
			if _, err := p.expectType(TokenRParen, "')'"); err != nil {
				return nil, err
			}
			st := &SubqueryTable{Select: sub, Lateral: lateral}
			p.acceptKw(kwAs)
			if p.cur().Type == TokenIdent {
				st.Alias = identText(p.advance())
				if p.isColumnListAhead() {
					st.Columns, err = p.parseNameList()
					if err != nil {
						return nil, err
					}
				}
			}
			return st, nil
		}
		p.advance()
		inner, err := p.parseTableExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expectType(TokenRParen, "')'"); err != nil {
			return nil, err
		}
		return inner, nil
	}

	tn := &TableName{}
	name, err := p.parseIdent("table name")
	if err != nil {
		return nil, err
	}
	if p.acceptType(TokenDot) {
		tn.Schema = name
		tn.Name, err = p.parseIdent("table name")
		if err != nil {
			return nil, err
		}
	} else {
		tn.Name = name
	}
	if alias, ok, err := p.parseTableAlias(); err != nil {
		return nil, err
	} else if ok {
		tn.Alias = alias
	}
	return tn, nil
}

// parseTableAlias parses [AS] alias for a table, refusing clause keywords.
func (p *Parser) parseTableAlias() (string, bool, error) {
	if p.acceptKw(kwAs) {
		name, err := p.parseIdent("table alias")
		return name, err == nil, err
	}
	if p.cur().Type == TokenIdent {
		return identText(p.advance()), true, nil
	}
	return "", false, nil
}

// ---------------------------------------------------------------------------
// ORDER BY / name lists
// ---------------------------------------------------------------------------

func (p *Parser) parseOrderList() ([]OrderItem, error) {
	var items []OrderItem
	for {
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		it := OrderItem{Expr: e}
		if p.acceptKw(kwAsc) {
			it.Desc = false
		} else if p.acceptKw(kwDesc) {
			it.Desc = true
		}
		if p.acceptKw(kwNulls) {
			it.NullsSet = true
			if p.acceptKw(kwFirst) {
				it.NullsFirst = true
			} else if !p.acceptKw(kwLast) {
				return nil, p.errf(p.cur(), "expected FIRST or LAST after NULLS")
			}
		}
		items = append(items, it)
		if !p.acceptType(TokenComma) {
			break
		}
	}
	return items, nil
}

func (p *Parser) parseNameList() ([]string, error) {
	if _, err := p.expectType(TokenLParen, "'('"); err != nil {
		return nil, err
	}
	var names []string
	for {
		n, err := p.parseIdent("column name")
		if err != nil {
			return nil, err
		}
		names = append(names, n)
		if !p.acceptType(TokenComma) {
			break
		}
	}
	if _, err := p.expectType(TokenRParen, "')'"); err != nil {
		return nil, err
	}
	return names, nil
}

func (p *Parser) parseExprList() ([]Expr, error) {
	var list []Expr
	for {
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		list = append(list, e)
		if !p.acceptType(TokenComma) {
			break
		}
	}
	return list, nil
}

// parseIdent reads one identifier (quoted or bare). Quoted identifiers keep
// their original case; bare identifiers are returned verbatim.
func (p *Parser) parseIdent(what string) (string, error) {
	t := p.cur()
	if t.Type != TokenIdent {
		return "", p.errf(t, "expected %s", what)
	}
	p.advance()
	return identText(t), nil
}

// identText strips surrounding double quotes from a quoted identifier.
func identText(t Token) string {
	v := t.Val
	if len(v) >= 2 && v[0] == '"' {
		return strings.ReplaceAll(v[1:len(v)-1], `""`, `"`)
	}
	return v
}

func sprintf(format string, args ...interface{}) string {
	if len(args) == 0 {
		return format
	}
	return fmt.Sprintf(format, args...)
}

func describe(t Token) string {
	if t.Type == TokenEOF {
		return "end of input"
	}
	return strconv.Quote(t.Val)
}
