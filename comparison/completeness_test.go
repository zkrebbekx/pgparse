package comparison

import (
	"fmt"
	"testing"

	gosqlx "github.com/ajitpratap0/GoSQLX/pkg/gosqlx"
	pgquery "github.com/pganalyze/pg_query_go/v5"
	"github.com/zkrebbekx/pgparse"
)

// safeParse runs a parser, converting both errors and panics into a simple
// accepted/rejected boolean so one engine crashing cannot fail the matrix.
func safeParse(fn func(string) error, sql string) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	return fn(sql) == nil
}

func ppParse(sql string) error  { _, err := pgparse.Parse(sql); return err }
func pgParse(sql string) error  { _, err := pgquery.Parse(sql); return err }
func gosParse(sql string) error { _, err := gosqlx.Parse(sql); return err }

// feature is one labelled query and the engines' acceptance of it.
type feature struct {
	name string
	sql  string
}

var features = []feature{
	{"simple select", `SELECT id, name FROM users WHERE id = $1`},
	{"multi join", `SELECT * FROM a JOIN b ON a.id = b.a_id LEFT JOIN c USING (k)`},
	{"cte", `WITH r AS (SELECT * FROM o) SELECT * FROM r`},
	{"recursive cte", `WITH RECURSIVE t AS (SELECT 1 UNION ALL SELECT n+1 FROM t) SELECT * FROM t`},
	{"set-op precedence", `SELECT 1 UNION SELECT 2 INTERSECT SELECT 3`},
	{"window partition", `SELECT row_number() OVER (PARTITION BY d ORDER BY s DESC) FROM e`},
	{"window frame", `SELECT sum(x) OVER (ORDER BY t ROWS BETWEEN 1 PRECEDING AND CURRENT ROW) FROM s`},
	{"filter clause", `SELECT count(*) FILTER (WHERE x > 0) FROM t`},
	{"within group", `SELECT percentile_cont(0.5) WITHIN GROUP (ORDER BY x) FROM t`},
	{"distinct on", `SELECT DISTINCT ON (a) a, b FROM t ORDER BY a, b`},
	{"json arrows", `SELECT a -> 'k' ->> 'm' FROM t`},
	{"jsonb operators", `SELECT * FROM t WHERE meta @> '{}' AND tags ?| arr`},
	{"array subscript", `SELECT x[1], y[1:3] FROM t`},
	{"multi-col UPDATE", `UPDATE users SET (name, email) = ($1, $2) WHERE id = $3`},
	{"upsert returning", `INSERT INTO t (a, b) VALUES (1, 2) ON CONFLICT (a) DO UPDATE SET b = 3 RETURNING id`},
	{"case + cast", `SELECT CASE WHEN a > 0 THEN 'p' ELSE 'n' END, a::numeric(10,2) FROM t`},
	{"in subquery", `SELECT * FROM t WHERE id IN (SELECT id FROM u)`},
	{"exists", `SELECT * FROM t WHERE EXISTS (SELECT 1 FROM u WHERE u.t = t.id)`},
	{"extract/substring", `SELECT extract(year FROM d), substring(p FROM 1 FOR 2) FROM t`},
	{"typed literal + interval", `SELECT date '1998-12-01' - interval '90' day`},
	{"aggregate order by", `SELECT string_agg(name, ',' ORDER BY name) FROM t`},
	{"lateral join", `SELECT * FROM t, LATERAL (SELECT * FROM u WHERE u.t = t.id) s`},
	{"ddl create table", `CREATE TABLE t (id int PRIMARY KEY, name text NOT NULL)`},
}

// TestCompleteness prints a feature-by-feature acceptance matrix across the
// three engines and the totals. pg_query_go (the real PostgreSQL parser) is the
// fidelity baseline; pgparse and GoSQLX are the pure-Go contenders.
func TestCompleteness(t *testing.T) {
	var pp, pg, gx int
	mark := func(b bool) string {
		if b {
			return " ✓ "
		}
		return " ✗ "
	}
	t.Logf("%-26s | pgparse | pg_query | GoSQLX", "feature")
	t.Logf("%s", "---------------------------+---------+----------+-------")
	for _, f := range features {
		a := safeParse(ppParse, f.sql)
		b := safeParse(pgParse, f.sql)
		c := safeParse(gosParse, f.sql)
		if a {
			pp++
		}
		if b {
			pg++
		}
		if c {
			gx++
		}
		t.Logf("%-26s |   %s   |    %s    |   %s", f.name, mark(a), mark(b), mark(c))
	}
	n := len(features)
	t.Logf("%s", "---------------------------+---------+----------+-------")
	t.Logf("%-26s |  %2d/%2d  |  %2d/%2d   |  %2d/%2d",
		"TOTAL", pp, n, pg, n, gx, n)
	fmt.Printf("completeness over %d features: pgparse=%d pg_query=%d gosqlx=%d\n", n, pp, pg, gx)
}
