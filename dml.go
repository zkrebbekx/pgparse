package pgparse

// parseInsert parses INSERT INTO table [(cols)] VALUES ... | SELECT ...
// with optional ON CONFLICT and RETURNING.
func (p *parser) parseInsert(with []*CTE) (*InsertStmt, error) {
	p.advance() // INSERT
	if !p.acceptKw(kwInto) {
		return nil, p.errf(p.cur(), "expected INTO after INSERT")
	}
	tn, err := p.parseTableName()
	if err != nil {
		return nil, err
	}
	ins := &InsertStmt{With: with, Table: tn}

	if p.isColumnListAhead() {
		ins.Columns, err = p.parseColumnTargets()
		if err != nil {
			return nil, err
		}
	}

	switch {
	case p.isKw(kwDefault):
		p.advance()
		if !p.acceptKw(kwValues) {
			return nil, p.errf(p.cur(), "expected VALUES after DEFAULT")
		}
		ins.DefaultValues = true
	case p.acceptKw(kwValues):
		rows, err := p.parseValuesRows()
		if err != nil {
			return nil, err
		}
		ins.Rows = rows
	case p.isKw(kwSelect) || p.isKw(kwWith) || p.isKw(kwValues) || p.cur().Type == TokenLParen:
		sel, err := p.parseSelect()
		if err != nil {
			return nil, err
		}
		ins.Select = sel
	default:
		return nil, p.errf(p.cur(), "expected VALUES, SELECT, or DEFAULT VALUES in INSERT")
	}

	if p.isKw(kwOn) {
		oc, err := p.parseOnConflict()
		if err != nil {
			return nil, err
		}
		ins.OnConflict = oc
	}
	if p.acceptKw(kwReturning) {
		ret, err := p.parseSelectList()
		if err != nil {
			return nil, err
		}
		ins.Returning = ret
	}
	return ins, nil
}

func (p *parser) parseValuesRows() ([][]Expr, error) {
	var rows [][]Expr
	for {
		if _, err := p.expectType(TokenLParen, "'(' before VALUES row"); err != nil {
			return nil, err
		}
		var row []Expr
		for {
			if p.acceptKw(kwDefault) {
				row = append(row, &Literal{Kind: LitString, Val: "DEFAULT"})
			} else {
				e, err := p.parseExpr()
				if err != nil {
					return nil, err
				}
				row = append(row, e)
			}
			if !p.acceptType(TokenComma) {
				break
			}
		}
		if _, err := p.expectType(TokenRParen, "')' after VALUES row"); err != nil {
			return nil, err
		}
		rows = append(rows, row)
		if !p.acceptType(TokenComma) {
			break
		}
	}
	return rows, nil
}

func (p *parser) parseOnConflict() (*OnConflict, error) {
	p.advance() // ON
	if !p.acceptKw(kwConflict) {
		return nil, p.errf(p.cur(), "expected CONFLICT after ON")
	}
	oc := &OnConflict{}
	if p.acceptWord("on") || p.isWord("constraint") {
		// ON CONFLICT ON CONSTRAINT name
		if err := p.expectWord("constraint"); err != nil {
			return nil, err
		}
		n, err := p.parseIdent("constraint name")
		if err != nil {
			return nil, err
		}
		oc.Constraint = n
	} else if p.isColumnListAhead() {
		cols, err := p.parseExprTargets()
		if err != nil {
			return nil, err
		}
		oc.Targets = cols
		if p.acceptKw(kwWhere) {
			oc.IndexWhere, err = p.parseExpr()
			if err != nil {
				return nil, err
			}
		}
	}
	if !p.acceptKw(kwDo) {
		return nil, p.errf(p.cur(), "expected DO in ON CONFLICT")
	}
	if p.acceptKw(kwNothing) {
		oc.DoNothing = true
		return oc, nil
	}
	if !p.acceptKw(kwUpdate) {
		return nil, p.errf(p.cur(), "expected UPDATE or NOTHING after DO")
	}
	if !p.acceptKw(kwSet) {
		return nil, p.errf(p.cur(), "expected SET after DO UPDATE")
	}
	asgs, err := p.parseAssignments()
	if err != nil {
		return nil, err
	}
	oc.DoUpdate = asgs
	if p.acceptKw(kwWhere) {
		oc.UpdateWhere, err = p.parseExpr()
		if err != nil {
			return nil, err
		}
	}
	return oc, nil
}

// parseUpdate parses UPDATE table SET ... [FROM ...] [WHERE ...] [RETURNING ...].
func (p *parser) parseUpdate(with []*CTE) (*UpdateStmt, error) {
	p.advance() // UPDATE
	tn, err := p.parseTableName()
	if err != nil {
		return nil, err
	}
	up := &UpdateStmt{With: with, Table: tn}
	if !p.acceptKw(kwSet) {
		return nil, p.errf(p.cur(), "expected SET in UPDATE")
	}
	up.Set, err = p.parseAssignments()
	if err != nil {
		return nil, err
	}
	if p.acceptKw(kwFrom) {
		up.From, err = p.parseFromList()
		if err != nil {
			return nil, err
		}
	}
	if p.acceptKw(kwWhere) {
		up.Where, err = p.parseExpr()
		if err != nil {
			return nil, err
		}
	}
	if p.acceptKw(kwReturning) {
		up.Returning, err = p.parseSelectList()
		if err != nil {
			return nil, err
		}
	}
	return up, nil
}

