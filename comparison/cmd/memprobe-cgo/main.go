// Command memprobe-cgo measures CPU and memory for the cgo pg_query_go engine
// (the real PostgreSQL parser). It is a separate binary from memprobe because
// pg_query_go and the WebAssembly go-pgquery bundle conflicting libpg_query
// symbols.
//
//	memprobe-cgo [-conc N] [-iters N] [-corpus DIR]
package main

import (
	"flag"

	pgcgo "github.com/pganalyze/pg_query_go/v6"
	"github.com/zkrebbekx/pgparse/comparison/internal/probe"
)

func main() {
	conc := flag.Int("conc", 1, "concurrent workers")
	iters := flag.Int("iters", 20, "passes over the corpus per worker")
	corpus := flag.String("corpus", "../../testdata/regress", "regression sql directory")
	flag.Parse()
	probe.Run("pgquery", func(s string) (any, error) { return pgcgo.Parse(s) }, *conc, *iters, *corpus)
}
