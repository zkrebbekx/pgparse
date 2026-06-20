package pgparse

import (
	"strconv"
	"strings"
)

// Deparse renders an AST node back to SQL text. It is deterministic and its
// output re-parses to an equivalent tree, which makes it useful both as a
// formatting utility and as a round-trip test oracle.
//
// Explicit grouping is preserved through ParenExpr nodes, so binary and unary
// expressions are emitted without adding parentheses; this keeps Deparse
// idempotent (deparse∘parse∘deparse == deparse∘parse).
func Deparse(n Node) string {
	var d deparser
	d.node(n)
	return d.b.String()
}

type deparser struct{ b strings.Builder }

func (d *deparser) ws(s string) { d.b.WriteString(s) }

func (d *deparser) node(n Node) {
	switch x := n.(type) {
	case *SelectStmt:
		d.selectStmt(x)
	case *InsertStmt:
		d.insert(x)
	case *UpdateStmt:
		d.update(x)
	case *DeleteStmt:
		d.del(x)
	case *CreateTableStmt:
		d.createTable(x)
	case *CreateViewStmt:
		d.createView(x)
	case *CreateIndexStmt:
		d.createIndex(x)
	case *DropStmt:
		d.drop(x)
	case *AlterTableStmt:
		d.alter(x)
	case *RawStmt:
		d.ws(x.SQL)
	case Expr:
		d.expr(x)
	case TableExpr:
		d.table(x)
	}
}

// ---------------------------------------------------------------------------
// Statements
// ---------------------------------------------------------------------------

func (d *deparser) with(ctes []*CTE) {
	if len(ctes) == 0 {
		return
	}
	d.ws("WITH ")
	if ctes[0].Recursive {
		d.ws("RECURSIVE ")
	}
	for i, c := range ctes {
		if i > 0 {
			d.ws(", ")
		}
		d.ws(quoteIdent(c.Name))
		if len(c.Columns) > 0 {
			d.ws(" (")
			d.identList(c.Columns)
			d.ws(")")
		}
		d.ws(" AS (")
		d.node(c.Stmt)
		d.ws(")")
	}
	d.ws(" ")
}

func (d *deparser) selectStmt(s *SelectStmt) {
	d.with(s.With)
	if s.Values != nil {
		d.ws("VALUES ")
		for i, row := range s.Values {
			if i > 0 {
				d.ws(", ")
			}
			d.ws("(")
			d.exprList(row)
			d.ws(")")
		}
		d.tail(s)
		return
	}
	if s.SetOp != SetOpNone {
		d.setOperand(s.Left)
		switch s.SetOp {
		case SetOpUnion:
			d.ws(" UNION ")
		case SetOpIntersect:
			d.ws(" INTERSECT ")
		case SetOpExcept:
			d.ws(" EXCEPT ")
		}
		if s.SetAll {
			d.ws("ALL ")
		}
		d.setOperand(s.Right)
		d.tail(s)
		return
	}

	d.ws("SELECT ")
	if s.Distinct {
		d.ws("DISTINCT ")
		if len(s.DistinctOn) > 0 {
			d.ws("ON (")
			d.exprList(s.DistinctOn)
			d.ws(") ")
		}
	}
	d.selectItems(s.Columns)
	if s.Into != nil {
		d.ws(" INTO ")
		d.tableName(s.Into)
	}
	if len(s.From) > 0 {
		d.ws(" FROM ")
		for i, t := range s.From {
			if i > 0 {
				d.ws(", ")
			}
			d.table(t)
		}
	}
	if s.Where != nil {
		d.ws(" WHERE ")
		d.expr(s.Where)
	}
	if len(s.GroupBy) > 0 {
		d.ws(" GROUP BY ")
		d.exprList(s.GroupBy)
	}
	if s.Having != nil {
		d.ws(" HAVING ")
		d.expr(s.Having)
	}
	if len(s.Window) > 0 {
		d.ws(" WINDOW ")
		for i, w := range s.Window {
			if i > 0 {
				d.ws(", ")
			}
			d.ws(quoteIdent(w.Name))
			d.ws(" AS ")
			d.windowSpec(w)
		}
	}
	d.tail(s)
}