// parseDelete parses DELETE FROM table [USING ...] [WHERE ...] [RETURNING ...].
func (p *parser) parseDelete(with []*CTE) (*DeleteStmt, error) {
	p.advance() // DELETE
	if !p.acceptKw(kwFrom) {
		return nil, p.errf(p.cur(), "expected FROM after DELETE")
	}
	tn, err := p.parseTableName()
	if err != nil {
		return nil, err
	}
	del := &DeleteStmt{With: with, Table: tn}
	if p.acceptKw(kwUsing) {
		del.Using, err = p.parseFromList()
		if err != nil {
			return nil, err
		}
	}
	if p.acceptKw(kwWhere) {
		del.Where, err = p.parseExpr()
		if err != nil {
			return nil, err
		}
	}
	if p.acceptKw(kwReturning) {
		del.Returning, err = p.parseSelectList()
		if err != nil {
			return nil, err
		}
	}
	return del, nil
}

// parseAssignments parses a SET list: either "col = expr" or the multi-column
// "(col, ...) = (expr, ...)" / "(col, ...) = (SELECT ...)" form, comma-separated.
func (p *parser) parseAssignments() ([]Assignment, error) {
	var asgs []Assignment
	for {
		a, err := p.parseAssignment()
		if err != nil {
			return nil, err
		}
		asgs = append(asgs, a)
		if !p.acceptType(TokenComma) {
			break
		}
	}
	return asgs, nil
}

func (p *parser) parseAssignment() (Assignment, error) {
	if p.cur().Type == TokenLParen {
		return p.parseMultiAssignment()
	}
	// The target is a column, possibly with a subscript or field indirection
	// (e.g. tags[1], composite.field).
	lhs, err := p.parsePostfix()
	if err != nil {
		return Assignment{}, err
	}
	var a Assignment
	if cr, ok := lhs.(*ColumnRef); ok && len(cr.Parts) == 1 {
		a.Column = cr.Parts[0]
	} else {
		a.Target = lhs
	}
	if !p.acceptType(TokenEq) {
		return Assignment{}, p.errf(p.cur(), "expected '=' in assignment")
	}
	a.Value, err = p.parseExpr()
	if err != nil {
		return Assignment{}, err
	}
	return a, nil
}

// parseMultiAssignment parses "(a, b) = (v1, v2)" or "(a, b) = (SELECT ...)".
func (p *parser) parseMultiAssignment() (Assignment, error) {
	cols, err := p.parseNameList()
	if err != nil {
		return Assignment{}, err
	}
	if !p.acceptType(TokenEq) {
		return Assignment{}, p.errf(p.cur(), "expected '=' after column list")
	}
	a := Assignment{Columns: cols}
	if _, err := p.expectType(TokenLParen, "'(' for value list"); err != nil {
		return Assignment{}, err
	}
	if p.isKw(kwSelect) || p.isKw(kwWith) {
		sub, err := p.parseSelect()
		if err != nil {
			return Assignment{}, err
		}
		a.Value = &SubqueryExpr{Select: sub}
	} else {
		vals, err := p.parseExprList()
		if err != nil {
			return Assignment{}, err
		}
		a.Values = vals
	}
	if _, err := p.expectType(TokenRParen, "')' after value list"); err != nil {
		return Assignment{}, err
	}
	return a, nil
}

// parseColumnTargets parses an INSERT column list "(target, ...)" where each
// target is a column with optional subscript/field indirection (f, f[1],
// f[1:2], f[1].g). Targets are stored as their rendered text.
func (p *parser) parseColumnTargets() ([]string, error) {
	if _, err := p.expectType(TokenLParen, "'('"); err != nil {
		return nil, err
	}
	var out []string
	for {
		e, err := p.parsePostfix()
		if err != nil {
			return nil, err
		}
		out = append(out, Deparse(e))
		if !p.acceptType(TokenComma) {
			break
		}
	}
	if _, err := p.expectType(TokenRParen, "')'"); err != nil {
		return nil, err
	}
	return out, nil
}

// parseExprTargets parses an ON CONFLICT target list "(expr, ...)", which may
// contain expressions (e.g. lower(name)). Targets are stored as rendered text.
func (p *parser) parseExprTargets() ([]string, error) {
	if _, err := p.expectType(TokenLParen, "'('"); err != nil {
		return nil, err
	}
	var out []string
	for {
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		out = append(out, Deparse(e))
		if !p.acceptType(TokenComma) {
			break
		}
	}
	if _, err := p.expectType(TokenRParen, "')'"); err != nil {
		return nil, err
	}
	return out, nil
}

// parseTableName parses an optionally schema-qualified table name with alias.
func (p *parser) parseTableName() (*TableName, error) {
	name, err := p.parseIdent("table name")
	if err != nil {
		return nil, err
	}
	tn := &TableName{Name: name}
	if p.acceptType(TokenDot) {
		tn.Schema = name
		tn.Name, err = p.parseIdent("table name")
		if err != nil {
			return nil, err
		}
	}
	if alias, ok, err := p.parseTableAlias(); err != nil {
		return nil, err
	} else if ok {
		tn.Alias = alias
	}
	return tn, nil
}
