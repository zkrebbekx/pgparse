// Package comparison benchmarks pgparse head-to-head against pganalyze's
// pg_query_go (the cgo binding around the real PostgreSQL parser) over the
// TPC-H query corpus.
//
// It lives in its own module so the root pgparse module stays cgo-free: nothing
// a normal pgparse consumer builds pulls in pg_query_go or a C toolchain.
//
// Run from this directory:
//
//	go test -bench=. -benchmem
package comparison

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	pgquery "github.com/pganalyze/pg_query_go/v5"
	"github.com/zkrebbekx/pgparse"
)

// loadCorpus loads the TPC-H queries that BOTH parsers accept, so each engine
// sees identical input. Queries either parser rejects (e.g. Q15's CREATE VIEW,
// which pgparse does not target) are excluded to keep the comparison fair.
func loadCorpus(tb testing.TB) []string {
	tb.Helper()
	files, err := filepath.Glob("../testdata/tpch/*.sql")
	if err != nil || len(files) == 0 {
		tb.Skipf("no corpus found: %v", err)
	}
	sort.Strings(files)
	var qs []string
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			tb.Fatal(err)
		}
		sql := string(b)
		_, e1 := pgparse.Parse(sql)
		_, e2 := pgquery.Parse(sql)
		if e1 == nil && e2 == nil {
			qs = append(qs, sql)
		}
	}
	if len(qs) == 0 {
		tb.Skip("no commonly-parseable queries")
	}
	return qs
}

func BenchmarkCorpus_pgparse(b *testing.B) {
	qs := loadCorpus(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, q := range qs {
			if _, err := pgparse.Parse(q); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkCorpus_pg_query_go(b *testing.B) {
	qs := loadCorpus(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, q := range qs {
			if _, err := pgquery.Parse(q); err != nil {
				b.Fatal(err)
			}
		}
	}
}

// TestReport prints a per-query and aggregate latency/allocation comparison.
func TestReport(t *testing.T) {
	qs := loadCorpus(t)
	t.Logf("%d TPC-H queries parsed by both engines", len(qs))

	pp := func() {
		for _, q := range qs {
			_, _ = pgparse.Parse(q)
		}
	}
	pg := func() {
		for _, q := range qs {
			_, _ = pgquery.Parse(q)
		}
	}

	ppRes := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			pp()
		}
	})
	pgRes := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			pg()
		}
	})

	n := int64(len(qs))
	t.Logf("pgparse     : %8.2f ns/query  %6d B/query  %4d allocs/query",
		float64(ppRes.NsPerOp())/float64(n),
		ppRes.AllocedBytesPerOp()/n,
		ppRes.AllocsPerOp()/n)
	t.Logf("pg_query_go : %8.2f ns/query  %6d B/query  %4d allocs/query",
		float64(pgRes.NsPerOp())/float64(n),
		pgRes.AllocedBytesPerOp()/n,
		pgRes.AllocsPerOp()/n)
	t.Logf("pgparse speedup: %.1fx", float64(pgRes.NsPerOp())/float64(ppRes.NsPerOp()))
}
