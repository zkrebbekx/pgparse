package pgparse

// parseInsert parses INSERT INTO table [(cols)] VALUES ... | SELECT ...
// with optional ON CONFLICT and RETURNING.
func (p *Parser) parseInsert(with []*CTE) (*InsertStmt, error) {
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
		ins.Columns, err = p.parseNameList()
		if err != nil {
			return nil, err
		}
	}

	switch {
	case p.acceptKw(kwValues):
		rows, err := p.parseValuesRows()
		if err != nil {
			return nil, err
		}
		ins.Rows = rows
	case p.isKw(kwSelect) || p.isKw(kwWith) || p.cur().Type == TokenLParen:
		sel, err := p.parseSelect()
		if err != nil {
			return nil, err
		}
		ins.Select = sel
	default:
		return nil, p.errf(p.cur(), "expected VALUES or SELECT in INSERT")
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

func (p *Parser) parseValuesRows() ([][]Expr, error) {
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

func (p *Parser) parseOnConflict() (*OnConflict, error) {
	p.advance() // ON
	if !p.acceptKw(kwConflict) {
		return nil, p.errf(p.cur(), "expected CONFLICT after ON")
	}
	oc := &OnConflict{}
	if p.isColumnListAhead() {
		cols, err := p.parseNameList()
		if err != nil {
			return nil, err
		}
		oc.Targets = cols
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
	return oc, nil
}

// parseUpdate parses UPDATE table SET ... [FROM ...] [WHERE ...] [RETURNING ...].
func (p *Parser) parseUpdate(with []*CTE) (*UpdateStmt, error) {
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
func (p *Parser) parseDelete(with []*CTE) (*DeleteStmt, error) {
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

// parseAssignments parses "col = expr [, ...]" for SET clauses.
func (p *Parser) parseAssignments() ([]Assignment, error) {
	var asgs []Assignment
	for {
		col, err := p.parseIdent("column name")
		if err != nil {
			return nil, err
		}
		if !p.acceptType(TokenEq) {
			return nil, p.errf(p.cur(), "expected '=' in assignment")
		}
		val, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		asgs = append(asgs, Assignment{Column: col, Value: val})
		if !p.acceptType(TokenComma) {
			break
		}
	}
	return asgs, nil
}

// parseTableName parses an optionally schema-qualified table name with alias.
func (p *Parser) parseTableName() (*TableName, error) {
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
