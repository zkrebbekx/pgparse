package pgparse

// Deparse rendering for DDL statements. Kept beside the DDL AST/parser so the
// three move together.

func (d *deparser) createTable(s *CreateTableStmt) {
	d.ws("CREATE ")
	if s.Temporary {
		d.ws("TEMPORARY ")
	}
	d.ws("TABLE ")
	if s.IfNotExists {
		d.ws("IF NOT EXISTS ")
	}
	d.tableName(s.Table)
	d.ws(" (")
	first := true
	for i := range s.Columns {
		if !first {
			d.ws(", ")
		}
		first = false
		d.columnDef(&s.Columns[i])
	}
	for i := range s.Constraints {
		if !first {
			d.ws(", ")
		}
		first = false
		d.tableConstraint(&s.Constraints[i])
	}
	d.ws(")")
}

func (d *deparser) columnDef(c *ColumnDef) {
	d.ws(quoteIdent(c.Name))
	d.ws(" ")
	d.ws(c.Type)
	for i := range c.Constraints {
		d.ws(" ")
		d.columnConstraint(&c.Constraints[i])
	}
}

func (d *deparser) columnConstraint(c *ColumnConstraint) {
	if c.Name != "" {
		d.ws("CONSTRAINT ")
		d.ws(quoteIdent(c.Name))
		d.ws(" ")
	}
	switch c.Kind {
	case ColConstraintNotNull:
		d.ws("NOT NULL")
	case ColConstraintNull:
		d.ws("NULL")
	case ColConstraintPrimaryKey:
		d.ws("PRIMARY KEY")
	case ColConstraintUnique:
		d.ws("UNIQUE")
	case ColConstraintDefault:
		d.ws("DEFAULT ")
		d.expr(c.Default)
	case ColConstraintCheck:
		d.ws("CHECK (")
		d.expr(c.Check)
		d.ws(")")
	case ColConstraintReferences:
		d.ws("REFERENCES ")
		d.tableName(c.RefTable)
		if len(c.RefColumns) > 0 {
			d.ws(" (")
			d.identList(c.RefColumns)
			d.ws(")")
		}
	}
}

func (d *deparser) tableConstraint(c *TableConstraint) {
	if c.Name != "" {
		d.ws("CONSTRAINT ")
		d.ws(quoteIdent(c.Name))
		d.ws(" ")
	}
	switch c.Kind {
	case TableConstraintPrimaryKey:
		d.ws("PRIMARY KEY (")
		d.identList(c.Columns)
		d.ws(")")
	case TableConstraintUnique:
		d.ws("UNIQUE (")
		d.identList(c.Columns)
		d.ws(")")
	case TableConstraintCheck:
		d.ws("CHECK (")
		d.expr(c.Check)
		d.ws(")")
	case TableConstraintForeignKey:
		d.ws("FOREIGN KEY (")
		d.identList(c.Columns)
		d.ws(") REFERENCES ")
		d.tableName(c.RefTable)
		if len(c.RefColumns) > 0 {
			d.ws(" (")
			d.identList(c.RefColumns)
			d.ws(")")
		}
	}
}

func (d *deparser) createView(s *CreateViewStmt) {
	d.ws("CREATE ")
	if s.OrReplace {
		d.ws("OR REPLACE ")
	}
	if s.Temporary {
		d.ws("TEMPORARY ")
	}
	d.ws("VIEW ")
	d.tableName(s.Name)
	if len(s.Columns) > 0 {
		d.ws(" (")
		d.identList(s.Columns)
		d.ws(")")
	}
	d.ws(" AS ")
	d.selectStmt(s.Select)
}

func (d *deparser) createIndex(s *CreateIndexStmt) {
	d.ws("CREATE ")
	if s.Unique {
		d.ws("UNIQUE ")
	}
	d.ws("INDEX ")
	if s.IfNotExists {
		d.ws("IF NOT EXISTS ")
	}
	if s.Name != "" {
		d.ws(quoteIdent(s.Name))
		d.ws(" ")
	}
	d.ws("ON ")
	d.tableName(s.Table)
	if s.Using != "" {
		d.ws(" USING ")
		d.ws(s.Using)
	}
	d.ws(" (")
	for i, e := range s.Columns {
		if i > 0 {
			d.ws(", ")
		}
		d.expr(e.Expr)
		if e.Desc {
			d.ws(" DESC")
		}
	}
	d.ws(")")
	if s.Where != nil {
		d.ws(" WHERE ")
		d.expr(s.Where)
	}
}

func (d *deparser) drop(s *DropStmt) {
	d.ws("DROP ")
	d.ws(s.Object)
	d.ws(" ")
	if s.IfExists {
		d.ws("IF EXISTS ")
	}
	for i, n := range s.Names {
		if i > 0 {
			d.ws(", ")
		}
		d.tableName(n)
	}
	if s.Cascade {
		d.ws(" CASCADE")
	}
}

func (d *deparser) alter(s *AlterTableStmt) {
	d.ws("ALTER TABLE ")
	if s.IfExists {
		d.ws("IF EXISTS ")
	}
	d.tableName(s.Table)
	d.ws(" ")
	for i := range s.Actions {
		if i > 0 {
			d.ws(", ")
		}
		d.alterAction(&s.Actions[i])
	}
}

func (d *deparser) alterAction(a *AlterAction) {
	switch a.Kind {
	case AlterAddColumn:
		d.ws("ADD COLUMN ")
		d.columnDef(&a.Column)
	case AlterDropColumn:
		d.ws("DROP COLUMN ")
		d.ws(quoteIdent(a.Name))
		if a.Cascade {
			d.ws(" CASCADE")
		}
	case AlterAddConstraint:
		d.ws("ADD ")
		d.tableConstraint(a.Constraint)
	case AlterDropConstraint:
		d.ws("DROP CONSTRAINT ")
		d.ws(quoteIdent(a.Name))
		if a.Cascade {
			d.ws(" CASCADE")
		}
	case AlterColumnType:
		d.ws("ALTER COLUMN ")
		d.ws(quoteIdent(a.Name))
		d.ws(" TYPE ")
		d.ws(a.Type)
	case AlterColumnSetDefault:
		d.ws("ALTER COLUMN ")
		d.ws(quoteIdent(a.Name))
		d.ws(" SET DEFAULT ")
		d.expr(a.Default)
	case AlterColumnDropDefault:
		d.ws("ALTER COLUMN ")
		d.ws(quoteIdent(a.Name))
		d.ws(" DROP DEFAULT")
	case AlterColumnSetNotNull:
		d.ws("ALTER COLUMN ")
		d.ws(quoteIdent(a.Name))
		d.ws(" SET NOT NULL")
	case AlterColumnDropNotNull:
		d.ws("ALTER COLUMN ")
		d.ws(quoteIdent(a.Name))
		d.ws(" DROP NOT NULL")
	case AlterRenameColumn:
		d.ws("RENAME COLUMN ")
		d.ws(quoteIdent(a.Name))
		d.ws(" TO ")
		d.ws(quoteIdent(a.NewName))
	case AlterRenameTable:
		d.ws("RENAME TO ")
		d.ws(quoteIdent(a.NewName))
	}
}
