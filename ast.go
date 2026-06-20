package pgparse

// Node is the root interface implemented by every AST node.
type Node interface{ node() }

// Stmt is a top-level SQL statement.
type Stmt interface {
	Node
	stmt()
}

// Expr is any value-producing expression.
type Expr interface {
	Node
	expr()
}

// ---------------------------------------------------------------------------
// Statements
// ---------------------------------------------------------------------------

// SelectStmt is a SELECT, optionally combined with set operations and CTEs.
type SelectStmt struct {
	With       []*CTE       // WITH clause, nil when absent
	Distinct   bool         // SELECT DISTINCT
	DistinctOn []Expr       // SELECT DISTINCT ON (...)
	Columns    []SelectItem // projection list
	From       []TableExpr  // FROM items (joined together)
	Where      Expr         // WHERE predicate, nil when absent
	GroupBy    []Expr       // GROUP BY keys
	Having     Expr         // HAVING predicate
	Window     []*WindowDef // named windows (WINDOW w AS (...)) — reserved
	OrderBy    []OrderItem  // ORDER BY items
	Limit      Expr         // LIMIT count
	Offset     Expr         // OFFSET count

	// Set operation: when SetOp != "", Left and Right are the operands and the
	// fields above describe Left's leading SELECT only when Left is nil.
	SetOp  SetOpKind
	SetAll bool // UNION ALL vs UNION
	Left   *SelectStmt
	Right  *SelectStmt
}

// SetOpKind identifies a set operation between two selects.
type SetOpKind uint8

const (
	SetOpNone SetOpKind = iota
	SetOpUnion
	SetOpIntersect
	SetOpExcept
)

// CTE is a single common table expression in a WITH clause.
type CTE struct {
	Name      string
	Columns   []string // optional explicit column list
	Recursive bool
	Stmt      Stmt
}

// SelectItem is one entry in a projection list.
type SelectItem struct {
	Star  bool   // SELECT *  (or table.* when Expr is a column ref)
	Expr  Expr   // value expression (nil when Star with no qualifier)
	Alias string // AS alias, "" when absent
}

// InsertStmt is INSERT INTO ... .
type InsertStmt struct {
	With       []*CTE
	Table      *TableName
	Columns    []string
	Rows       [][]Expr    // VALUES rows
	Select     *SelectStmt // INSERT ... SELECT (mutually exclusive with Rows)
	OnConflict *OnConflict
	Returning  []SelectItem
}

// OnConflict models a basic ON CONFLICT clause.
type OnConflict struct {
	Targets   []string     // conflict target columns
	DoNothing bool         // ON CONFLICT DO NOTHING
	DoUpdate  []Assignment // ON CONFLICT DO UPDATE SET ...
}

// UpdateStmt is UPDATE ... SET ... .
type UpdateStmt struct {
	With      []*CTE
	Table     *TableName
	Set       []Assignment
	From      []TableExpr
	Where     Expr
	Returning []SelectItem
}

// Assignment is a single col = expr in SET.
type Assignment struct {
	Column string
	Value  Expr
}

// DeleteStmt is DELETE FROM ... .
type DeleteStmt struct {
	With      []*CTE
	Table     *TableName
	Using     []TableExpr
	Where     Expr
	Returning []SelectItem
}

func (*SelectStmt) node() {}
func (*InsertStmt) node() {}
func (*UpdateStmt) node() {}
func (*DeleteStmt) node() {}
func (*SelectStmt) stmt() {}
func (*InsertStmt) stmt() {}
func (*UpdateStmt) stmt() {}
func (*DeleteStmt) stmt() {}

// ---------------------------------------------------------------------------
// FROM / table expressions
// ---------------------------------------------------------------------------

// TableExpr is a FROM item: a table, subquery, function, or join.
type TableExpr interface {
	Node
	tableExpr()
}

// TableName references a (optionally schema-qualified) relation.
type TableName struct {
	Schema string
	Name   string
	Alias  string
}

// SubqueryTable is a parenthesised subquery used in FROM.
type SubqueryTable struct {
	Select  *SelectStmt
	Alias   string
	Columns []string
}

// JoinExpr joins two table expressions.
type JoinExpr struct {
	Kind  JoinKind
	Left  TableExpr
	Right TableExpr
	On    Expr     // ON predicate
	Using []string // USING (cols)
}

// JoinKind enumerates join flavours.
type JoinKind uint8

const (
	JoinInner JoinKind = iota
	JoinLeft
	JoinRight
	JoinFull
	JoinCross
)

func (*TableName) node()          {}
func (*SubqueryTable) node()      {}
func (*JoinExpr) node()           {}
func (*TableName) tableExpr()     {}
func (*SubqueryTable) tableExpr() {}
func (*JoinExpr) tableExpr()      {}

