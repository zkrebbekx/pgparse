package pgparse

import (
	"fmt"
	"strconv"
	"strings"
)

// Parser turns a token stream into an AST. One Parser handles one input; reuse
// is not supported (cheap to allocate).
type Parser struct {
	src       string
	toks      []Token
	pos       int
	stmtStart int // token index where the current statement began
}

// newParser constructs a Parser over an already-lexed token slice.
func newParser(src string, toks []Token) *Parser { return &Parser{src: src, toks: toks} }

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
	p.stmtStart = p.pos
	var with []*CTE
	if p.isKw(kwWith) {
		var err error
		with, err = p.parseWith()
		if err != nil {
			return nil, err
		}
	}
	switch {
	case p.isKw(kwSelect), p.isKw(kwValues), p.isWord("table"), p.cur().Type == TokenLParen:
		// A statement may be a SELECT, a bare VALUES list, the "TABLE name"
		// shorthand, or a parenthesised set-operation select.
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
	case p.isKw(kwCreate):
		return p.parseCreate()
	case p.isKw(kwAlter):
		return p.ddlOrRaw(p.parseAlter)
	case p.isKw(kwDrop):
		return p.ddlOrRaw(p.parseDrop)
	}
	if isUtilityStart(p.cur()) {
		return p.parseRawStmt()
	}
	return nil, p.errf(p.cur(), "expected a statement (SELECT/INSERT/UPDATE/DELETE/CREATE/ALTER/DROP)")
}

// parseRawStmt consumes a recognised but unmodelled statement up to (not
// including) the top-level statement terminator, validating that delimiters are
// balanced, and preserves the verbatim SQL.
func (p *Parser) parseRawStmt() (Stmt, error) {
	startTok := p.toks[p.stmtStart]
	kw := strings.ToUpper(startTok.Val)
	depth := 0
	for !p.atEOF() {
		t := p.cur()
		if depth == 0 && t.Type == TokenSemicolon {
			break
		}
		switch t.Type {
		case TokenLParen, TokenLBracket:
			depth++
		case TokenRParen, TokenRBracket:
			if depth > 0 {
				depth--
			}
		}
		p.advance()
	}
	end := len(p.src)
	if !p.atEOF() {
		end = p.cur().Pos
	}
	sql := strings.TrimSpace(p.src[startTok.Pos:end])
	return &RawStmt{Keyword: kw, SQL: sql}, nil
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
		// Optional [NOT] MATERIALIZED hint.
		if !p.acceptWord("materialized") && p.acceptKw(kwNot) {
			if err := p.expectWord("materialized"); err != nil {
				return nil, err
			}
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
		// Optional SEARCH / CYCLE clauses on a recursive CTE — recognised and
		// consumed (not modelled).
		if err := p.parseSearchCycle(); err != nil {
			return nil, err
		}
		ctes = append(ctes, cte)
		if !p.acceptType(TokenComma) {
			break
		}
	}
	return ctes, nil
}

