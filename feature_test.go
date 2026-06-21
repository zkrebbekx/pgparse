package pgparse

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestIndirectionAndTargets(t *testing.T) {
	Convey("Given an INSERT whose column targets use subscripts and fields", t, func() {
		s, err := ParseOne("INSERT INTO t (f2[1], f4[1].if2[2], plain) VALUES (1, 2, 3)")
		So(err, ShouldBeNil)
		Convey("Then each target is captured with its indirection", func() {
			So(s.(*InsertStmt).Columns, ShouldResemble,
				[]string{"f2[1]", "f4[1].if2[2]", "plain"})
		})
	})

	Convey("Given field selection on a complex expression", t, func() {
		s, _ := ParseOne("SELECT (x).a, (arr[1]).b, (f()).* FROM t")
		cols := s.(*SelectStmt).Columns
		Convey("Then FieldExpr nodes are produced, including (expr).*", func() {
			So(cols[0].Expr, ShouldHaveSameTypeAs, &FieldExpr{})
			So(cols[0].Expr.(*FieldExpr).Field, ShouldEqual, "a")
			So(cols[2].Expr.(*FieldExpr).Field, ShouldEqual, "*")
		})
	})

	Convey("Given ON CONFLICT on an expression", t, func() {
		s, err := ParseOne("INSERT INTO t (a) VALUES (1) ON CONFLICT (lower(name)) DO NOTHING")
		So(err, ShouldBeNil)
		Convey("Then the conflict target keeps the expression text", func() {
			So(s.(*InsertStmt).OnConflict.Targets, ShouldResemble, []string{"lower(name)"})
		})
	})
}

func TestJoinAliasAndFuncCols(t *testing.T) {
	Convey("Given a join with a USING alias", t, func() {
		s, _ := ParseOne("SELECT * FROM a JOIN b USING (id) AS j")
		Convey("Then the JoinExpr carries the alias", func() {
			So(s.(*SelectStmt).From[0].(*JoinExpr).Alias, ShouldEqual, "j")
		})
	})

	Convey("Given a parenthesised join with an alias", t, func() {
		s, _ := ParseOne("SELECT * FROM (a JOIN b USING (id)) AS j")
		Convey("Then the inner JoinExpr carries the alias", func() {
			So(s.(*SelectStmt).From[0].(*JoinExpr).Alias, ShouldEqual, "j")
		})
	})

	Convey("Given a set-returning function with a column-definition list", t, func() {
		s, err := ParseOne(`SELECT * FROM jsonb_to_record('{}') AS x (a int, b text)`)
		So(err, ShouldBeNil)
		ft := s.(*SelectStmt).From[0].(*FuncTable)
		Convey("Then names and the verbatim definition text are both kept", func() {
			So(ft.Alias, ShouldEqual, "x")
			So(ft.Columns, ShouldResemble, []string{"a", "b"})
			So(ft.ColumnsText, ShouldEqual, "(a int, b text)")
		})
	})

	Convey("Given a column alias that is a non-reserved keyword", t, func() {
		s, err := ParseOne("SELECT count(*) AS desc FROM t")
		So(err, ShouldBeNil)
		Convey("Then the keyword is accepted as the alias", func() {
			So(s.(*SelectStmt).Columns[0].Alias, ShouldEqual, "desc")
		})
	})
}
