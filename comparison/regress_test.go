package comparison

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	pgquery "github.com/pganalyze/pg_query_go/v5"
	"github.com/zkrebbekx/pgparse"
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

// loadRegressShared returns the regression-suite statements that BOTH pgparse
// and pg_query_go accept, so a latency comparison feeds each engine identical,
// real-world input across the whole supported grammar (not just TPC-H).
func loadRegressShared(tb testing.TB) []string {
	tb.Helper()
	files, err := filepath.Glob("testdata/regress/*.sql")
	if err != nil || len(files) == 0 {
		tb.Skipf("no regression corpus: %v", err)
	}
	var shared []string
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			tb.Fatal(err)
		}
		stmts, err := pgquery.SplitWithScanner(string(raw), true)
		if err != nil {
			continue
		}
		for _, stmt := range stmts {
			s := strings.TrimSpace(stmt)
			if s == "" || strings.HasPrefix(s, "\\") {
				continue
			}
			if safeParse(pgParse, s) && safeParse(ppParse, s) {
				shared = append(shared, s)
			}
		}
	}
	return shared
}

func BenchmarkRegress_pgparse(b *testing.B) {
	qs := loadRegressShared(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, q := range qs {
			_, _ = pgparse.Parse(q)
		}
	}
}

func BenchmarkRegress_pg_query_go(b *testing.B) {
	qs := loadRegressShared(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, q := range qs {
			_, _ = pgquery.Parse(q)
		}
	}
}

// TestRegressSpeed prints a per-statement latency/allocation comparison over the
// shared regression corpus.
func TestRegressSpeed(t *testing.T) {
	qs := loadRegressShared(t)
	t.Logf("%d regression statements parsed by both engines", len(qs))

	ppRes := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			for _, q := range qs {
				_, _ = pgparse.Parse(q)
			}
		}
	})
	pgRes := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			for _, q := range qs {
				_, _ = pgquery.Parse(q)
			}
		}
	})

	n := int64(len(qs))
	t.Logf("pgparse     : %8.0f ns/stmt  %6d B/stmt  %4d allocs/stmt",
		float64(ppRes.NsPerOp())/float64(n), ppRes.AllocedBytesPerOp()/n, ppRes.AllocsPerOp()/n)
	t.Logf("pg_query_go : %8.0f ns/stmt  %6d B/stmt  %4d allocs/stmt",
		float64(pgRes.NsPerOp())/float64(n), pgRes.AllocedBytesPerOp()/n, pgRes.AllocsPerOp()/n)
	t.Logf("pgparse speedup: %.1fx", float64(pgRes.NsPerOp())/float64(ppRes.NsPerOp()))
	fmt.Printf("regress speed over %d shared statements: pgparse %.1fx faster than pg_query_go\n",
		n, float64(pgRes.NsPerOp())/float64(ppRes.NsPerOp()))
}
