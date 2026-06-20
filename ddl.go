package pgparse

import "strings"

// ---------------------------------------------------------------------------
// Word helpers
//
// DDL grammar uses many words that are non-reserved in PostgreSQL (TABLE, VIEW,
// COLUMN, CONSTRAINT, PRIMARY, KEY, ...). Keeping them out of the keyword table
// preserves their use as ordinary identifiers in DML; inside DDL they are
// matched positionally by text instead.
// ---------------------------------------------------------------------------

func (p *Parser) isWord(s string) bool {
	t := p.cur()
	return (t.Type == TokenIdent || t.Type == TokenKeyword) && strings.EqualFold(t.Val, s)
}

func (p *Parser) acceptWord(s string) bool {
	if p.isWord(s) {
		p.advance()
		return true
	}
	return false
}

func (p *Parser) expectWord(s string) error {
	if !p.acceptWord(s) {
		return p.errf(p.cur(), "expected %q", s)
	}
	return nil
}

// utilityWords are leading keywords of utility and administrative statements
// that pgparse recognises (and validates the extent of) but does not model
// structurally; they are parsed into a RawStmt.
var utilityWords = map[string]bool{
	"analyze": true, "analyse": true, "vacuum": true, "explain": true,
	"set": true, "reset": true, "show": true, "copy": true, "comment": true,
	"grant": true, "revoke": true, "begin": true, "start": true, "commit": true,
	"end": true, "rollback": true, "abort": true, "savepoint": true,
	"release": true, "truncate": true, "cluster": true, "reindex": true,
	"prepare": true, "execute": true, "deallocate": true, "declare": true,
	"fetch": true, "move": true, "close": true, "lock": true, "do": true,
	"call": true, "listen": true, "notify": true, "unlisten": true,
	"discard": true, "refresh": true, "checkpoint": true, "load": true,
	"reassign": true, "import": true, "security": true, "merge": true,
}

func isUtilityStart(t Token) bool {
	if t.Type != TokenIdent && t.Type != TokenKeyword {
		return false
	}
	return utilityWords[strings.ToLower(t.Val)]
}

// parseObjectName parses an optionally schema-qualified object name (no alias).
func (p *Parser) parseObjectName() (*TableName, error) {
	name, err := p.parseIdent("name")
	if err != nil {
		return nil, err
	}
	tn := &TableName{Name: name}
	if p.acceptType(TokenDot) {
		tn.Schema = name
		tn.Name, err = p.parseIdent("name")
		if err != nil {
			return nil, err
		}
	}
	return tn, nil
}

// parseIfNotExists consumes an optional "IF NOT EXISTS".
func (p *Parser) parseIfNotExists() (bool, error) {
	if !p.acceptWord("if") {
		return false, nil
	}
	if !p.acceptKw(kwNot) {
		return false, p.errf(p.cur(), "expected NOT after IF")
	}
	if !p.acceptKw(kwExists) {
		return false, p.errf(p.cur(), "expected EXISTS after IF NOT")
	}
	return true, nil
}

// ---------------------------------------------------------------------------
// CREATE
// ---------------------------------------------------------------------------

func (p *Parser) parseCreate() (Stmt, error) {
	p.advance() // CREATE
	orReplace := false
	if p.acceptKw(kwOr) {
		if err := p.expectWord("replace"); err != nil {
			return nil, err
		}
		orReplace = true
	}
	temp := p.acceptWord("temporary") || p.acceptWord("temp")
	unique := p.acceptWord("unique")

	switch {
	case p.acceptWord("table"):
		return p.ddlOrRaw(func() (Stmt, error) { return p.parseCreateTable(temp) })
	case p.acceptWord("view"):
		return p.ddlOrRaw(func() (Stmt, error) { return p.parseCreateView(orReplace, temp) })
	case p.acceptWord("index"):
		return p.ddlOrRaw(func() (Stmt, error) { return p.parseCreateIndex(unique) })
	}
	// Other CREATE forms (TYPE, SEQUENCE, SCHEMA, FUNCTION, …) are recognised but
	// not modelled.
	p.pos = p.stmtStart
	return p.parseRawStmt()
}

