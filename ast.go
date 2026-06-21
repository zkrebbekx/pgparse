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

	// Values holds the rows of a VALUES query (VALUES (1,2),(3,4)). When set,
	// this SelectStmt is a values list and the projection/FROM fields are empty.
	Values [][]Expr

	// Locking holds FOR UPDATE / FOR SHARE clauses.
	Locking []LockClause

	// Into holds the target of SELECT ... INTO table.
	Into *TableName
}

// LockClause is a row-level locking clause: FOR UPDATE/SHARE [OF t,...]
// [NOWAIT|SKIP LOCKED].
type LockClause struct {
	Strength string   // "UPDATE", "NO KEY UPDATE", "SHARE", "KEY SHARE"
	Of       []string // OF table list
	Wait     string   // "", "NOWAIT", "SKIP LOCKED"
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
	Aux       string // verbatim SEARCH/CYCLE clause text, preserved but not modelled
}

// SelectItem is one entry in a projection list.
type SelectItem struct {
	Star  bool   // SELECT *  (or table.* when Expr is a column ref)
	Expr  Expr   // value expression (nil when Star with no qualifier)
	Alias string // AS alias, "" when absent
}

// InsertStmt is INSERT INTO ... .
type InsertStmt struct {
	With          []*CTE
	Table         *TableName
	Columns       []string
	Rows          [][]Expr    // VALUES rows
	Select        *SelectStmt // INSERT ... SELECT (mutually exclusive with Rows)
	DefaultValues bool        // INSERT INTO t DEFAULT VALUES
	OnConflict    *OnConflict
	Returning     []SelectItem
}

