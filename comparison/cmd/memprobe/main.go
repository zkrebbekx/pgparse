// Command memprobe measures CPU and memory for the pure-Go engines: pgparse,
// GoSQLX, and the WebAssembly go-pgquery. The cgo pg_query_go engine lives in a
// separate binary (memprobe-cgo) because it bundles conflicting libpg_query
// symbols.
//
//	memprobe -engine pgparse|gosqlx|wasm [-conc N] [-iters N] [-corpus DIR]
package main

import (
	"flag"
	"fmt"
	"os"

	gosqlx "github.com/ajitpratap0/GoSQLX/pkg/gosqlx"
	wasmpg "github.com/wasilibs/go-pgquery"
	"github.com/zkrebbekx/pgparse"
	"github.com/zkrebbekx/pgparse/comparison/internal/probe"
)

func main() {
	engine := flag.String("engine", "pgparse", "pgparse|gosqlx|wasm")
	conc := flag.Int("conc", 1, "concurrent workers")
	iters := flag.Int("iters", 20, "passes over the corpus per worker")
	corpus := flag.String("corpus", "../../testdata/regress", "regression sql directory")
	flag.Parse()

	var parse func(string) (any, error)
	switch *engine {
	case "pgparse":
		parse = func(s string) (any, error) { return pgparse.Parse(s) }
	case "wasm":
		parse = func(s string) (any, error) { return wasmpg.Parse(s) }
	case "gosqlx":
		parse = func(s string) (any, error) {
			defer func() { recover() }()
			return gosqlx.Parse(s)
		}
	default:
		fmt.Fprintln(os.Stderr, "unknown engine:", *engine)
		os.Exit(2)
	}
	probe.Run(*engine, parse, *conc, *iters, *corpus)
}