// ddlOrRaw runs a structured DDL parser; if it fails on a tail pgparse does not
// model (partitioning, storage options, inheritance, …), it rewinds and accepts
// the statement as a RawStmt. This best-effort fallback applies only to DDL —
// DML and query parsing report errors normally.
func (p *Parser) ddlOrRaw(parse func() (Stmt, error)) (Stmt, error) {
	start := p.stmtStart
	stmt, err := parse()
	// Fall back when the structured parse failed, or succeeded but left trailing
	// tokens before the statement boundary (an unmodelled tail such as INHERITS,
	// WITH (...) storage options, TABLESPACE, or PARTITION BY).
	if err != nil || !p.atStmtEnd() {
		p.pos = start
		return p.parseRawStmt()
	}
	return stmt, nil
}

// atStmtEnd reports whether the parser is at a statement boundary.
func (p *Parser) atStmtEnd() bool {
	return p.atEOF() || p.cur().Type == TokenSemicolon
}

func (p *Parser) parseCreateTable(temp bool) (Stmt, error) {
	ine, err := p.parseIfNotExists()
	if err != nil {
		return nil, err
	}
	name, err := p.parseObjectName()
	if err != nil {
		return nil, err
	}
	ct := &CreateTableStmt{Table: name, Temporary: temp, IfNotExists: ine}
	if _, err := p.expectType(TokenLParen, "'(' to begin table definition"); err != nil {
		return nil, err
	}
	if p.cur().Type != TokenRParen {
		for {
			if p.isTableConstraintStart() {
				tc, err := p.parseTableConstraint()
				if err != nil {
					return nil, err
				}
				ct.Constraints = append(ct.Constraints, tc)
			} else {
				col, err := p.parseColumnDef()
				if err != nil {
					return nil, err
				}
				ct.Columns = append(ct.Columns, col)
			}
			if !p.acceptType(TokenComma) {
				break
			}
		}
	}
	if _, err := p.expectType(TokenRParen, "')' to end table definition"); err != nil {
		return nil, err
	}
	return ct, nil
}

func (p *Parser) parseColumnDef() (ColumnDef, error) {
	name, err := p.parseIdent("column name")
	if err != nil {
		return ColumnDef{}, err
	}
	typ, err := p.parseTypeName()
	if err != nil {
		return ColumnDef{}, err
	}
	col := ColumnDef{Name: name, Type: typ}
	for p.isColumnConstraintStart() {
		c, err := p.parseColumnConstraint()
		if err != nil {
			return ColumnDef{}, err
		}
		col.Constraints = append(col.Constraints, c)
	}
	return col, nil
}

func (p *Parser) isColumnConstraintStart() bool {
	switch {
	case p.isWord("constraint"), p.isKw(kwNot), p.isKw(kwNull), p.isWord("primary"),
		p.isWord("unique"), p.isKw(kwDefault), p.isWord("check"), p.isWord("references"):
		return true
	}
	return false
}

func (p *Parser) parseColumnConstraint() (ColumnConstraint, error) {
	c := ColumnConstraint{}
	if p.acceptWord("constraint") {
		n, err := p.parseIdent("constraint name")
		if err != nil {
			return c, err
		}
		c.Name = n
	}
	switch {
	case p.isKw(kwNot):
		p.advance()
		if !p.acceptKw(kwNull) {
			return c, p.errf(p.cur(), "expected NULL after NOT")
		}
		c.Kind = ColConstraintNotNull
	case p.acceptKw(kwNull):
		c.Kind = ColConstraintNull
	case p.acceptWord("primary"):
		if err := p.expectWord("key"); err != nil {
			return c, err
		}
		c.Kind = ColConstraintPrimaryKey
	case p.acceptWord("unique"):
		c.Kind = ColConstraintUnique
	case p.acceptKw(kwDefault):
		e, err := p.parseExpr()
		if err != nil {
			return c, err
		}
		c.Kind, c.Default = ColConstraintDefault, e
	case p.acceptWord("check"):
		if _, err := p.expectType(TokenLParen, "'(' after CHECK"); err != nil {
			return c, err
		}
		e, err := p.parseExpr()
		if err != nil {
			return c, err
		}
		if _, err := p.expectType(TokenRParen, "')'"); err != nil {
			return c, err
		}
		c.Kind, c.Check = ColConstraintCheck, e
	case p.acceptWord("references"):
		ref, cols, err := p.parseReferences()
		if err != nil {
			return c, err
		}
		c.Kind, c.RefTable, c.RefColumns = ColConstraintReferences, ref, cols
	default:
		return c, p.errf(p.cur(), "unexpected token in column constraint")
	}
	return c, nil
}

