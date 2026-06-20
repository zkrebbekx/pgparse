package pgparse

import (
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
