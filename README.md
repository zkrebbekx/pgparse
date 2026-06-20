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
  `ON` / `USING`), `WHERE`, `GROUP BY`, `HAVING`, `ORDER BY` (`ASC`/`DESC`,
  `NULLS FIRST`/`LAST`), `LIMIT`, `OFFSET`
- `WITH` CTEs (incl. `RECURSIVE`) and subqueries in `FROM` / expressions
- Set operations: `UNION` / `INTERSECT` / `EXCEPT` (`ALL`)
- Window functions: `func(...) OVER (PARTITION BY ... ORDER BY ...)`
- `INSERT` — column lists, multi-row `VALUES`, `INSERT ... SELECT`,
  `ON CONFLICT (...) DO NOTHING | DO UPDATE SET ...`, `RETURNING`
- `UPDATE` — `SET`, `FROM`, `WHERE`, `RETURNING`
- `DELETE` — `USING`, `WHERE`, `RETURNING`

**Expressions**

- Logical (`AND` / `OR` / `NOT`), comparison, arithmetic, `||` concatenation
  — all with correct precedence and associativity
- `CASE` (simple + searched), `CAST(x AS t)` and `x::t`, `INTERVAL '...'`
- `IN (list | subquery)`, `BETWEEN`, `LIKE` / `ILIKE`, `IS [NOT] NULL/TRUE/FALSE`
- `EXISTS (...)`, scalar subqueries, function calls (`DISTINCT`, `count(*)`)
- Literals (string with `''` escapes, dollar-quoted, int/float, bool, NULL),
  positional parameters `$n`, qualified names `schema.table.column`, `table.*`

**Out of scope (use `pg_query_go`):** DDL (`CREATE`/`ALTER`/`DROP`), `MERGE`,
`GRANT`, PL/pgSQL bodies, `COPY`, full multi-word type grammar, and exact
`pg_query` node-tree compatibility.

The AST is idiomatic typed Go (see [`ast.go`](ast.go)) — ergonomic to walk and
pattern-match, not a protobuf mirror.

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

## License

MIT — see [LICENSE](LICENSE).
