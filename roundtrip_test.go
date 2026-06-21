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
			"WITH upd AS (UPDATE t SET a = 1 WHERE id = $1 RETURNING id, a) SELECT * FROM upd",
			"WITH moved AS (DELETE FROM t WHERE x < $1 RETURNING *) INSERT INTO archive SELECT * FROM moved",
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
			"CREATE TABLE t (id bigint PRIMARY KEY, name text NOT NULL DEFAULT 'x', CONSTRAINT u UNIQUE (name))",
			"CREATE OR REPLACE VIEW v AS SELECT a, b FROM t WHERE a > 0",
			"CREATE UNIQUE INDEX i ON t USING btree (a, b DESC) WHERE a IS NOT NULL",
			"DROP TABLE IF EXISTS a, b CASCADE",
			"ALTER TABLE t ADD COLUMN c int, DROP COLUMN d, ALTER COLUMN e TYPE text, RENAME COLUMN f TO g",
			"VALUES (1, 'a'), (2, 'b')",
			"SELECT * FROM t WHERE id = ANY (ARRAY[1, 2, 3]) AND a IS DISTINCT FROM b",
			"SELECT a, sum(b) FROM t GROUP BY ROLLUP (a, b), GROUPING SETS ((a), ())",
			"SELECT * FROM generate_series(1, 5) WITH ORDINALITY AS g (n, o)",
			"SELECT * FROM t ORDER BY a USING > LIMIT 5 FOR UPDATE OF t SKIP LOCKED",
			"SELECT x[1] FROM t WHERE a -> 'k' @> b",
			"INSERT INTO t (a) VALUES (1) ON CONFLICT (a) DO UPDATE SET a = 2 WHERE t.a < 5",
			"UPDATE t SET tags[1] = 'x', (a, b) = (1, 2) WHERE id = $1",
			"SELECT a, b INTO TEMP newtab FROM src WHERE a > 0",
			"SELECT * FROM t AS x (c0, c1), generate_series(1, 3) g WHERE c0 = 1",
			`SELECT max(a) OVER w, sum(b) FROM t WINDOW w AS (PARTITION BY c ORDER BY d)`,
			"SELECT * FROM (WITH c AS (SELECT 1 AS n) SELECT n FROM c) s",
			"SELECT a, sum(b) FROM t GROUP BY CUBE (a) ORDER BY a USING <",
			"WITH RECURSIVE t (n) AS (SELECT 1 UNION ALL SELECT n + 1 FROM t) SEARCH DEPTH FIRST BY n SET ord SELECT * FROM t",
			"SELECT sum(x) OVER (ORDER BY t ROWS CURRENT ROW EXCLUDE TIES) FROM s",
			"SELECT -a + +b, ~c FROM t",
			"SELECT * FROM t WHERE a IS NOT TRUE AND b IS FALSE AND c IS NOT DISTINCT FROM d AND e IS NULL",
			"SELECT x COLLATE \"C\", position('a' IN b), trim('x'), extract(year FROM d) FROM t",
			"WITH RECURSIVE t (n) AS (SELECT 1 UNION ALL SELECT n + 1 FROM t) SEARCH BREADTH FIRST BY n SET ord CYCLE n SET cyc USING pth SELECT * FROM t",
			"SELECT CAST(a AS int), ARRAY(SELECT id FROM t) FROM x",
			`SELECT a COLLATE pg_catalog."C" FROM t`,
			"ALTER TABLE t ALTER COLUMN c SET DEFAULT 0, ALTER COLUMN d DROP DEFAULT, ALTER COLUMN e SET NOT NULL, ALTER COLUMN f DROP NOT NULL",
			"DELETE FROM t USING u WHERE t.id = u.id",
			"DROP VIEW v; DROP SEQUENCE s; DROP INDEX i",
			"WITH RECURSIVE t (n) AS (SELECT 1 UNION ALL SELECT n + 1 FROM t) CYCLE n SET cyc TO true DEFAULT false USING pth SELECT * FROM t",
			// v1.1 additions
			"INSERT INTO t (f2[1], f2[2], f4[1].if2[1]) VALUES (1, 2, 3)",
			`INSERT INTO t ("Foo", "Bar") VALUES (1, 2) ON CONFLICT ("Foo") DO NOTHING`,
			"SELECT (x).a, (arr[1]).b, (f()).* FROM t",
			"INSERT INTO t (a) VALUES (1) ON CONFLICT (lower(name)) DO NOTHING",
			"INSERT INTO t (a) VALUES (1) ON CONFLICT (a, b) DO NOTHING",
			"SELECT * FROM a JOIN b USING (id) AS j",
			"SELECT * FROM (a JOIN b USING (id)) AS j",
			"SELECT * FROM jsonb_to_record('{}') AS x (ia _int4, j json)",
			"SELECT * FROM jsonb_to_record('{}') AS (a int, b text)",
			"SELECT count(*) AS desc FROM t",
			"SELECT f1 <@ circle '<(0,0),5>' FROM t",
			`SELECT "a""b", ARRAY[1, 2], ARRAY(SELECT 1) FROM t`,
			"SELECT CAST(a AS numeric(10, 2)) FROM t",
			"SELECT * FROM t FOR UPDATE OF t NOWAIT",
			"SELECT * FROM t FOR SHARE SKIP LOCKED",
			"SELECT * FROM t FOR NO KEY UPDATE",
			"SELECT * FROM t FOR KEY SHARE",
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
						continue // any query outside the supported subset
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
