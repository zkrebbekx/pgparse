package comparison

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	pgquery "github.com/pganalyze/pg_query_go/v5"
)

// TestRegressCompleteness measures pgparse (and GoSQLX) completeness over the
// PostgreSQL regression-test SQL corpus — the same body of SQL that libpg_query
// (and therefore pg_query_go) is validated against.
//
// Method: each file is split into statements with pg_query_go's scanner. A
// statement counts only if pg_query_go — the real PostgreSQL parser — accepts
// it, which filters out psql meta-commands and the suite's deliberately-invalid
// statements. The score is the fraction of those genuinely-valid statements each
// pure-Go parser also accepts.
func TestRegressCompleteness(t *testing.T) {
	files, err := filepath.Glob("testdata/regress/*.sql")
	if err != nil || len(files) == 0 {
		t.Skipf("no regression corpus: %v", err)
	}
	sort.Strings(files)

	type tally struct{ total, pp, gx int }
	per := map[string]*tally{}
	overall := &tally{}

	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		stmts, err := pgquery.SplitWithScanner(string(raw), true)
		if err != nil {
			t.Logf("split %s: %v", filepath.Base(file), err)
			continue
		}
		ty := &tally{}
		for _, stmt := range stmts {
			s := strings.TrimSpace(stmt)
			if s == "" || strings.HasPrefix(s, "\\") {
				continue // empty or psql meta-command
			}
			if !safeParse(pgParse, s) {
				continue // not valid PostgreSQL (intentional error, psql var, ...)
			}
			ty.total++
			if safeParse(ppParse, s) {
				ty.pp++
			}
			if safeParse(gosParse, s) {
				ty.gx++
			}
		}
		per[filepath.Base(file)] = ty
		overall.total += ty.total
		overall.pp += ty.pp
		overall.gx += ty.gx
	}

	pct := func(n, d int) float64 {
		if d == 0 {
			return 0
		}
		return 100 * float64(n) / float64(d)
	}

	t.Logf("%-20s | %6s | %-15s | %-15s", "file", "valid", "pgparse", "GoSQLX")
	t.Logf("%s", strings.Repeat("-", 68))
	names := make([]string, 0, len(per))
	for n := range per {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		ty := per[n]
		t.Logf("%-20s | %6d | %4d (%5.1f%%) | %4d (%5.1f%%)",
			n, ty.total, ty.pp, pct(ty.pp, ty.total), ty.gx, pct(ty.gx, ty.total))
	}
	t.Logf("%s", strings.Repeat("-", 68))
	t.Logf("%-20s | %6d | %4d (%5.1f%%) | %4d (%5.1f%%)",
		"TOTAL", overall.total, overall.pp, pct(overall.pp, overall.total),
		overall.gx, pct(overall.gx, overall.total))

	fmt.Printf("regress completeness over %d valid statements: pgparse=%.1f%% gosqlx=%.1f%%\n",
		overall.total, pct(overall.pp, overall.total), pct(overall.gx, overall.total))
}