// OnConflict models an ON CONFLICT clause.
type OnConflict struct {
	Targets     []string     // conflict target columns
	Constraint  string       // ON CONFLICT ON CONSTRAINT name
	IndexWhere  Expr         // partial-index predicate on the conflict target
	DoNothing   bool         // ON CONFLICT DO NOTHING
	DoUpdate    []Assignment // ON CONFLICT DO UPDATE SET ...
	UpdateWhere Expr         // DO UPDATE ... WHERE predicate
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

// Assignment is one SET target. The common form is a single column = value
// (Column / Value). PostgreSQL also allows a multi-column form,
// SET (a, b) = (v1, v2) or SET (a, b) = (SELECT ...), captured by Columns plus
// either Values (the parenthesised expression list) or Value (a row subquery).
type Assignment struct {
	Column  string   // single-column target ("" when multi-column or indirected)
	Target  Expr     // complex single target (subscript/field), e.g. tags[1]
	Value   Expr     // single value, or a row subquery for the multi-column form
	Columns []string // multi-column targets (nil for the single form)
	Values  []Expr   // multi-column values aligned with Columns
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
	Schema        string
	Name          string
	Alias         string
	ColumnAliases []string // t AS x (c1, c2) column-alias list
}

// SubqueryTable is a parenthesised subquery used in FROM.
type SubqueryTable struct {
	Select  *SelectStmt
	Alias   string
	Columns []string
	Lateral bool // LATERAL (...)
}

// JoinExpr joins two table expressions.
type JoinExpr struct {
	Kind    JoinKind
	Natural bool // NATURAL join (no ON/USING)
	Left    TableExpr
	Right   TableExpr
	On      Expr     // ON predicate
	Using   []string // USING (cols)
}

// FuncTable is a set-returning function used as a FROM item, e.g.
// generate_series(1, 10) AS g, optionally WITH ORDINALITY.
type FuncTable struct {
	Func       Expr // the function call
	Lateral    bool
	Ordinality bool
	Alias      string
	Columns    []string
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
func (*FuncTable) node()          {}
func (*TableName) tableExpr()     {}
func (*SubqueryTable) tableExpr() {}
func (*JoinExpr) tableExpr()      {}
func (*FuncTable) tableExpr()     {}

// ---------------------------------------------------------------------------
// ORDER BY / windows
// ---------------------------------------------------------------------------

// OrderItem is one ORDER BY term.
type OrderItem struct {
	Expr       Expr
	Desc       bool
	UsingOp    string // ORDER BY x USING <op>; "" when ASC/DESC
	NullsFirst bool
	NullsSet   bool // whether NULLS FIRST/LAST was explicit
}

// GroupingExpr is a GROUP BY grouping element: ROLLUP/CUBE/GROUPING SETS (...).
type GroupingExpr struct {
	Kind string // "ROLLUP", "CUBE", "GROUPING SETS"
	Args []Expr
}

func (*GroupingExpr) node() {}
func (*GroupingExpr) expr() {}

// WindowDef is an OVER specification: a reference to a named window (only Ref
// set), an inline definition, or a named definition in a WINDOW clause (Name
// set). Ref may also hold the base window name of an inline definition that
// copies an existing window, e.g. OVER (w ORDER BY x).
type WindowDef struct {
	Name        string       // WINDOW name AS (...) definition name
	Ref         string       // OVER window_name, or base window inside OVER (w ...)
	PartitionBy []Expr       // PARTITION BY ...
	OrderBy     []OrderItem  // ORDER BY ...
	Frame       *WindowFrame // ROWS/RANGE/GROUPS frame, nil when absent
}

// WindowFrame is a frame clause: ROWS|RANGE|GROUPS Start [BETWEEN Start AND End].
type WindowFrame struct {
	Mode    string      // "ROWS", "RANGE", or "GROUPS"
	Start   FrameBound  // frame start (or the sole bound when End is nil)
	End     *FrameBound // frame end when BETWEEN ... AND ... is used
	Exclude string      // verbatim EXCLUDE clause, e.g. "EXCLUDE TIES" ("" when absent)
}

// FrameBound is one frame endpoint.
type FrameBound struct {
	Kind   FrameBoundKind
	Offset Expr // for N PRECEDING / N FOLLOWING
}

// FrameBoundKind enumerates frame endpoint kinds.
type FrameBoundKind uint8

const (
	FrameUnboundedPreceding FrameBoundKind = iota
	FramePreceding
	FrameCurrentRow
	FrameFollowing
	FrameUnboundedFollowing
)

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

// FuncCall is a function invocation, possibly with DISTINCT, *, ORDER BY in the
// argument list, a FILTER clause, WITHIN GROUP, or an OVER window.
type FuncCall struct {
	Name        string
	Schema      string
	Args        []Expr
	Distinct    bool
	Star        bool        // count(*)
	OrderBy     []OrderItem // aggregate ORDER BY: array_agg(x ORDER BY y)
	Filter      Expr        // FILTER (WHERE ...)
	WithinGroup []OrderItem // WITHIN GROUP (ORDER BY ...)
	Over        *WindowDef
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

// SubscriptExpr is an array subscript a[i] or slice a[lo:hi]. For a slice,
// Slice is true and either bound may be nil (a[:hi], a[lo:]).
type SubscriptExpr struct {
	Base  Expr
	Lower Expr
	Upper Expr
	Slice bool
}

// AnyAllExpr is "Left Op ANY/ALL (Right)" where Right is an array expression or
// a subquery. Any is true for ANY/SOME, false for ALL.
type AnyAllExpr struct {
	Op    string
	Any   bool
	Left  Expr
	Right Expr // array expression, or *SubqueryExpr
}

// ArrayExpr is an ARRAY[...] constructor or ARRAY(subquery).
type ArrayExpr struct {
	Elements []Expr
	Subquery *SelectStmt
}

// IsDistinctExpr is "Left IS [NOT] DISTINCT FROM Right".
type IsDistinctExpr struct {
	Left  Expr
	Right Expr
	Not   bool
}

// RowExpr is a row/tuple constructor: ROW(a, b) or a bare (a, b, ...).
type RowExpr struct {
	Elements []Expr
	Explicit bool // true when written ROW(...)
}

// CollateExpr is "Expr COLLATE collation".
type CollateExpr struct {
	Expr      Expr
	Collation string // collation name, as written (may be quoted/qualified)
}

func (*CollateExpr) node() {}
func (*CollateExpr) expr() {}

func (*ColumnRef) node()      {}
func (*Star) node()           {}
func (*Literal) node()        {}
func (*Param) node()          {}
func (*BinaryExpr) node()     {}
func (*UnaryExpr) node()      {}
func (*FuncCall) node()       {}
func (*CaseExpr) node()       {}
func (*CastExpr) node()       {}
func (*InExpr) node()         {}
func (*BetweenExpr) node()    {}
func (*IsExpr) node()         {}
func (*LikeExpr) node()       {}
func (*SubqueryExpr) node()   {}
func (*ExistsExpr) node()     {}
func (*ParenExpr) node()      {}
func (*SubscriptExpr) node()  {}
func (*AnyAllExpr) node()     {}
func (*ArrayExpr) node()      {}
func (*IsDistinctExpr) node() {}
func (*RowExpr) node()        {}

func (*ColumnRef) expr()      {}
func (*Star) expr()           {}
func (*Literal) expr()        {}
func (*Param) expr()          {}
func (*BinaryExpr) expr()     {}
func (*UnaryExpr) expr()      {}
func (*FuncCall) expr()       {}
func (*CaseExpr) expr()       {}
func (*CastExpr) expr()       {}
func (*InExpr) expr()         {}
func (*BetweenExpr) expr()    {}
func (*IsExpr) expr()         {}
func (*LikeExpr) expr()       {}
func (*SubqueryExpr) expr()   {}
func (*ExistsExpr) expr()     {}
func (*ParenExpr) expr()      {}
func (*SubscriptExpr) expr()  {}
func (*AnyAllExpr) expr()     {}
func (*ArrayExpr) expr()      {}
func (*IsDistinctExpr) expr() {}
func (*RowExpr) expr()        {}