// parseSearchCycle consumes the optional SEARCH and CYCLE clauses of a recursive
// CTE. They are recognised but not modelled.
func (p *Parser) parseSearchCycle() error {
	identList := func() error {
		for {
			if _, err := p.parseIdent("column name"); err != nil {
				return err
			}
			if !p.acceptType(TokenComma) {
				return nil
			}
		}
	}
	if p.acceptWord("search") {
		if !p.acceptWord("depth") {
			p.acceptWord("breadth")
		}
		if err := p.expectWord("first"); err != nil {
			return err
		}
		if err := p.expectWord("by"); err != nil {
			return err
		}
		if err := identList(); err != nil {
			return err
		}
		if !p.acceptKw(kwSet) {
			return p.errf(p.cur(), "expected SET in SEARCH clause")
		}
		if _, err := p.parseIdent("sequence column"); err != nil {
			return err
		}
	}
	if p.acceptWord("cycle") {
		if err := identList(); err != nil {
			return err
		}
		if !p.acceptKw(kwSet) {
			return p.errf(p.cur(), "expected SET in CYCLE clause")
		}
		if _, err := p.parseIdent("cycle mark column"); err != nil {
			return err
		}
		// Optional TO value DEFAULT value.
		if p.acceptWord("to") {
			if _, err := p.parseExpr(); err != nil {
				return err
			}
			if p.acceptKw(kwDefault) {
				if _, err := p.parseExpr(); err != nil {
					return err
				}
			}
		}
		if p.acceptWord("using") {
			if _, err := p.parseIdent("path column"); err != nil {
				return err
			}
		}
	}
	return nil
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
	// A WITH clause may lead a select expression anywhere it appears (subqueries,
	// set-op operands, LATERAL items), not only at statement top level.
	var with []*CTE
	if p.isKw(kwWith) {
		var err error
		with, err = p.parseWith()
		if err != nil {
			return nil, err
		}
	}
	node, err := p.parseUnionExpr()
	if err != nil {
		return nil, err
	}
	if with != nil {
		node.With = with
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
	if p.isKw(kwValues) {
		p.advance()
		rows, err := p.parseValuesRows()
		if err != nil {
			return nil, err
		}
		return &SelectStmt{Values: rows}, nil
	}
	// "TABLE name" is shorthand for "SELECT * FROM name".
	if p.isWord("table") {
		p.advance()
		name, err := p.parseObjectName()
		if err != nil {
			return nil, err
		}
		return &SelectStmt{
			Columns: []SelectItem{{Star: true}},
			From:    []TableExpr{name},
		}, nil
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

	// SELECT ... INTO [TEMP|UNLOGGED] [TABLE] name.
	if p.acceptKw(kwInto) {
		p.acceptWord("temporary")
		p.acceptWord("temp")
		p.acceptWord("unlogged")
		p.acceptWord("table")
		s.Into, err = p.parseObjectName()
		if err != nil {
			return nil, err
		}
	}

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
		p.acceptKw(kwAll)
		p.acceptKw(kwDistinct)
		s.GroupBy, err = p.parseGroupList()
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
	// WINDOW name AS (spec) [, ...].
	if p.isWord("window") {
		p.advance()
		for {
			name, err := p.parseIdent("window name")
			if err != nil {
				return nil, err
			}
			if !p.acceptKw(kwAs) {
				return nil, p.errf(p.cur(), "expected AS in WINDOW clause")
			}
			wd, err := p.parseWindowSpec()
			if err != nil {
				return nil, err
			}
			wd.Name = name
			s.Window = append(s.Window, wd)
			if !p.acceptType(TokenComma) {
				break
			}
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
		// Optional ROW / ROWS noise word.
		if !p.acceptWord("rows") {
			p.acceptWord("row")
		}
	}
	// FETCH FIRST|NEXT [count] ROW|ROWS ONLY|WITH TIES.
	if identIs(p.cur(), "fetch") {
		p.advance()
		if !p.acceptWord("first") {
			p.acceptWord("next")
		}
		if !p.acceptWord("row") && !p.acceptWord("rows") {
			e, err := p.parseExpr()
			if err != nil {
				return err
			}
			s.Limit = e
			if !p.acceptWord("rows") {
				p.acceptWord("row")
			}
		}
		if !p.acceptWord("only") {
			if p.acceptKw(kwWith) {
				if err := p.expectWord("ties"); err != nil {
					return err
				}
			}
		}
	}
	// Row-level locking: FOR UPDATE|SHARE|... [OF t,...] [NOWAIT|SKIP LOCKED].
	for identIs(p.cur(), "for") {
		lc, err := p.parseLockClause()
		if err != nil {
			return err
		}
		s.Locking = append(s.Locking, lc)
	}
	return nil
}

func (p *Parser) parseLockClause() (LockClause, error) {
	p.advance() // FOR
	var lc LockClause
	switch {
	case p.isKw(kwUpdate):
		p.advance()
		lc.Strength = "UPDATE"
	case identIs(p.cur(), "share"):
		p.advance()
		lc.Strength = "SHARE"
	case identIs(p.cur(), "key"):
		p.advance()
		p.acceptWord("share")
		lc.Strength = "KEY SHARE"
	case identIs(p.cur(), "no"):
		p.advance()
		p.acceptWord("key")
		p.acceptKw(kwUpdate)
		lc.Strength = "NO KEY UPDATE"
	default:
		return lc, p.errf(p.cur(), "expected UPDATE/SHARE after FOR")
	}
	if p.acceptWord("of") {
		for {
			n, err := p.parseIdent("table name")
			if err != nil {
				return lc, err
			}
			lc.Of = append(lc.Of, n)
			if !p.acceptType(TokenComma) {
				break
			}
		}
	}
	if p.acceptWord("nowait") {
		lc.Wait = "NOWAIT"
	} else if p.acceptWord("skip") {
		if err := p.expectWord("locked"); err != nil {
			return lc, err
		}
		lc.Wait = "SKIP LOCKED"
	}
	return lc, nil
}

// parseGroupList parses GROUP BY elements, including ROLLUP/CUBE/GROUPING SETS
// and the empty grouping "()".
func (p *Parser) parseGroupList() ([]Expr, error) {
	var list []Expr
	for {
		e, err := p.parseGroupElem()
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

func (p *Parser) parseGroupElem() (Expr, error) {
	// Empty grouping set: ().
	if p.cur().Type == TokenLParen && p.peekAt(1).Type == TokenRParen {
		p.advance()
		p.advance()
		return &RowExpr{}, nil
	}
	var kind string
	switch {
	case identIs(p.cur(), "rollup"):
		kind = "ROLLUP"
	case identIs(p.cur(), "cube"):
		kind = "CUBE"
	case identIs(p.cur(), "grouping") && identIs(p.peekAt(1), "sets"):
		kind = "GROUPING SETS"
	}
	if kind == "" {
		return p.parseExpr()
	}
	p.advance() // ROLLUP/CUBE/GROUPING
	if kind == "GROUPING SETS" {
		p.advance() // SETS
	}
	if _, err := p.expectType(TokenLParen, "'(' after grouping element"); err != nil {
		return nil, err
	}
	g := &GroupingExpr{Kind: kind}
	if p.cur().Type != TokenRParen {
		args, err := p.parseGroupList()
		if err != nil {
			return nil, err
		}
		g.Args = args
	}
	if _, err := p.expectType(TokenRParen, "')'"); err != nil {
		return nil, err
	}
	return g, nil
}

func (p *Parser) parseSelectList() ([]SelectItem, error) {
	// PostgreSQL allows an empty target list: SELECT FROM t.
	if p.isKw(kwFrom) || p.atEOF() || p.cur().Type == TokenSemicolon || p.cur().Type == TokenRParen {
		return nil, nil
	}
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
		natural := identIs(p.cur(), "natural")
		if natural {
			p.advance()
		}
		jk, ok := p.peekJoinKind()
		if !ok {
			if natural {
				return nil, p.errf(p.cur(), "expected JOIN after NATURAL")
			}
			return left, nil
		}
		right, on, using, err := p.parseJoinRHS(jk, natural)
		if err != nil {
			return nil, err
		}
		left = &JoinExpr{Kind: jk, Natural: natural, Left: left, Right: right, On: on, Using: using}
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

func (p *Parser) parseJoinRHS(jk JoinKind, natural bool) (TableExpr, Expr, []string, error) {
	right, err := p.parseTablePrimary()
	if err != nil {
		return nil, nil, nil, err
	}
	if jk == JoinCross || natural {
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
		if k := p.peekAt(1); k.Type == TokenKeyword && (k.Kw == kwSelect || k.Kw == kwWith || k.Kw == kwValues) {
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

	parts := []string{}
	name, err := p.parseIdent("table name")
	if err != nil {
		return nil, err
	}
	parts = append(parts, name)
	for p.acceptType(TokenDot) {
		n, err := p.parseIdent("table name")
		if err != nil {
			return nil, err
		}
		parts = append(parts, n)
	}

	// Set-returning function as a FROM item: func(args) [WITH ORDINALITY] [alias].
	if p.cur().Type == TokenLParen {
		fn, err := p.parseCallTail(parts)
		if err != nil {
			return nil, err
		}
		ft := &FuncTable{Func: fn, Lateral: lateral}
		if p.isWord("with") && identIs(p.peekAt(1), "ordinality") {
			p.advance()
			p.advance()
			ft.Ordinality = true
		}
		if alias, ok, err := p.parseTableAlias(); err != nil {
			return nil, err
		} else if ok {
			ft.Alias = alias
			if p.isColumnListAhead() {
				ft.Columns, err = p.parseNameList()
				if err != nil {
					return nil, err
				}
			}
		}
		return ft, nil
	}

	tn := &TableName{}
	switch len(parts) {
	case 1:
		tn.Name = parts[0]
	default:
		tn.Schema, tn.Name = parts[len(parts)-2], parts[len(parts)-1]
	}
	if alias, ok, err := p.parseTableAlias(); err != nil {
		return nil, err
	} else if ok {
		tn.Alias = alias
		if p.isColumnListAhead() {
			tn.ColumnAliases, err = p.parseNameList()
			if err != nil {
				return nil, err
			}
		}
	}
	return tn, nil
}

// nonAliasWords are non-reserved identifiers that introduce a following clause
// and therefore must not be swallowed as a table alias.
var nonAliasWords = map[string]bool{
	"for": true, "natural": true, "fetch": true, "tablesample": true,
	"window": true,
}

func nonAliasWord(t Token) bool {
	return t.Type == TokenIdent && nonAliasWords[strings.ToLower(t.Val)]
}

// parseTableAlias parses [AS] alias for a table, refusing clause keywords.
func (p *Parser) parseTableAlias() (string, bool, error) {
	if p.acceptKw(kwAs) {
		name, err := p.parseIdent("table alias")
		return name, err == nil, err
	}
	if p.cur().Type == TokenIdent && !nonAliasWord(p.cur()) {
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
		} else if p.acceptKw(kwUsing) {
			op := p.cur()
			if op.Type != TokenOp && compOps[op.Type] == "" {
				return nil, p.errf(op, "expected an operator after USING")
			}
			it.UsingOp = p.advance().Val
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
