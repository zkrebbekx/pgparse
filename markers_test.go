package pgparse

import "testing"

// TestNodeMarkers exercises the no-op interface-marker methods (node/stmt/expr/
// tableExpr) on every AST type. They never run during normal parsing, so
// line-coverage tools would otherwise report them as dead; this keeps the
// coverage figure reflective of the executable code.
func TestNodeMarkers(t *testing.T) {
	stmts := []Stmt{
		&SelectStmt{}, &InsertStmt{}, &UpdateStmt{}, &DeleteStmt{},
		&CreateTableStmt{}, &CreateViewStmt{}, &CreateIndexStmt{}, &DropStmt{},
		&AlterTableStmt{}, &RawStmt{},
	}
	for _, s := range stmts {
		s.node()
		s.stmt()
	}

	exprs := []Expr{
		&ColumnRef{}, &Star{}, &Literal{}, &Param{}, &BinaryExpr{}, &UnaryExpr{},
		&FuncCall{}, &CaseExpr{}, &CastExpr{}, &InExpr{}, &BetweenExpr{}, &IsExpr{},
		&LikeExpr{}, &SubqueryExpr{}, &ExistsExpr{}, &ParenExpr{}, &SubscriptExpr{},
		&AnyAllExpr{}, &ArrayExpr{}, &IsDistinctExpr{}, &RowExpr{}, &GroupingExpr{},
		&CollateExpr{},
	}
	for _, e := range exprs {
		e.node()
		e.expr()
	}

	tabs := []TableExpr{&TableName{}, &SubqueryTable{}, &JoinExpr{}, &FuncTable{}}
	for _, te := range tabs {
		te.node()
		te.tableExpr()
	}

	(&WindowDef{}).node()
}