// parseReferences parses "table [(cols)]" after the REFERENCES keyword.
func (p *Parser) parseReferences() (*TableName, []string, error) {
	ref, err := p.parseObjectName()
	if err != nil {
		return nil, nil, err
	}
	var cols []string
	if p.cur().Type == TokenLParen {
		cols, err = p.parseNameList()
		if err != nil {
			return nil, nil, err
		}
	}
	return ref, cols, nil
}

func (p *Parser) isTableConstraintStart() bool {
	return p.isWord("constraint") || p.isWord("primary") || p.isWord("unique") ||
		p.isWord("check") || p.isWord("foreign")
}

func (p *Parser) parseTableConstraint() (TableConstraint, error) {
	tc := TableConstraint{}
	if p.acceptWord("constraint") {
		n, err := p.parseIdent("constraint name")
		if err != nil {
			return tc, err
		}
		tc.Name = n
	}
	switch {
	case p.acceptWord("primary"):
		if err := p.expectWord("key"); err != nil {
			return tc, err
		}
		cols, err := p.parseNameList()
		if err != nil {
			return tc, err
		}
		tc.Kind, tc.Columns = TableConstraintPrimaryKey, cols
	case p.acceptWord("unique"):
		cols, err := p.parseNameList()
		if err != nil {
			return tc, err
		}
		tc.Kind, tc.Columns = TableConstraintUnique, cols
	case p.acceptWord("check"):
		if _, err := p.expectType(TokenLParen, "'(' after CHECK"); err != nil {
			return tc, err
		}
		e, err := p.parseExpr()
		if err != nil {
			return tc, err
		}
		if _, err := p.expectType(TokenRParen, "')'"); err != nil {
			return tc, err
		}
		tc.Kind, tc.Check = TableConstraintCheck, e
	case p.acceptWord("foreign"):
		if err := p.expectWord("key"); err != nil {
			return tc, err
		}
		cols, err := p.parseNameList()
		if err != nil {
			return tc, err
		}
		if err := p.expectWord("references"); err != nil {
			return tc, err
		}
		ref, refCols, err := p.parseReferences()
		if err != nil {
			return tc, err
		}
		tc.Kind, tc.Columns, tc.RefTable, tc.RefColumns =
			TableConstraintForeignKey, cols, ref, refCols
	default:
		return tc, p.errf(p.cur(), "unexpected token in table constraint")
	}
	return tc, nil
}

func (p *Parser) parseCreateView(orReplace, temp bool) (Stmt, error) {
	name, err := p.parseObjectName()
	if err != nil {
		return nil, err
	}
	v := &CreateViewStmt{Name: name, OrReplace: orReplace, Temporary: temp}
	if p.cur().Type == TokenLParen {
		v.Columns, err = p.parseNameList()
		if err != nil {
			return nil, err
		}
	}
	if !p.acceptKw(kwAs) {
		return nil, p.errf(p.cur(), "expected AS in CREATE VIEW")
	}
	sel, err := p.parseSelect()
	if err != nil {
		return nil, err
	}
	v.Select = sel
	return v, nil
}

