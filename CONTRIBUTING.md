# Contributing to pgparse

Thanks for your interest. pgparse is a pure-Go PostgreSQL parser with no runtime
dependencies; contributions that keep it small, fast, and dependency-free are
very welcome.

## Getting started

```bash
git clone https://github.com/zkrebbekx/pgparse
cd pgparse
go test ./...        # unit, round-trip, and corpus tests
go vet ./...
gofmt -l .           # should print nothing
```

The root module has no third-party runtime dependencies (`goconvey` is a
test-only dependency). The `comparison/` and `playground/` directories are
separate modules and are not part of the library's dependency graph.

## Conventions

- **Tests are BDD** with [goconvey](https://github.com/smartystreets/goconvey):
  write `Given / When / Then` `Convey` blocks, not bare table tests.
- **Format with `gofmt`**; CI rejects unformatted code.
- **Every change to the grammar gets a test** — a parse assertion and, where it
  produces structured output, a round-trip case in `roundtrip_test.go`.
- **The parser must never panic.** `Parse` recovers internally, and `FuzzParse`
  drives the unguarded path; run `go test -fuzz=FuzzParse -fuzztime=30s` after
  parser changes.
- **Keep it concurrency-safe.** No package-level mutable state on the parse path
  (the keyword tables are read-only). `concurrent_test.go` runs under `-race`.

## Benchmarks and comparisons

```bash
make bench         # parser microbenchmarks
make compare       # vs pg_query_go (cgo) and GoSQLX — needs a C toolchain
make memcompare    # CPU/RAM vs cgo and the wasm go-pgquery build
```

## Scope

pgparse targets the SQL that real applications write, not the entire PostgreSQL
grammar. New DML/query/DDL coverage is welcome; full PL/pgSQL, `MERGE`, and exact
`pg_query` node-tree parity are out of scope (use `pg_query_go` for those).

## Reporting issues

Security issues: see [SECURITY.md](SECURITY.md) (report privately). For bugs,
include the SQL, what you expected, and what pgparse produced.
