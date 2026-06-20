# comparison

Benchmarks and a completeness matrix comparing [`pgparse`](..) against
[`pg_query_go`](https://github.com/pganalyze/pg_query_go) (the cgo binding around
the real PostgreSQL parser) and [GoSQLX](https://github.com/ajitpratap0/GoSQLX)
(another pure-Go parser), over the **TPC-H** query corpus in
[`../testdata/tpch`](../testdata/tpch) plus a curated feature set.

Versions under test (pinned in `go.mod`): `pg_query_go` **v6.2.2**, GoSQLX
**v1.14.0**, and (for `make memcompare`) the wasm
[`go-pgquery`](https://github.com/wasilibs/go-pgquery) on wazero v1.12.0.

- `BenchmarkCorpus_*` / `TestReport` — latency, bytes, allocs: pgparse vs pg_query_go.
- `TestCompleteness` — a 23-feature acceptance matrix across all three engines.
- `TestRegressCompleteness` — breadth over a subset of the **PostgreSQL
  regression suite** ([`testdata/regress`](testdata/regress)): each file split
  into statements, scored as the fraction of `pg_query_go`-valid statements that
  pgparse / GoSQLX also accept. ~7,985 valid statements; pgparse 97.8%, GoSQLX 48.4%.
- `cmd/memprobe` + `cmd/memprobe-cgo` — CPU/RAM probes used by `make memcompare`,
  comparing pgparse, GoSQLX, the cgo `pg_query_go`, and the wasm
  [`go-pgquery`](https://github.com/wasilibs/go-pgquery) on latency and peak RSS,
  single-threaded and concurrent. (cgo and wasm bundle conflicting libpg_query
  symbols, so each is a separate binary.)

This is a **separate Go module** on purpose: `pg_query_go` requires cgo and a C
toolchain, and the root `pgparse` module must stay cgo-free. Nothing here is
imported by the library itself.

## Run

```bash
# from this directory
CGO_CFLAGS="-DHAVE_STRCHRNUL -Wno-error" go test -bench=Corpus -benchmem
CGO_CFLAGS="-DHAVE_STRCHRNUL -Wno-error" go test -run=TestReport -v
```

`CGO_CFLAGS` works around a `strchrnul` redefinition between recent macOS SDKs
and libpg_query's bundled copy; it is harmless on other platforms.

## What is measured

`loadCorpus` keeps only the queries that **both** engines accept, so each parser
sees identical input. `BenchmarkCorpus_pgparse` and `BenchmarkCorpus_pg_query_go`
then parse that same set; `TestReport` prints a per-query latency, bytes, and
allocation summary plus the speedup.

## Fairness note

`pg_query_go` produces the full PostgreSQL node tree for every statement kind;
its cost includes the cgo crossing and protobuf (de)serialization of that tree.
pgparse parses a pragmatic DML subset into a lean Go AST. The benchmark reflects
the real cost of obtaining a usable parse tree in Go from each — not a claim
that the two produce equivalent output.