// setOperand renders one operand of a set operation, wrapping it in parentheses
// when it is itself a set operation or carries a trailing ORDER BY/LIMIT/OFFSET.
// Without this, the flat output would re-parse to a differently-grouped tree
// (set-operator precedence and left-associativity would regroup the operands).
func (d *deparser) setOperand(s *SelectStmt) {
	if s.SetOp != SetOpNone || len(s.OrderBy) > 0 || s.Limit != nil || s.Offset != nil {
		d.ws("(")
		d.selectStmt(s)
		d.ws(")")
		return
	}
	d.selectStmt(s)
}

func (d *deparser) tail(s *SelectStmt) {
	if len(s.OrderBy) > 0 {
		d.ws(" ORDER BY ")
		d.orderList(s.OrderBy)
	}
	if s.Limit != nil {
		d.ws(" LIMIT ")
		d.expr(s.Limit)
	}
	if s.Offset != nil {
		d.ws(" OFFSET ")
		d.expr(s.Offset)
	}
	for _, lc := range s.Locking {
		d.ws(" FOR ")
		d.ws(lc.Strength)
		if len(lc.Of) > 0 {
			d.ws(" OF ")
			d.identList(lc.Of)
		}
		if lc.Wait != "" {
			d.ws(" ")
			d.ws(lc.Wait)
		}
	}
}

func (d *deparser) selectItems(items []SelectItem) {
	for i, it := range items {
		if i > 0 {
			d.ws(", ")
		}
		if it.Star && it.Expr == nil {
			d.ws("*")
		} else {
			d.expr(it.Expr)
		}
		if it.Alias != "" {
			d.ws(" AS ")
			d.ws(quoteIdent(it.Alias))
		}
	}
}

func (d *deparser) insert(s *InsertStmt) {
	d.with(s.With)
	d.ws("INSERT INTO ")
	d.tableName(s.Table)
	if len(s.Columns) > 0 {
		d.ws(" (")
		d.identList(s.Columns)
		d.ws(")")
	}
	switch {
	case s.DefaultValues:
		d.ws(" DEFAULT VALUES")
	case s.Select != nil:
		d.ws(" ")
		d.selectStmt(s.Select)
	case len(s.Rows) > 0:
		d.ws(" VALUES ")
		for i, row := range s.Rows {
			if i > 0 {
				d.ws(", ")
			}
			d.ws("(")
			d.exprList(row)
			d.ws(")")
		}
	}
	if s.OnConflict != nil {
		d.onConflict(s.OnConflict)
	}
	if len(s.Returning) > 0 {
		d.ws(" RETURNING ")
		d.selectItems(s.Returning)
	}
}

func (d *deparser) onConflict(oc *OnConflict) {
	d.ws(" ON CONFLICT")
	if oc.Constraint != "" {
		d.ws(" ON CONSTRAINT ")
		d.ws(quoteIdent(oc.Constraint))
	} else if len(oc.Targets) > 0 {
		d.ws(" (")
		d.identList(oc.Targets)
		d.ws(")")
		if oc.IndexWhere != nil {
			d.ws(" WHERE ")
			d.expr(oc.IndexWhere)
		}
	}
	if oc.DoNothing {
		d.ws(" DO NOTHING")
		return
	}
	d.ws(" DO UPDATE SET ")
	d.assignments(oc.DoUpdate)
	if oc.UpdateWhere != nil {
		d.ws(" WHERE ")
		d.expr(oc.UpdateWhere)
	}
}

