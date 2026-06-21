package pgparse

import (
	"os"
	"path/filepath"
	"testing"
)

// FuzzParse drives the unguarded parser with arbitrary input. The contract is
// simple: parsing may return an error, but it must never panic and must always
// terminate. The fuzzer is seeded with the TPC-H corpus and a spread of tricky
// fragments.
func FuzzParse(f *testing.F) {
	seeds := []string{
		"", " ", ";", ";;;",
		"SELECT", "SELECT 1", "SELECT * FROM t",
		"SELECT a->b->>'c' #> '{x}' FROM t WHERE a @> b AND c ?| d",
		"SELECT x[1], y[1:2], z[:3] FROM t",
		"UPDATE t SET (a, b) = (1, 2) WHERE id = $1",
		"SELECT sum(x) FILTER (WHERE y > 0) OVER (ORDER BY z ROWS BETWEEN 1 PRECEDING AND CURRENT ROW)",
		"WITH r AS (SELECT 1) SELECT * FROM r",
		"WITH u AS (UPDATE t SET a=1 RETURNING *) SELECT * FROM u",
		"WITH d AS (DELETE FROM t RETURNING id) INSERT INTO x SELECT * FROM d",
		"CREATE TABLE t (id int PRIMARY KEY, name text NOT NULL, CHECK (id > 0))",
		"CREATE OR REPLACE VIEW v AS SELECT 1",
		"CREATE UNIQUE INDEX i ON t USING gin (a) WHERE b",
		"ALTER TABLE t ADD COLUMN c int, DROP CONSTRAINT x CASCADE",
		"DROP VIEW IF EXISTS a, b RESTRICT",
		"((((SELECT 1))))",
		// prefix/array recursion vectors that bypass parseExpr's depth guard
		"SELECT NOT NOT NOT NOT x",
		"SELECT - - - - 1", "SELECT + + + 1", "SELECT ~ ~ ~ 1",
		"SELECT ARRAY[[[[1]]]]",
		"SELECT '''", "SELECT $tag$", "/* unclosed",
		"SELECT 1 UNION SELECT 2 INTERSECT SELECT 3 EXCEPT SELECT 4",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	if files, err := filepath.Glob("testdata/tpch/*.sql"); err == nil {
		for _, file := range files {
			if b, err := os.ReadFile(file); err == nil {
				f.Add(string(b))
			}
		}
	}

	f.Fuzz(func(t *testing.T, sql string) {
		// Unguarded path: a panic here fails the fuzz test.
		_, _ = parseInternal(sql)
	})
}
