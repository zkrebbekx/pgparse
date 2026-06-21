package pgparse

import (
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestSelect(t *testing.T) {
	Convey("Given a SELECT with projection, FROM, WHERE and ORDER", t, func() {
		Convey("When parsed", func() {
			s, err := ParseOne("SELECT id, name AS n FROM users WHERE id = $1 ORDER BY name DESC LIMIT 10")
			Convey("Then the AST reflects each clause", func() {
				So(err, ShouldBeNil)
				sel := s.(*SelectStmt)
				So(len(sel.Columns), ShouldEqual, 2)
				So(sel.Columns[1].Alias, ShouldEqual, "n")
				So(len(sel.From), ShouldEqual, 1)
				So(sel.From[0].(*TableName).Name, ShouldEqual, "users")
				So(sel.Where, ShouldHaveSameTypeAs, &BinaryExpr{})
				So(len(sel.OrderBy), ShouldEqual, 1)
				So(sel.OrderBy[0].Desc, ShouldBeTrue)
				So(sel.Limit.(*Literal).Val, ShouldEqual, "10")
			})
		})
	})

	Convey("Given SELECT * with no FROM", t, func() {
		Convey("When parsed", func() {
			s, err := ParseOne("SELECT *")
			Convey("Then a single star item is produced", func() {
				So(err, ShouldBeNil)
				So(s.(*SelectStmt).Columns[0].Star, ShouldBeTrue)
			})
		})
	})

	Convey("Given an inner join with ON", t, func() {
		Convey("When parsed", func() {
			s, _ := ParseOne("SELECT * FROM a JOIN b ON a.id = b.a_id")
			Convey("Then a JoinExpr is built with both sides", func() {
				j := s.(*SelectStmt).From[0].(*JoinExpr)
				So(j.Kind, ShouldEqual, JoinInner)
				So(j.Left.(*TableName).Name, ShouldEqual, "a")
				So(j.Right.(*TableName).Name, ShouldEqual, "b")
				So(j.On, ShouldNotBeNil)
			})
		})
	})

	Convey("Given a LEFT OUTER JOIN", t, func() {
		Convey("When parsed", func() {
			s, _ := ParseOne("SELECT * FROM a LEFT OUTER JOIN b USING (id)")
			Convey("Then the join kind is left and USING is captured", func() {
				j := s.(*SelectStmt).From[0].(*JoinExpr)
				So(j.Kind, ShouldEqual, JoinLeft)
				So(j.Using, ShouldResemble, []string{"id"})
			})
		})
	})

	Convey("Given a UNION of two selects", t, func() {
		Convey("When parsed", func() {
			s, _ := ParseOne("SELECT 1 UNION ALL SELECT 2")
			Convey("Then a set-op node wraps both operands", func() {
				sel := s.(*SelectStmt)
				So(sel.SetOp, ShouldEqual, SetOpUnion)
				So(sel.SetAll, ShouldBeTrue)
				So(sel.Left, ShouldNotBeNil)
				So(sel.Right, ShouldNotBeNil)
			})
		})
	})

	Convey("Given ORDER BY / LIMIT trailing a UNION", t, func() {
		Convey("When parsed", func() {
			s, _ := ParseOne("SELECT a FROM t UNION SELECT a FROM u ORDER BY a LIMIT 5")
			Convey("Then the tail binds to the whole union, not the last operand", func() {
				sel := s.(*SelectStmt)
				So(sel.SetOp, ShouldEqual, SetOpUnion)
				So(len(sel.OrderBy), ShouldEqual, 1)
				So(sel.Limit.(*Literal).Val, ShouldEqual, "5")
				// The right operand must NOT have absorbed the tail.
				So(len(sel.Right.OrderBy), ShouldEqual, 0)
				So(sel.Right.Limit, ShouldBeNil)
			})
		})
	})

	Convey("Given mixed UNION and INTERSECT", t, func() {
		Convey("When parsed", func() {
			s, _ := ParseOne("SELECT 1 UNION SELECT 2 INTERSECT SELECT 3")
			Convey("Then INTERSECT binds tighter than UNION", func() {
				sel := s.(*SelectStmt)
				So(sel.SetOp, ShouldEqual, SetOpUnion)
				// Right side is (SELECT 2 INTERSECT SELECT 3).
				So(sel.Right.SetOp, ShouldEqual, SetOpIntersect)
			})
		})
	})

	Convey("Given a CTE feeding a select", t, func() {
		Convey("When parsed", func() {
			s, err := ParseOne("WITH recent AS (SELECT * FROM orders) SELECT * FROM recent")
			Convey("Then the WITH clause is attached", func() {
				So(err, ShouldBeNil)
				sel := s.(*SelectStmt)
				So(len(sel.With), ShouldEqual, 1)
				So(sel.With[0].Name, ShouldEqual, "recent")
			})
		})
	})

	Convey("Given a data-modifying CTE (UPDATE ... RETURNING)", t, func() {
		Convey("When parsed", func() {
			s, err := ParseOne("WITH upd AS (UPDATE t SET a = 1 WHERE id = $1 RETURNING id, a) SELECT * FROM upd")
			Convey("Then the CTE body is an UpdateStmt with RETURNING", func() {
				So(err, ShouldBeNil)
				sel := s.(*SelectStmt)
				So(len(sel.With), ShouldEqual, 1)
				up := sel.With[0].Stmt.(*UpdateStmt)
				So(up.Table.Name, ShouldEqual, "t")
				So(len(up.Returning), ShouldEqual, 2)
			})
		})
	})

	Convey("Given a DELETE-CTE feeding an INSERT", t, func() {
		Convey("When parsed", func() {
			s, err := ParseOne("WITH moved AS (DELETE FROM t WHERE x < $1 RETURNING *) INSERT INTO archive SELECT * FROM moved")
			Convey("Then the outer statement is an INSERT carrying the data-modifying CTE", func() {
				So(err, ShouldBeNil)
				ins := s.(*InsertStmt)
				So(len(ins.With), ShouldEqual, 1)
				So(ins.With[0].Stmt, ShouldHaveSameTypeAs, &DeleteStmt{})
			})
		})
	})

	Convey("Given a window function", t, func() {
		Convey("When parsed", func() {
			s, err := ParseOne("SELECT row_number() OVER (PARTITION BY dept ORDER BY salary DESC) FROM emp")
			Convey("Then the OVER spec is captured", func() {
				So(err, ShouldBeNil)
				fc := s.(*SelectStmt).Columns[0].Expr.(*FuncCall)
				So(fc.Name, ShouldEqual, "row_number")
				So(fc.Over, ShouldNotBeNil)
				So(len(fc.Over.PartitionBy), ShouldEqual, 1)
				So(len(fc.Over.OrderBy), ShouldEqual, 1)
			})
		})
	})

	Convey("Given a correlated subquery in WHERE with EXISTS", t, func() {
		Convey("When parsed", func() {
			s, err := ParseOne("SELECT * FROM a WHERE EXISTS (SELECT 1 FROM b WHERE b.a = a.id)")
			Convey("Then an ExistsExpr wraps the subquery", func() {
				So(err, ShouldBeNil)
				So(s.(*SelectStmt).Where, ShouldHaveSameTypeAs, &ExistsExpr{})
			})
		})
	})
}

func TestExpressions(t *testing.T) {
	Convey("Given expression operators with precedence", t, func() {
		Convey("When 'a + b * c' is parsed", func() {
			s, _ := ParseOne("SELECT a + b * c")
			Convey("Then multiplication binds tighter than addition", func() {
				top := s.(*SelectStmt).Columns[0].Expr.(*BinaryExpr)
				So(top.Op, ShouldEqual, "+")
				So(top.Right.(*BinaryExpr).Op, ShouldEqual, "*")
			})
		})

		Convey("When a CASE expression is parsed", func() {
			s, err := ParseOne("SELECT CASE WHEN x > 0 THEN 'pos' ELSE 'neg' END")
			Convey("Then the branches and else are captured", func() {
				So(err, ShouldBeNil)
				ce := s.(*SelectStmt).Columns[0].Expr.(*CaseExpr)
				So(len(ce.Whens), ShouldEqual, 1)
				So(ce.Else, ShouldNotBeNil)
			})
		})

		Convey("When an IN list and BETWEEN are parsed", func() {
			s, err := ParseOne("SELECT * FROM t WHERE a IN (1, 2, 3) AND b NOT BETWEEN 10 AND 20")
			Convey("Then the predicates are modelled", func() {
				So(err, ShouldBeNil)
				and := s.(*SelectStmt).Where.(*BinaryExpr)
				So(and.Left, ShouldHaveSameTypeAs, &InExpr{})
				bt := and.Right.(*BetweenExpr)
				So(bt.Not, ShouldBeTrue)
			})
		})

		Convey("When a cast with :: is parsed", func() {
			s, _ := ParseOne("SELECT id::text")
			Convey("Then a CastExpr to text is produced", func() {
				c := s.(*SelectStmt).Columns[0].Expr.(*CastExpr)
				So(c.Type, ShouldEqual, "text")
			})
		})

		Convey("When IS NOT NULL is parsed", func() {
			s, _ := ParseOne("SELECT * FROM t WHERE x IS NOT NULL")
			Convey("Then an IsExpr with Not is produced", func() {
				ie := s.(*SelectStmt).Where.(*IsExpr)
				So(ie.Not, ShouldBeTrue)
				So(ie.Kind, ShouldEqual, LitNull)
			})
		})
	})
}

func TestPostgresConstructs(t *testing.T) {
	Convey("Given Postgres-specific literal and function syntax", t, func() {
		Convey("When a typed date literal is parsed", func() {
			s, err := ParseOne("SELECT date '1998-12-01'")
			Convey("Then it becomes a cast of a string literal", func() {
				So(err, ShouldBeNil)
				c := s.(*SelectStmt).Columns[0].Expr.(*CastExpr)
				So(c.Type, ShouldEqual, "date")
				So(c.Expr.(*Literal).Val, ShouldEqual, "1998-12-01")
			})
		})

		Convey("When INTERVAL with a unit is parsed", func() {
			s, err := ParseOne("SELECT date '1998-12-01' - interval '90' day")
			Convey("Then the trailing unit is consumed", func() {
				So(err, ShouldBeNil)
				So(s.(*SelectStmt).Columns[0].Expr, ShouldHaveSameTypeAs, &BinaryExpr{})
			})
		})

		Convey("When extract(field FROM source) is parsed", func() {
			s, err := ParseOne("SELECT extract(year FROM o_orderdate) FROM orders")
			Convey("Then a func call with field and source args is built", func() {
				So(err, ShouldBeNil)
				fc := s.(*SelectStmt).Columns[0].Expr.(*FuncCall)
				So(fc.Name, ShouldEqual, "extract")
				So(len(fc.Args), ShouldEqual, 2)
				So(fc.Args[0].(*Literal).Val, ShouldEqual, "year")
			})
		})

		Convey("When substring(x FROM a FOR b) is parsed", func() {
			s, err := ParseOne("SELECT substring(c_phone FROM 1 FOR 2) FROM customer")
			Convey("Then three arguments are captured", func() {
				So(err, ShouldBeNil)
				fc := s.(*SelectStmt).Columns[0].Expr.(*FuncCall)
				So(fc.Name, ShouldEqual, "substring")
				So(len(fc.Args), ShouldEqual, 3)
			})
		})

		Convey("When JSON/array operators are parsed", func() {
			s, err := ParseOne("SELECT a -> 'k' ->> 'm' FROM t WHERE meta @> '{}' AND tags ?| arr")
			Convey("Then they bind as left-assoc binary operators above comparison", func() {
				So(err, ShouldBeNil)
				and := s.(*SelectStmt).Where.(*BinaryExpr)
				So(and.Op, ShouldEqual, "AND")
				So(and.Left.(*BinaryExpr).Op, ShouldEqual, "@>")
				So(and.Right.(*BinaryExpr).Op, ShouldEqual, "?|")
			})
		})

		Convey("When an array subscript and slice are parsed", func() {
			s, err := ParseOne("SELECT x[1], y[1:3] FROM t")
			Convey("Then index and slice nodes are produced", func() {
				So(err, ShouldBeNil)
				cols := s.(*SelectStmt).Columns
				So(cols[0].Expr.(*SubscriptExpr).Slice, ShouldBeFalse)
				So(cols[1].Expr.(*SubscriptExpr).Slice, ShouldBeTrue)
			})
		})

		Convey("When a multi-column UPDATE assignment is parsed", func() {
			s, err := ParseOne("UPDATE users SET (name, email) = ($1, $2) WHERE id = $3")
			Convey("Then the column list and value list align", func() {
				So(err, ShouldBeNil)
				a := s.(*UpdateStmt).Set[0]
				So(a.Columns, ShouldResemble, []string{"name", "email"})
				So(len(a.Values), ShouldEqual, 2)
			})
		})

		Convey("When a window frame with FILTER is parsed", func() {
			s, err := ParseOne("SELECT sum(x) FILTER (WHERE x > 0) OVER (ORDER BY t ROWS BETWEEN 1 PRECEDING AND CURRENT ROW) FROM s")
			Convey("Then filter, frame, and bounds are captured", func() {
				So(err, ShouldBeNil)
				fc := s.(*SelectStmt).Columns[0].Expr.(*FuncCall)
				So(fc.Filter, ShouldNotBeNil)
				So(fc.Over.Frame.Mode, ShouldEqual, "ROWS")
				So(fc.Over.Frame.Start.Kind, ShouldEqual, FramePreceding)
				So(fc.Over.Frame.End.Kind, ShouldEqual, FrameCurrentRow)
			})
		})

		Convey("When an aggregate ORDER BY is parsed", func() {
			s, err := ParseOne("SELECT string_agg(name, ',' ORDER BY name DESC) FROM t")
			Convey("Then the aggregate carries its own ORDER BY", func() {
				So(err, ShouldBeNil)
				fc := s.(*SelectStmt).Columns[0].Expr.(*FuncCall)
				So(len(fc.OrderBy), ShouldEqual, 1)
				So(fc.OrderBy[0].Desc, ShouldBeTrue)
			})
		})
	})
}

func TestExtendedFeatures(t *testing.T) {
	Convey("Given assorted PostgreSQL constructs", t, func() {
		Convey("When a quantified comparison '= ANY(...)' is parsed", func() {
			s, err := ParseOne("SELECT * FROM t WHERE id = ANY (ARRAY[1, 2, 3])")
			So(err, ShouldBeNil)
			aa := s.(*SelectStmt).Where.(*AnyAllExpr)
			So(aa.Any, ShouldBeTrue)
			So(aa.Right, ShouldHaveSameTypeAs, &ArrayExpr{})
		})

		Convey("When IS DISTINCT FROM is parsed", func() {
			s, err := ParseOne("SELECT * FROM t WHERE a IS DISTINCT FROM b")
			So(err, ShouldBeNil)
			So(s.(*SelectStmt).Where, ShouldHaveSameTypeAs, &IsDistinctExpr{})
		})

		Convey("When a bare VALUES statement is parsed", func() {
			s, err := ParseOne("VALUES (1, 'a'), (2, 'b')")
			So(err, ShouldBeNil)
			So(len(s.(*SelectStmt).Values), ShouldEqual, 2)
		})

		Convey("When GROUP BY ROLLUP is parsed", func() {
			s, err := ParseOne("SELECT a, sum(b) FROM t GROUP BY ROLLUP (a, b)")
			So(err, ShouldBeNil)
			g := s.(*SelectStmt).GroupBy[0].(*GroupingExpr)
			So(g.Kind, ShouldEqual, "ROLLUP")
			So(len(g.Args), ShouldEqual, 2)
		})

		Convey("When a set-returning function appears in FROM", func() {
			s, err := ParseOne("SELECT * FROM generate_series(1, 5) AS g (n)")
			So(err, ShouldBeNil)
			ft := s.(*SelectStmt).From[0].(*FuncTable)
			So(ft.Alias, ShouldEqual, "g")
			So(ft.Columns, ShouldResemble, []string{"n"})
		})

		Convey("When FETCH FIRST and FOR UPDATE are parsed", func() {
			s, err := ParseOne("SELECT * FROM t ORDER BY a FETCH FIRST 5 ROWS ONLY FOR UPDATE OF t NOWAIT")
			So(err, ShouldBeNil)
			sel := s.(*SelectStmt)
			So(sel.Limit.(*Literal).Val, ShouldEqual, "5")
			So(sel.Locking[0].Strength, ShouldEqual, "UPDATE")
			So(sel.Locking[0].Wait, ShouldEqual, "NOWAIT")
		})

		Convey("When a utility statement is parsed", func() {
			s, err := ParseOne("SET search_path TO public, app")
			So(err, ShouldBeNil)
			rs := s.(*RawStmt)
			So(rs.Keyword, ShouldEqual, "SET")
			So(rs.SQL, ShouldStartWith, "SET search_path")
		})

		Convey("When INSERT ... ON CONFLICT ... WHERE is parsed", func() {
			s, err := ParseOne("INSERT INTO t (a) VALUES (1) ON CONFLICT (a) WHERE a > 0 DO UPDATE SET a = 2 WHERE t.a < 5")
			So(err, ShouldBeNil)
			oc := s.(*InsertStmt).OnConflict
			So(oc.IndexWhere, ShouldNotBeNil)
			So(oc.UpdateWhere, ShouldNotBeNil)
		})

		Convey("When SELECT ... INTO is parsed", func() {
			s, err := ParseOne("SELECT a, b INTO TEMP newtab FROM src WHERE a > 0")
			So(err, ShouldBeNil)
			So(s.(*SelectStmt).Into.Name, ShouldEqual, "newtab")
		})

		Convey("When a table column-alias list is parsed", func() {
			s, err := ParseOne("SELECT * FROM t x (c0, c1) WHERE c0 = 1")
			So(err, ShouldBeNil)
			tn := s.(*SelectStmt).From[0].(*TableName)
			So(tn.Alias, ShouldEqual, "x")
			So(tn.ColumnAliases, ShouldResemble, []string{"c0", "c1"})
		})

		Convey("When COLLATE and a WINDOW clause are parsed", func() {
			s, err := ParseOne(`SELECT max(a COLLATE "C") OVER w FROM t WINDOW w AS (ORDER BY b)`)
			So(err, ShouldBeNil)
			sel := s.(*SelectStmt)
			So(len(sel.Window), ShouldEqual, 1)
			So(sel.Window[0].Name, ShouldEqual, "w")
		})

		Convey("When an escape string and a multidimensional array are parsed", func() {
			s, err := ParseOne(`SELECT E'a\'b', ARRAY[[1, 2], [3, 4]]`)
			So(err, ShouldBeNil)
			cols := s.(*SelectStmt).Columns
			So(cols[0].Expr.(*Literal).Kind, ShouldEqual, LitString)
			So(cols[1].Expr.(*ArrayExpr).Elements[0], ShouldHaveSameTypeAs, &ArrayExpr{})
		})

		Convey("When WITH appears inside a parenthesised subquery", func() {
			s, err := ParseOne("SELECT * FROM (WITH c AS (SELECT 1 AS n) SELECT n FROM c) s")
			So(err, ShouldBeNil)
			sub := s.(*SelectStmt).From[0].(*SubqueryTable)
			So(len(sub.Select.With), ShouldEqual, 1)
		})
	})
}

func TestDML(t *testing.T) {
	Convey("Given an INSERT with columns, VALUES and RETURNING", t, func() {
		Convey("When parsed", func() {
			s, err := ParseOne("INSERT INTO users (name, age) VALUES ('a', 1), ('b', 2) RETURNING id")
			Convey("Then rows and returning are modelled", func() {
				So(err, ShouldBeNil)
				ins := s.(*InsertStmt)
				So(ins.Columns, ShouldResemble, []string{"name", "age"})
				So(len(ins.Rows), ShouldEqual, 2)
				So(len(ins.Returning), ShouldEqual, 1)
			})
		})
	})

	Convey("Given an INSERT ... ON CONFLICT DO UPDATE", t, func() {
		Convey("When parsed", func() {
			s, err := ParseOne("INSERT INTO t (id, v) VALUES (1, 2) ON CONFLICT (id) DO UPDATE SET v = 3")
			Convey("Then the conflict clause carries the update", func() {
				So(err, ShouldBeNil)
				oc := s.(*InsertStmt).OnConflict
				So(oc.Targets, ShouldResemble, []string{"id"})
				So(len(oc.DoUpdate), ShouldEqual, 1)
			})
		})
	})

	Convey("Given an UPDATE with WHERE", t, func() {
		Convey("When parsed", func() {
			s, err := ParseOne("UPDATE accounts SET balance = balance - 10 WHERE id = $1")
			Convey("Then assignments and predicate are present", func() {
				So(err, ShouldBeNil)
				up := s.(*UpdateStmt)
				So(up.Set[0].Column, ShouldEqual, "balance")
				So(up.Where, ShouldNotBeNil)
			})
		})
	})

	Convey("Given a DELETE with WHERE", t, func() {
		Convey("When parsed", func() {
			s, err := ParseOne("DELETE FROM logs WHERE created_at < $1")
			Convey("Then the table and predicate are captured", func() {
				So(err, ShouldBeNil)
				del := s.(*DeleteStmt)
				So(del.Table.Name, ShouldEqual, "logs")
				So(del.Where, ShouldNotBeNil)
			})
		})
	})
}

func TestDepthGuard(t *testing.T) {
	Convey("Given pathologically nested input", t, func() {
		deep := "SELECT " + strings.Repeat("(", 200000) + "1" + strings.Repeat(")", 200000)
		Convey("When parsed", func() {
			_, err := Parse(deep)
			Convey("Then it returns an error instead of overflowing the stack", func() {
				So(err, ShouldNotBeNil)
			})
		})
	})

	Convey("Given a reasonably nested expression", t, func() {
		ok := "SELECT " + strings.Repeat("(", 50) + "1 + 2" + strings.Repeat(")", 50)
		Convey("When parsed", func() {
			_, err := Parse(ok)
			Convey("Then it parses normally (within the limit)", func() {
				So(err, ShouldBeNil)
			})
		})
	})
}

func TestErrors(t *testing.T) {
	Convey("Given malformed SQL", t, func() {
		cases := []string{
			"SELECT FROM",
			"SELECT * FROM",
			"INSERT users VALUES (1)",
			"SELECT * FROM t WHERE",
			"UPDATE t SET",
		}
		Convey("When each is parsed", func() {
			Convey("Then every case yields a syntax error, not a panic", func() {
				for _, c := range cases {
					_, err := ParseOne(c)
					So(err, ShouldNotBeNil)
				}
			})
		})
	})
}