func (d *deparser) update(s *UpdateStmt) {
	d.with(s.With)
	d.ws("UPDATE ")
	d.tableName(s.Table)
	d.ws(" SET ")
	d.assignments(s.Set)
	if len(s.From) > 0 {
		d.ws(" FROM ")
		for i, t := range s.From {
			if i > 0 {
				d.ws(", ")
			}
			d.table(t)
		}
	}
	if s.Where != nil {
		d.ws(" WHERE ")
		d.expr(s.Where)
	}
	if len(s.Returning) > 0 {
		d.ws(" RETURNING ")
		d.selectItems(s.Returning)
	}
}

func (d *deparser) del(s *DeleteStmt) {
	d.with(s.With)
	d.ws("DELETE FROM ")
	d.tableName(s.Table)
	if len(s.Using) > 0 {
		d.ws(" USING ")
		for i, t := range s.Using {
			if i > 0 {
				d.ws(", ")
			}
			d.table(t)
		}
	}
	if s.Where != nil {
		d.ws(" WHERE ")
		d.expr(s.Where)
	}
	if len(s.Returning) > 0 {
		d.ws(" RETURNING ")
		d.selectItems(s.Returning)
	}
}

func (d *deparser) assignments(as []Assignment) {
	for i, a := range as {
		if i > 0 {
			d.ws(", ")
		}
		if len(a.Columns) > 0 {
			d.ws("(")
			d.identList(a.Columns)
			d.ws(") = (")
			if len(a.Values) > 0 {
				d.exprList(a.Values)
			} else if a.Value != nil {
				d.expr(a.Value)
			}
			d.ws(")")
			continue
		}
		if a.Target != nil {
			d.expr(a.Target)
		} else {
			d.ws(quoteIdent(a.Column))
		}
		d.ws(" = ")
		d.expr(a.Value)
	}
}

// ---------------------------------------------------------------------------
// FROM
// ---------------------------------------------------------------------------

func (d *deparser) table(t TableExpr) {
	switch x := t.(type) {
	case *TableName:
		d.tableName(x)
		if x.Alias != "" {
			d.ws(" AS ")
			d.ws(quoteIdent(x.Alias))
			if len(x.ColumnAliases) > 0 {
				d.ws(" (")
				d.identList(x.ColumnAliases)
				d.ws(")")
			}
		}
	case *SubqueryTable:
		if x.Lateral {
			d.ws("LATERAL ")
		}
		d.ws("(")
		d.selectStmt(x.Select)
		d.ws(")")
		if x.Alias != "" {
			d.ws(" AS ")
			d.ws(quoteIdent(x.Alias))
			if len(x.Columns) > 0 {
				d.ws(" (")
				d.identList(x.Columns)
				d.ws(")")
			}
		}
	case *JoinExpr:
		d.join(x)
	case *FuncTable:
		if x.Lateral {
			d.ws("LATERAL ")
		}
		d.expr(x.Func)
		if x.Ordinality {
			d.ws(" WITH ORDINALITY")
		}
		if x.Alias != "" {
			d.ws(" AS ")
			d.ws(quoteIdent(x.Alias))
			if len(x.Columns) > 0 {
				d.ws(" (")
				d.identList(x.Columns)
				d.ws(")")
			}
		}
	}
}

func (d *deparser) join(j *JoinExpr) {
	d.table(j.Left)
	if j.Natural {
		d.ws(" NATURAL")
	}
	switch j.Kind {
	case JoinInner:
		d.ws(" JOIN ")
	case JoinLeft:
		d.ws(" LEFT JOIN ")
	case JoinRight:
		d.ws(" RIGHT JOIN ")
	case JoinFull:
		d.ws(" FULL JOIN ")
	case JoinCross:
		d.ws(" CROSS JOIN ")
	}
	d.table(j.Right)
	if j.On != nil {
		d.ws(" ON ")
		d.expr(j.On)
	}
	if len(j.Using) > 0 {
		d.ws(" USING (")
		d.identList(j.Using)
		d.ws(")")
	}
}

func (d *deparser) tableName(t *TableName) {
	if t.Schema != "" {
		d.ws(quoteIdent(t.Schema))
		d.ws(".")
	}
	d.ws(quoteIdent(t.Name))
}