func (p *Parser) parseCreateIndex(unique bool) (Stmt, error) {
	p.acceptWord("concurrently")
	ine, err := p.parseIfNotExists()
	if err != nil {
		return nil, err
	}
	idx := &CreateIndexStmt{Unique: unique, IfNotExists: ine}
	// Optional index name before ON.
	if p.cur().Type == TokenIdent {
		idx.Name = identText(p.advance())
	}
	if !p.acceptKw(kwOn) {
		return nil, p.errf(p.cur(), "expected ON in CREATE INDEX")
	}
	idx.Table, err = p.parseObjectName()
	if err != nil {
		return nil, err
	}
	if p.acceptKw(kwUsing) {
		m, err := p.parseIdent("index method")
		if err != nil {
			return nil, err
		}
		idx.Using = m
	}
	if _, err := p.expectType(TokenLParen, "'(' for index columns"); err != nil {
		return nil, err
	}
	for {
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		el := IndexElem{Expr: e}
		if p.acceptKw(kwAsc) {
			el.Desc = false
		} else if p.acceptKw(kwDesc) {
			el.Desc = true
		}
		idx.Columns = append(idx.Columns, el)
		if !p.acceptType(TokenComma) {
			break
		}
	}
	if _, err := p.expectType(TokenRParen, "')'"); err != nil {
		return nil, err
	}
	if p.acceptKw(kwWhere) {
		idx.Where, err = p.parseExpr()
		if err != nil {
			return nil, err
		}
	}
	return idx, nil
}

// ---------------------------------------------------------------------------
// DROP
// ---------------------------------------------------------------------------

func (p *Parser) parseDrop() (Stmt, error) {
	p.advance() // DROP
	var obj string
	switch {
	case p.acceptWord("table"):
		obj = "TABLE"
	case p.acceptWord("view"):
		obj = "VIEW"
	case p.acceptWord("index"):
		obj = "INDEX"
	case p.acceptWord("sequence"):
		obj = "SEQUENCE"
	default:
		// DROP of other object kinds (ROLE, TYPE, FUNCTION, …) is recognised but
		// not modelled.
		p.pos = p.stmtStart
		return p.parseRawStmt()
	}
	d := &DropStmt{Object: obj}
	if p.acceptWord("if") {
		if !p.acceptKw(kwExists) {
			return nil, p.errf(p.cur(), "expected EXISTS after IF")
		}
		d.IfExists = true
	}
	for {
		n, err := p.parseObjectName()
		if err != nil {
			return nil, err
		}
		d.Names = append(d.Names, n)
		if !p.acceptType(TokenComma) {
			break
		}
	}
	if p.acceptWord("cascade") {
		d.Cascade = true
	} else {
		p.acceptWord("restrict")
	}
	return d, nil
}

// ---------------------------------------------------------------------------
// ALTER TABLE
// ---------------------------------------------------------------------------

func (p *Parser) parseAlter() (Stmt, error) {
	p.advance() // ALTER
	if !p.acceptWord("table") {
		// ALTER of other object kinds (INDEX, SEQUENCE, ROLE, …) is recognised
		// but not modelled.
		p.pos = p.stmtStart
		return p.parseRawStmt()
	}
	ife := false
	if p.acceptWord("if") {
		if !p.acceptKw(kwExists) {
			return nil, p.errf(p.cur(), "expected EXISTS after IF")
		}
		ife = true
	}
	name, err := p.parseObjectName()
	if err != nil {
		p.pos = p.stmtStart
		return p.parseRawStmt()
	}
	at := &AlterTableStmt{Table: name, IfExists: ife}
	for {
		a, err := p.parseAlterAction()
		if err != nil {
			// An action pgparse does not model — accept the whole statement raw.
			p.pos = p.stmtStart
			return p.parseRawStmt()
		}
		at.Actions = append(at.Actions, a)
		if !p.acceptType(TokenComma) {
			break
		}
	}
	return at, nil
}

func (p *Parser) parseAlterAction() (AlterAction, error) {
	switch {
	case p.acceptWord("add"):
		return p.parseAlterAdd()
	case p.isKw(kwDrop):
		p.advance()
		return p.parseAlterDrop()
	case p.isKw(kwAlter):
		p.advance()
		return p.parseAlterColumn()
	case p.acceptWord("rename"):
		return p.parseAlterRename()
	}
	return AlterAction{}, p.errf(p.cur(), "unsupported ALTER TABLE action")
}