// ---------------------------------------------------------------------------
// ORDER BY / windows
// ---------------------------------------------------------------------------

// OrderItem is one ORDER BY term.
type OrderItem struct {
	Expr       Expr
	Desc       bool
	NullsFirst bool
	NullsSet   bool // whether NULLS FIRST/LAST was explicit
}

// WindowDef is an OVER (...) specification.
type WindowDef struct {
	Name        string // for named WINDOW clauses
	PartitionBy []Expr
	OrderBy     []OrderItem
}

func (*WindowDef) node() {}

// ---------------------------------------------------------------------------
// Expressions
// ---------------------------------------------------------------------------

// ColumnRef is a (optionally qualified) column reference: a, t.a, s.t.a.
type ColumnRef struct {
	Parts []string // 1..3 identifiers
}

// Star is a bare * in an expression position.
type Star struct{}

// Literal is a constant: string, number, boolean, or NULL.
type Literal struct {
	Kind LiteralKind
	Val  string // raw source text (quotes stripped for strings)
}

// LiteralKind classifies a Literal.
type LiteralKind uint8

const (
	LitNull LiteralKind = iota
	LitBool
	LitInt
	LitFloat
	LitString
)

// Param is a positional parameter placeholder ($1).
type Param struct{ Num int }

// BinaryExpr is "Left Op Right".
type BinaryExpr struct {
	Op    string
	Left  Expr
	Right Expr
}

// UnaryExpr is "Op Operand" (e.g. -x, NOT x).
type UnaryExpr struct {
	Op      string
	Operand Expr
}

// FuncCall is a function invocation, possibly with DISTINCT, *, or OVER.
type FuncCall struct {
	Name     string
	Schema   string
	Args     []Expr
	Distinct bool
	Star     bool // count(*)
	Over     *WindowDef
}

// CaseExpr is a CASE expression.
type CaseExpr struct {
	Operand Expr // CASE <operand> WHEN ...; nil for searched CASE
	Whens   []CaseWhen
	Else    Expr
}

// CaseWhen is one WHEN/THEN branch.
type CaseWhen struct {
	Cond   Expr
	Result Expr
}

// CastExpr is CAST(x AS type) or x::type.
type CastExpr struct {
	Expr Expr
	Type string
}

// InExpr is "Expr IN (list)" or "Expr IN (subquery)".
type InExpr struct {
	Expr     Expr
	Not      bool
	List     []Expr
	Subquery *SelectStmt
}

// BetweenExpr is "Expr BETWEEN Low AND High".
type BetweenExpr struct {
	Expr Expr
	Not  bool
	Low  Expr
	High Expr
}

// IsExpr is "Expr IS [NOT] NULL/TRUE/FALSE".
type IsExpr struct {
	Expr Expr
	Not  bool
	Kind LiteralKind // LitNull/LitBool target
	Bool bool        // value when Kind == LitBool
}

// LikeExpr is "Expr [NOT] LIKE/ILIKE Pattern".
type LikeExpr struct {
	Expr    Expr
	Pattern Expr
	Not     bool
	ILike   bool
}

// SubqueryExpr wraps a parenthesised scalar subquery.
type SubqueryExpr struct{ Select *SelectStmt }

// ExistsExpr is EXISTS (subquery).
type ExistsExpr struct {
	Not    bool
	Select *SelectStmt
}

// ParenExpr preserves explicit parentheses for faithful round-tripping.
type ParenExpr struct{ Expr Expr }

func (*ColumnRef) node()    {}
func (*Star) node()         {}
func (*Literal) node()      {}
func (*Param) node()        {}
func (*BinaryExpr) node()   {}
func (*UnaryExpr) node()    {}
func (*FuncCall) node()     {}
func (*CaseExpr) node()     {}
func (*CastExpr) node()     {}
func (*InExpr) node()       {}
func (*BetweenExpr) node()  {}
func (*IsExpr) node()       {}
func (*LikeExpr) node()     {}
func (*SubqueryExpr) node() {}
func (*ExistsExpr) node()   {}
func (*ParenExpr) node()    {}

func (*ColumnRef) expr()    {}
func (*Star) expr()         {}
func (*Literal) expr()      {}
func (*Param) expr()        {}
func (*BinaryExpr) expr()   {}
func (*UnaryExpr) expr()    {}
func (*FuncCall) expr()     {}
func (*CaseExpr) expr()     {}
func (*CastExpr) expr()     {}
func (*InExpr) expr()       {}
func (*BetweenExpr) expr()  {}
func (*IsExpr) expr()       {}
func (*LikeExpr) expr()     {}
func (*SubqueryExpr) expr() {}
func (*ExistsExpr) expr()   {}
func (*ParenExpr) expr()    {}