// ---------------------------------------------------------------------------
// Expressions
// ---------------------------------------------------------------------------

func (d *deparser) expr(e Expr) {
	switch x := e.(type) {
	case *ColumnRef:
		for i, p := range x.Parts {
			if i > 0 {
				d.ws(".")
			}
			if p == "*" {
				d.ws("*")
			} else {
				d.ws(quoteIdent(p))
			}
		}
	case *Star:
		d.ws("*")
	case *Literal:
		d.literal(x)
	case *Param:
		d.ws("$")
		d.ws(strconv.Itoa(x.Num))
	case *BinaryExpr:
		d.expr(x.Left)
		d.ws(" ")
		d.ws(x.Op)
		d.ws(" ")
		d.expr(x.Right)
	case *UnaryExpr:
		if isWordOp(x.Op) {
			d.ws(x.Op)
			d.ws(" ")
		} else {
			d.ws(x.Op)
		}
		d.expr(x.Operand)
	case *FuncCall:
		d.funcCall(x)
	case *CaseExpr:
		d.caseExpr(x)
	case *CastExpr:
		d.ws("CAST(")
		d.expr(x.Expr)
		d.ws(" AS ")
		d.ws(x.Type)
		d.ws(")")
	case *InExpr:
		d.expr(x.Expr)
		if x.Not {
			d.ws(" NOT")
		}
		d.ws(" IN (")
		if x.Subquery != nil {
			d.selectStmt(x.Subquery)
		} else {
			d.exprList(x.List)
		}
		d.ws(")")
	case *BetweenExpr:
		d.expr(x.Expr)
		if x.Not {
			d.ws(" NOT")
		}
		d.ws(" BETWEEN ")
		d.expr(x.Low)
		d.ws(" AND ")
		d.expr(x.High)
	case *IsExpr:
		d.expr(x.Expr)
		d.ws(" IS ")
		if x.Not {
			d.ws("NOT ")
		}
		switch x.Kind {
		case LitNull:
			d.ws("NULL")
		case LitBool:
			if x.Bool {
				d.ws("TRUE")
			} else {
				d.ws("FALSE")
			}
		}
	case *LikeExpr:
		d.expr(x.Expr)
		if x.Not {
			d.ws(" NOT")
		}
		if x.ILike {
			d.ws(" ILIKE ")
		} else {
			d.ws(" LIKE ")
		}
		d.expr(x.Pattern)
	case *SubqueryExpr:
		d.ws("(")
		d.selectStmt(x.Select)
		d.ws(")")
	case *ExistsExpr:
		if x.Not {
			d.ws("NOT ")
		}
		d.ws("EXISTS (")
		d.selectStmt(x.Select)
		d.ws(")")
	case *ParenExpr:
		d.ws("(")
		d.expr(x.Expr)
		d.ws(")")
	case *SubscriptExpr:
		d.expr(x.Base)
		d.ws("[")
		if x.Lower != nil {
			d.expr(x.Lower)
		}
		if x.Slice {
			d.ws(":")
			if x.Upper != nil {
				d.expr(x.Upper)
			}
		}
		d.ws("]")
	case *AnyAllExpr:
		d.expr(x.Left)
		d.ws(" ")
		d.ws(x.Op)
		if x.Any {
			d.ws(" ANY (")
		} else {
			d.ws(" ALL (")
		}
		if sq, ok := x.Right.(*SubqueryExpr); ok {
			d.selectStmt(sq.Select)
		} else {
			d.expr(x.Right)
		}
		d.ws(")")
	case *ArrayExpr:
		if x.Subquery != nil {
			d.ws("ARRAY(")
			d.selectStmt(x.Subquery)
			d.ws(")")
		} else {
			d.ws("ARRAY[")
			d.exprList(x.Elements)
			d.ws("]")
		}
	case *IsDistinctExpr:
		d.expr(x.Left)
		if x.Not {
			d.ws(" IS NOT DISTINCT FROM ")
		} else {
			d.ws(" IS DISTINCT FROM ")
		}
		d.expr(x.Right)
	case *RowExpr:
		if x.Explicit {
			d.ws("ROW")
		}
		d.ws("(")
		d.exprList(x.Elements)
		d.ws(")")
	case *GroupingExpr:
		d.ws(x.Kind)
		d.ws(" (")
		d.exprList(x.Args)
		d.ws(")")
	case *CollateExpr:
		d.expr(x.Expr)
		d.ws(" COLLATE ")
		d.ws(x.Collation)
	}
}