func (p *Parser) parseAlterAdd() (AlterAction, error) {
	if p.isTableConstraintStart() {
		tc, err := p.parseTableConstraint()
		if err != nil {
			return AlterAction{}, err
		}
		return AlterAction{Kind: AlterAddConstraint, Constraint: &tc}, nil
	}
	p.acceptWord("column")
	col, err := p.parseColumnDef()
	if err != nil {
		return AlterAction{}, err
	}
	return AlterAction{Kind: AlterAddColumn, Column: col}, nil
}

func (p *Parser) parseAlterDrop() (AlterAction, error) {
	if p.acceptWord("constraint") {
		n, err := p.parseIdent("constraint name")
		if err != nil {
			return AlterAction{}, err
		}
		a := AlterAction{Kind: AlterDropConstraint, Name: n}
		a.Cascade = p.acceptWord("cascade")
		if !a.Cascade {
			p.acceptWord("restrict")
		}
		return a, nil
	}
	p.acceptWord("column")
	n, err := p.parseIdent("column name")
	if err != nil {
		return AlterAction{}, err
	}
	a := AlterAction{Kind: AlterDropColumn, Name: n}
	a.Cascade = p.acceptWord("cascade")
	if !a.Cascade {
		p.acceptWord("restrict")
	}
	return a, nil
}

func (p *Parser) parseAlterColumn() (AlterAction, error) {
	p.acceptWord("column")
	name, err := p.parseIdent("column name")
	if err != nil {
		return AlterAction{}, err
	}
	switch {
	case p.acceptWord("type"):
		typ, err := p.parseTypeName()
		if err != nil {
			return AlterAction{}, err
		}
		return AlterAction{Kind: AlterColumnType, Name: name, Type: typ}, nil
	case p.acceptKw(kwSet):
		if p.acceptKw(kwDefault) {
			e, err := p.parseExpr()
			if err != nil {
				return AlterAction{}, err
			}
			return AlterAction{Kind: AlterColumnSetDefault, Name: name, Default: e}, nil
		}
		if p.acceptKw(kwNot) {
			if !p.acceptKw(kwNull) {
				return AlterAction{}, p.errf(p.cur(), "expected NULL after NOT")
			}
			return AlterAction{Kind: AlterColumnSetNotNull, Name: name}, nil
		}
		return AlterAction{}, p.errf(p.cur(), "expected DEFAULT or NOT NULL after SET")
	case p.isKw(kwDrop):
		p.advance()
		if p.acceptKw(kwDefault) {
			return AlterAction{Kind: AlterColumnDropDefault, Name: name}, nil
		}
		if p.acceptKw(kwNot) {
			if !p.acceptKw(kwNull) {
				return AlterAction{}, p.errf(p.cur(), "expected NULL after NOT")
			}
			return AlterAction{Kind: AlterColumnDropNotNull, Name: name}, nil
		}
		return AlterAction{}, p.errf(p.cur(), "expected DEFAULT or NOT NULL after DROP")
	}
	return AlterAction{}, p.errf(p.cur(), "unsupported ALTER COLUMN action")
}

func (p *Parser) parseAlterRename() (AlterAction, error) {
	if p.acceptWord("to") {
		n, err := p.parseIdent("new table name")
		if err != nil {
			return AlterAction{}, err
		}
		return AlterAction{Kind: AlterRenameTable, NewName: n}, nil
	}
	p.acceptWord("column")
	from, err := p.parseIdent("column name")
	if err != nil {
		return AlterAction{}, err
	}
	if err := p.expectWord("to"); err != nil {
		return AlterAction{}, err
	}
	to, err := p.parseIdent("new column name")
	if err != nil {
		return AlterAction{}, err
	}
	return AlterAction{Kind: AlterRenameColumn, Name: from, NewName: to}, nil
}
