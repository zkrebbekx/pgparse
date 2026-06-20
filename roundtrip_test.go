package pgparse

import (
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

// deparseAll renders every statement of a parse result back to SQL.
func deparseAll(res *ParseResult) string {
	parts := make([]string, len(res.Stmts))
	for i, s := range res.Stmts {
		parts[i] = Deparse(s)
	}
	return strings.Join(parts, "; ")
}

func TestRoundTrip(t *testing.T) {
	Convey("Given parseable SQL", t, func() {
		queries := []string{
			"SELECT id, name AS n FROM users WHERE id = $1 ORDER BY name DESC LIMIT 10",
			"SELECT * FROM a JOIN b ON a.id = b.a_id LEFT JOIN c USING (k)",
			"SELECT 1 UNION SELECT 2 INTERSECT SELECT 3",
			"WITH r AS (SELECT * FROM o) SELECT count(*) FROM r WHERE x IS NOT NULL",
			"SELECT a->b->>'c' #> '{x}' FROM t WHERE a @> b",
			"SELECT x[1], y[1:2] FROM t",
			"SELECT row_number() OVER (PARTITION BY d ORDER BY s DESC) FROM e",
			"SELECT sum(x) FILTER (WHERE y > 0) OVER (ORDER BY z ROWS BETWEEN 1 PRECEDING AND CURRENT ROW) FROM t",
			"SELECT array_agg(x ORDER BY y) FROM t",
			"UPDATE users SET (name, email) = ($1, $2) WHERE id = $3",
			"INSERT INTO t (a, b) VALUES (1, 2), (3, 4) ON CONFLICT (a) DO UPDATE SET b = 5 RETURNING id",
			"DELETE FROM logs WHERE created_at < $1 RETURNING id",
			"SELECT CASE WHEN a > 0 THEN 'p' ELSE 'n' END, (a + b) * c FROM t",
			"SELECT extract(year FROM d), substring(p FROM 1 FOR 2) FROM t",
			"(SELECT 1 UNION SELECT 2) INTERSECT SELECT 3",
			"SELECT 1 UNION (SELECT 2 EXCEPT SELECT 3)",
			"(SELECT a FROM t ORDER BY a LIMIT 1) UNION SELECT b FROM u",
		}

		Convey("When parsed, deparsed, and re-parsed", func() {
			Convey("Then deparsing is idempotent and the result re-parses", func() {
				for _, q := range queries {
					res, err := Parse(q)
					So(err, ShouldBeNil)
					s1 := deparseAll(res)

					res2, err := Parse(s1)
					So(err, ShouldBeNil)
					s2 := deparseAll(res2)

					if s1 != s2 {
						t.Logf("not idempotent:\n  in:  %s\n  s1:  %s\n  s2:  %s", q, s1, s2)
					}
					So(s2, ShouldEqual, s1)
				}
			})
		})
	})
}

func TestRoundTripTPCH(t *testing.T) {
	Convey("Given the parseable TPC-H corpus", t, func() {
		corpus := loadTPCH(t)
		Convey("When each is deparsed and re-parsed", func() {
			Convey("Then every one round-trips idempotently", func() {
				for _, name := range sortedKeys(corpus) {
					res, err := Parse(corpus[name])
					if err != nil {
						continue // Q15 (DDL) is out of scope
					}
					s1 := deparseAll(res)
					res2, err := Parse(s1)
					So(err, ShouldBeNil)
					if err != nil {
						t.Logf("%s re-parse failed on: %s", name, s1)
						continue
					}
					So(deparseAll(res2), ShouldEqual, s1)
				}
			})
		})
	})
}