func (d *deparser) literal(l *Literal) {
	switch l.Kind {
	case LitNull:
		d.ws("NULL")
	case LitBool:
		d.ws(l.Val)
	case LitInt, LitFloat:
		d.ws(l.Val)
	case LitString:
		d.ws("'")
		d.ws(strings.ReplaceAll(l.Val, "'", "''"))
		d.ws("'")
	}
}

func (d *deparser) funcCall(f *FuncCall) {
	if f.Schema == "" && !f.Star && isSpecialFunc(f.Name) && d.specialFunc(f) {
		return
	}
	if f.Schema != "" {
		d.ws(quoteIdent(f.Schema))
		d.ws(".")
	}
	d.ws(f.Name)
	d.ws("(")
	if f.Star {
		d.ws("*")
	} else {
		if f.Distinct {
			d.ws("DISTINCT ")
		}
		d.exprList(f.Args)
		if len(f.OrderBy) > 0 {
			d.ws(" ORDER BY ")
			d.orderList(f.OrderBy)
		}
	}
	d.ws(")")
	if len(f.WithinGroup) > 0 {
		d.ws(" WITHIN GROUP (ORDER BY ")
		d.orderList(f.WithinGroup)
		d.ws(")")
	}
	if f.Filter != nil {
		d.ws(" FILTER (WHERE ")
		d.expr(f.Filter)
		d.ws(")")
	}
	if f.Over != nil {
		d.over(f.Over)
	}
}

// specialFunc renders a special-syntax function in its canonical keyword form
// so that the output re-parses through parseSpecialArgs. It returns false to
// fall back to the generic rendering when the argument shape is unexpected.
func (d *deparser) specialFunc(f *FuncCall) bool {
	switch strings.ToLower(f.Name) {
	case "extract":
		if len(f.Args) != 2 {
			return false
		}
		d.ws(f.Name)
		d.ws("(")
		if lit, ok := f.Args[0].(*Literal); ok {
			d.ws(lit.Val) // field name, emitted bare
		} else {
			d.expr(f.Args[0])
		}
		d.ws(" FROM ")
		d.expr(f.Args[1])
		d.ws(")")
		return true
	case "position":
		if len(f.Args) != 2 {
			return false
		}
		d.ws(f.Name)
		d.ws("(")
		d.expr(f.Args[0])
		d.ws(" IN ")
		d.expr(f.Args[1])
		d.ws(")")
		return true
	case "substring", "overlay":
		if len(f.Args) < 2 {
			return false
		}
		d.ws(f.Name)
		d.ws("(")
		d.expr(f.Args[0])
		d.ws(" FROM ")
		d.expr(f.Args[1])
		if len(f.Args) >= 3 {
			d.ws(" FOR ")
			d.expr(f.Args[2])
		}
		d.ws(")")
		return true
	case "trim":
		d.ws(f.Name)
		d.ws("(")
		if len(f.Args) == 2 {
			d.expr(f.Args[0])
			d.ws(" FROM ")
			d.expr(f.Args[1])
		} else if len(f.Args) == 1 {
			d.expr(f.Args[0])
		} else {
			return false
		}
		d.ws(")")
		return true
	}
	return false
}

func (d *deparser) over(w *WindowDef) {
	d.ws(" OVER ")
	// A bare reference to a named window: OVER w (no partition/order/frame).
	if w.Ref != "" && len(w.PartitionBy) == 0 && len(w.OrderBy) == 0 && w.Frame == nil {
		d.ws(quoteIdent(w.Ref))
		return
	}
	d.windowSpec(w)
}

