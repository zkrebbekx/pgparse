package pgparse

// This file defines the AST for the DDL statements pgparse supports:
// CREATE TABLE / VIEW / INDEX, ALTER TABLE, and DROP.

// CreateTableStmt is CREATE [TEMP] TABLE [IF NOT EXISTS] name (...).
type CreateTableStmt struct {
	Table       *TableName
	Temporary   bool
	IfNotExists bool
	Columns     []ColumnDef
	Constraints []TableConstraint // table-level constraints
}

// ColumnDef is one column in a CREATE TABLE / ADD COLUMN.
type ColumnDef struct {
	Name        string
	Type        string
	Constraints []ColumnConstraint
}

// ColumnConstraintKind enumerates per-column constraints.
type ColumnConstraintKind uint8

const (
	ColConstraintNotNull ColumnConstraintKind = iota
	ColConstraintNull
	ColConstraintPrimaryKey
	ColConstraintUnique
	ColConstraintDefault
	ColConstraintCheck
	ColConstraintReferences
)

// ColumnConstraint is a single inline column constraint.
type ColumnConstraint struct {
	Kind       ColumnConstraintKind
	Name       string     // CONSTRAINT name, "" when unnamed
	Default    Expr       // ColConstraintDefault
	Check      Expr       // ColConstraintCheck
	RefTable   *TableName // ColConstraintReferences
	RefColumns []string   // referenced columns
}

// TableConstraintKind enumerates table-level constraints.
type TableConstraintKind uint8

const (
	TableConstraintPrimaryKey TableConstraintKind = iota
	TableConstraintUnique
	TableConstraintForeignKey
	TableConstraintCheck
)

// TableConstraint is a table-level constraint (PRIMARY KEY, UNIQUE, FOREIGN
// KEY, CHECK).
type TableConstraint struct {
	Kind       TableConstraintKind
	Name       string     // CONSTRAINT name, "" when unnamed
	Columns    []string   // local columns for PK/UNIQUE/FK
	Check      Expr       // TableConstraintCheck
	RefTable   *TableName // foreign key target
	RefColumns []string   // foreign key referenced columns
}

// CreateViewStmt is CREATE [OR REPLACE] [TEMP] VIEW name [(cols)] AS select.
type CreateViewStmt struct {
	Name      *TableName
	OrReplace bool
	Temporary bool
	Columns   []string
	Select    *SelectStmt
}

// CreateIndexStmt is CREATE [UNIQUE] INDEX [name] ON table [USING m] (cols)
// [WHERE predicate].
type CreateIndexStmt struct {
	Name        string
	Unique      bool
	IfNotExists bool
	Table       *TableName
	Using       string // access method (btree, gin, ...)
	Columns     []IndexElem
	Where       Expr // partial-index predicate
}

// IndexElem is one indexed column or expression.
type IndexElem struct {
	Expr Expr
	Desc bool
}

// DropStmt is DROP TABLE|VIEW|INDEX [IF EXISTS] name[, ...] [CASCADE|RESTRICT].
type DropStmt struct {
	Object   string // "TABLE", "VIEW", "INDEX", ...
	IfExists bool
	Names    []*TableName
	Cascade  bool
}

// AlterTableStmt is ALTER TABLE [IF EXISTS] name action[, ...].
type AlterTableStmt struct {
	Table    *TableName
	IfExists bool
	Actions  []AlterAction
}

// AlterActionKind enumerates ALTER TABLE actions.
type AlterActionKind uint8

const (
	AlterAddColumn AlterActionKind = iota
	AlterDropColumn
	AlterAddConstraint
	AlterDropConstraint
	AlterColumnType
	AlterColumnSetDefault
	AlterColumnDropDefault
	AlterColumnSetNotNull
	AlterColumnDropNotNull
	AlterRenameColumn
	AlterRenameTable
)

// AlterAction is a single ALTER TABLE action.
type AlterAction struct {
	Kind       AlterActionKind
	Column     ColumnDef        // AddColumn
	Name       string           // target column / constraint / new name
	NewName    string           // rename target
	Constraint *TableConstraint // AddConstraint
	Type       string           // AlterColumnType
	Default    Expr             // AlterColumnSetDefault
	Cascade    bool
}

func (*CreateTableStmt) node() {}
func (*CreateViewStmt) node()  {}
func (*CreateIndexStmt) node() {}
func (*DropStmt) node()        {}
func (*AlterTableStmt) node()  {}
func (*CreateTableStmt) stmt() {}
func (*CreateViewStmt) stmt()  {}
func (*CreateIndexStmt) stmt() {}
func (*DropStmt) stmt()        {}
func (*AlterTableStmt) stmt()  {}
