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
		"((((SELECT 1))))",
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