// windowSpec renders the "(...)" body of a window definition.
func (d *deparser) windowSpec(w *WindowDef) {
	d.ws("(")
	sp := false
	if w.Ref != "" {
		d.ws(quoteIdent(w.Ref))
		sp = true
	}
	if len(w.PartitionBy) > 0 {
		if sp {
			d.ws(" ")
		}
		d.ws("PARTITION BY ")
		d.exprList(w.PartitionBy)
		sp = true
	}
	if len(w.OrderBy) > 0 {
		if sp {
			d.ws(" ")
		}
		d.ws("ORDER BY ")
		d.orderList(w.OrderBy)
		sp = true
	}
	if w.Frame != nil {
		if sp {
			d.ws(" ")
		}
		d.frame(w.Frame)
	}
	d.ws(")")
}

func (d *deparser) frame(f *WindowFrame) {
	d.ws(f.Mode)
	d.ws(" ")
	if f.End != nil {
		d.ws("BETWEEN ")
		d.frameBound(f.Start)
		d.ws(" AND ")
		d.frameBound(*f.End)
	} else {
		d.frameBound(f.Start)
	}
}

func (d *deparser) frameBound(b FrameBound) {
	switch b.Kind {
	case FrameUnboundedPreceding:
		d.ws("UNBOUNDED PRECEDING")
	case FramePreceding:
		d.expr(b.Offset)
		d.ws(" PRECEDING")
	case FrameCurrentRow:
		d.ws("CURRENT ROW")
	case FrameFollowing:
		d.expr(b.Offset)
		d.ws(" FOLLOWING")
	case FrameUnboundedFollowing:
		d.ws("UNBOUNDED FOLLOWING")
	}
}

func (d *deparser) caseExpr(c *CaseExpr) {
	d.ws("CASE")
	if c.Operand != nil {
		d.ws(" ")
		d.expr(c.Operand)
	}
	for _, w := range c.Whens {
		d.ws(" WHEN ")
		d.expr(w.Cond)
		d.ws(" THEN ")
		d.expr(w.Result)
	}
	if c.Else != nil {
		d.ws(" ELSE ")
		d.expr(c.Else)
	}
	d.ws(" END")
}

func (d *deparser) exprList(es []Expr) {
	for i, e := range es {
		if i > 0 {
			d.ws(", ")
		}
		d.expr(e)
	}
}

func (d *deparser) orderList(os []OrderItem) {
	for i, o := range os {
		if i > 0 {
			d.ws(", ")
		}
		d.expr(o.Expr)
		if o.UsingOp != "" {
			d.ws(" USING ")
			d.ws(o.UsingOp)
		} else if o.Desc {
			d.ws(" DESC")
		}
		if o.NullsSet {
			if o.NullsFirst {
				d.ws(" NULLS FIRST")
			} else {
				d.ws(" NULLS LAST")
			}
		}
	}
}

func (d *deparser) identList(names []string) {
	for i, n := range names {
		if i > 0 {
			d.ws(", ")
		}
		d.ws(quoteIdent(n))
	}
}

// isWordOp reports whether a unary operator is an alphabetic word (needing a
// trailing space) rather than a symbol.
func isWordOp(op string) bool {
	for i := 0; i < len(op); i++ {
		c := op[i]
		if !(c >= 'A' && c <= 'Z') && !(c >= 'a' && c <= 'z') {
			return false
		}
	}
	return len(op) > 0
}

// quoteIdent double-quotes an identifier only when it is not a safe bare
// identifier (lowercase letters, digits, underscore, not starting with a digit).
func quoteIdent(s string) string {
	if s == "" {
		return `""`
	}
	safe := true
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c == '_':
		case c >= '0' && c <= '9' && i > 0:
		default:
			safe = false
		}
		if !safe {
			break
		}
	}
	if safe && lookupKeyword(s) == kwNone {
		return s
	}
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}
