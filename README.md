# pgparse

A **pure-Go PostgreSQL SQL parser**. No cgo. No WebAssembly runtime. No native
library to link. It compiles into any Go program as ordinary source and imposes
**zero per-process memory overhead at startup**.

It exists to fill the gap between the two common options:

| Approach | Cost |
|---|---|
| [`pg_query_go`](https://github.com/pganalyze/pg_query_go) | Wraps the real Postgres parser via **cgo** — needs a C toolchain, cross-compilation pain, and a non-trivial binary. |
| WebAssembly builds of `libpg_query` | Pure Go to *call*, but the wasm runtime allocates a large linear memory region **on startup** for every instance. |
| **pgparse** | Hand-written scanner + recursive-descent parser. Pure Go, zero deps, no startup allocation, low per-parse allocation. |

`pgparse` targets a **pragmatic, high-coverage subset** of PostgreSQL DML rather
than the entire grammar — see [Coverage](#coverage). If you need every Postgres
node exactly, use `pg_query_go`. If you need to parse the SQL that real apps
actually write, fast, with no cgo, this is for you.

Requires **Go 1.20+**. Zero runtime dependencies (`goconvey` is a test-only dep).

## Install

```bash
go get github.com/zkrebbekx/pgparse
```

## Usage

```go
package main

import (
	"fmt"
	"log"

	"github.com/zkrebbekx/pgparse"
)

func main() {
	res, err := pgparse.Parse(`
		SELECT u.id, u.name, count(o.id) AS orders
		FROM users u
		LEFT JOIN orders o ON o.user_id = u.id
		WHERE u.active = true
		GROUP BY u.id, u.name
		HAVING count(o.id) > 0
		ORDER BY orders DESC
		LIMIT 10
	`)
	if err != nil {
		log.Fatal(err)
	}

	sel := res.Stmts[0].(*pgparse.SelectStmt)
	fmt.Println("columns:", len(sel.Columns))
	fmt.Println("from:", sel.From[0].(*pgparse.JoinExpr).Kind)
}
```

Single-statement helper:

```go
stmt, err := pgparse.ParseOne("DELETE FROM logs WHERE created_at < $1")
del := stmt.(*pgparse.DeleteStmt)
```

Token stream only (e.g. for highlighting):

```go
toks, _ := pgparse.Tokenize("SELECT 1 + 2")
```

## Coverage

**Statements**

- `SELECT` — projection with aliases, `DISTINCT` / `DISTINCT ON`, `FROM` with
  comma and explicit joins (`INNER`, `LEFT`, `RIGHT`, `FULL`, `CROSS`, `OUTER`,
  `ON` / `USING`, `LATERAL` subqueries), `WHERE`, `GROUP BY`, `HAVING`,
  `ORDER BY` (`ASC`/`DESC`, `NULLS FIRST`/`LAST`), `LIMIT`, `OFFSET`
- `WITH` CTEs (incl. `RECURSIVE`) and subqueries in `FROM` / expressions
- Set operations: `UNION` / `INTERSECT` / `EXCEPT` (`ALL`), with correct
  precedence (`INTERSECT` binds tighter) and a tail that binds to the whole
  expression
- Window functions: `OVER (PARTITION BY ... ORDER BY ...)`, frame clauses
  (`ROWS`/`RANGE`/`GROUPS ... BETWEEN ... AND ...`, `EXCLUDE`), named windows
  (`OVER w`), `FILTER (WHERE ...)`, `WITHIN GROUP (ORDER BY ...)`, and aggregate
  `ORDER BY` (`array_agg(x ORDER BY y)`)
- `INSERT` — column lists, multi-row `VALUES`, `INSERT ... SELECT`,
  `ON CONFLICT (...) DO NOTHING | DO UPDATE SET ...`, `RETURNING`
- `UPDATE` — single and multi-column `SET (a, b) = (v1, v2)` / `= (SELECT ...)`,
  `FROM`, `WHERE`, `RETURNING`
- `DELETE` — `USING`, `WHERE`, `RETURNING`
- **DDL** — `CREATE TABLE` (column + table constraints: `PRIMARY KEY`,
  `NOT NULL`, `UNIQUE`, `DEFAULT`, `CHECK`, `REFERENCES`/`FOREIGN KEY`,
  `IF NOT EXISTS`, `TEMP`), `CREATE [OR REPLACE] VIEW`, `CREATE [UNIQUE] INDEX`
  (`USING`, partial `WHERE`, `DESC`), `DROP TABLE/VIEW/INDEX` (`IF EXISTS`,
  `CASCADE`), `ALTER TABLE` (`ADD`/`DROP COLUMN`, `ADD`/`DROP CONSTRAINT`,
  `ALTER COLUMN … TYPE`/`SET`/`DROP DEFAULT`/`NOT NULL`, `RENAME`)
- Query features — `VALUES` lists (standalone, in `FROM`, as set-op operands),
  set-returning functions in `FROM` (`WITH ORDINALITY`), `LATERAL` and `NATURAL`
  joins, `GROUP BY` `ROLLUP`/`CUBE`/`GROUPING SETS`, `ORDER BY … USING`,
  `FETCH FIRST … ROWS ONLY`/`WITH TIES`, `FOR UPDATE`/`SHARE` locking
- Expression extras — quantified `op ANY`/`ALL (array | subquery)`, `ARRAY[…]`
  and `ARRAY(subquery)`, `IS [NOT] DISTINCT FROM`, row/tuple constructors, the
  full open operator class (geometric, range, text-search operators)
- **Utility & admin statements** (`SET`, `SHOW`, `COPY`, `GRANT`/`REVOKE`,
  `ANALYZE`, `VACUUM`, `EXPLAIN`, transaction control, `TRUNCATE`, `COMMENT`,
  `CREATE TYPE`/`SEQUENCE`/`FUNCTION`/…, and DDL options pgparse does not model)
  are recognised and validated as `RawStmt` — leading keyword plus verbatim text

**Expressions**

- Logical (`AND` / `OR` / `NOT`), comparison, arithmetic, `||` concatenation
  — all with correct precedence and associativity
- JSON/array operators (`->` `->>` `#>` `#>>` `@>` `<@` `?` `?|` `?&`), bitwise
  (`&` `|` `#` `<<` `>>` `~`), and regex match (`~` `~*` `!~` `!~*`); array
  subscript and slice (`a[1]`, `a[1:3]`, `a[:2]`)
- `CASE` (simple + searched), `CAST(x AS t)` and `x::t`, typed literals
  (`date '...'`), `INTERVAL '90' day`
- `IN (list | subquery)`, `BETWEEN`, `LIKE` / `ILIKE`, `IS [NOT] NULL/TRUE/FALSE`
- `EXISTS (...)`, scalar subqueries, function calls (`DISTINCT`, `count(*)`),
  SQL special functions (`extract`, `substring`, `position`, `trim`, `overlay`)
- Literals (string with `''` escapes, dollar-quoted, int/float, bool, NULL),
  positional parameters `$n`, qualified names `schema.table.column`, `table.*`

**Out of scope (use `pg_query_go`):** `MERGE`, `GRANT`/`REVOKE`, PL/pgSQL
bodies, `COPY`, multi-word type names (`timestamp with time zone`,
`double precision` — use `timestamptz` etc.), the long tail of `ALTER`
sub-commands, and exact `pg_query` node-tree compatibility.

The AST is idiomatic typed Go (see [`ast.go`](ast.go)) — ergonomic to walk and
pattern-match, not a protobuf mirror.

## Deparse (AST → SQL)

`Deparse` renders any AST node back to SQL. It is deterministic and idempotent
(`deparse∘parse∘deparse == deparse∘parse`), which makes it both a formatting
utility and a round-trip test oracle — every query in the test corpus is
verified to survive parse → print → re-parse unchanged.

```go
stmt, _ := pgparse.ParseOne("select a,b from t where a>1")
fmt.Println(pgparse.Deparse(stmt))
// SELECT a, b FROM t WHERE a > 1
```

## Robustness

`Parse` never panics: any internal panic is recovered and returned as an error,
and a Go [fuzz target](fuzz_test.go) (`FuzzParse`) drives the unguarded parser
to prove it. Millions of executions over arbitrary and malformed input produce
errors, never crashes or hangs.

```bash
go test -fuzz=FuzzParse -fuzztime=30s
```

## Completeness vs other Go parsers

A feature-by-feature acceptance matrix (in [`comparison/`](comparison)) over 23
representative constructs, parsed by each engine. `pg_query_go` (the real
PostgreSQL parser) is the fidelity baseline; pgparse and
[GoSQLX](https://github.com/ajitpratap0/GoSQLX) are the pure-Go contenders:

| | pgparse | pg_query_go | GoSQLX |
|---|:--:|:--:|:--:|
| **features accepted** | **23 / 23** | 23 / 23 | 20 / 23 |
| multi-column `UPDATE SET (a,b)=(…)` | ✓ | ✓ | ✗ |
| `extract` / `substring` keyword syntax | ✓ | ✓ | ✗ |
| typed literal + `INTERVAL '90' day` | ✓ | ✓ | ✗ |
| `LATERAL` join | ✓ | ✓ | ✓ |
| DDL `CREATE TABLE` | ✓ | ✓ | ✓ |

pgparse matches the real-PostgreSQL baseline across all 23 representative
constructs, and parses the multi-column `UPDATE` form GoSQLX rejects.

### Breadth: the PostgreSQL regression suite

A 23-feature matrix only proves the happy path. For a breadth measure, pgparse
is run against a subset of PostgreSQL's own [regression test suite](comparison/testdata/regress)
(`src/test/regress/sql`) — the SQL that `libpg_query`/`pg_query_go` is validated
against. Each file is split into statements; a statement counts only when
`pg_query_go` (the real parser) accepts it, which discards psql meta-commands
and the suite's deliberately-invalid cases. The score is the share of those
genuinely-valid statements each pure-Go parser also accepts:

| | statements | accepted |
|---|--:|--:|
| **pgparse** | 7,985 | **92.2%** |
| GoSQLX | 7,985 | 48.4% |

pgparse accepts **92.2%** of the real PostgreSQL statements `pg_query_go`
accepts, far ahead of GoSQLX. Of those, DML, queries, and core DDL are parsed
into a full typed AST; utility and administrative statements (`SET`, `COPY`,
`GRANT`, `ANALYZE`, `CREATE TYPE`/`SEQUENCE`/`FUNCTION`, `DROP ROLE`, …) and the
DDL options pgparse does not model (partitioning, storage parameters, …) are
recognised as a [`RawStmt`](ddl_ast.go): the leading keyword plus the verbatim,
delimiter-validated statement text. So "accepted" means *recognised and
lexically validated*, with full structure for the DML/DDL core. For an
exhaustive node tree of every statement, use `pg_query_go`.

### vs `pg_query_go`, in one place

`pg_query_go` *is* the PostgreSQL parser (via cgo), so on coverage it is the
100% baseline. The trade is everything else. Measured over the 4,231 regression
statements both engines accept:

| | pgparse | pg_query_go |
|---|--:|--:|
| statements accepted (valid PG) | 92% | 100% |
| latency / statement | **~2.8 µs** | ~50 µs |
| speedup | **~18×** | 1× |
| memory / statement | ~2.2 KB | ~2.9 KB |
| allocations / statement | **~18** | ~56 |
| cgo / C toolchain | none | required |
| startup memory cost | none | C runtime |

Latency, memory, and allocations are measured over the 5,126 statements pgparse
parses into a full AST (RawStmt-recognised statements are excluded so both sides
do real parsing work). In short: `pg_query_go` for exhaustive fidelity; pgparse
when you want most of the SQL real apps write, ~18× faster, with no cgo.

Reproduce all of the above with `make compare`.

## Performance

Hand-written byte scanner (no regex) producing tokens whose values alias the
source string, feeding a single-pass recursive-descent parser. No reflection,
no intermediate string copies in the hot path.

Measured on Go 1.26 / arm64 (`go test -bench=. -benchmem`):

```
BenchmarkParse/simple-10        774518     1564 ns/op   25.58 MB/s    1488 B/op    19 allocs/op
BenchmarkParse/join-10          194725     6043 ns/op   41.04 MB/s    5168 B/op    65 allocs/op
BenchmarkParse/cte_window-10    195241     6195 ns/op   49.56 MB/s    5752 B/op    64 allocs/op
BenchmarkParse/expr_heavy-10    127866     9399 ns/op   27.66 MB/s   11984 B/op    90 allocs/op
BenchmarkParse/insert-10        268135     4464 ns/op   43.23 MB/s    3448 B/op    41 allocs/op
BenchmarkTokenize-10            399202     2997 ns/op   82.74 MB/s    3176 B/op    14 allocs/op
```

Unlike cgo/wasm approaches, there is **no fixed startup memory cost** — a process
that never parses pays nothing, and each parse allocates only its own AST.

Reproduce:

```bash
make bench
```

### Head-to-head vs `pg_query_go`

Benchmarked over the **TPC-H** query corpus (all 22 queries parse). Both engines
parse the **identical** queries; `pg_query_go` is the cgo binding around the real
PostgreSQL parser. Apple M-series, Go 1.26, `go test -bench=Corpus -benchmem`:

| Engine | ns / query | B / query | allocs / query |
|---|--:|--:|--:|
| **pgparse** | **~13,200** | **~9,300** | **~94** |
| `pg_query_go` | ~358,000 | ~18,900 | ~395 |
| **pgparse advantage** | **~27× faster** | **~2× less** | **~4× fewer** |

`pg_query_go` does strictly *more* — it produces the full-fidelity PostgreSQL
node tree for **every** statement kind. Its per-call cost includes the cgo
boundary and protobuf (de)serialization of that tree, which is the real price a
Go program pays to use it. pgparse trades exhaustive fidelity for a lean Go AST,
no cgo, and the throughput above. Pick accordingly.

The comparison lives in a **separate module** (`comparison/`) so the root module
never pulls in cgo. Reproduce:

```bash
make compare        # see Makefile; sets the macOS CGO workaround
```

Coverage of the corpus is asserted by `TestTPCHCoverage` (22/22).

## License

MIT — see [LICENSE](LICENSE).
