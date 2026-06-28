# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/), and the project follows
[Semantic Versioning](https://semver.org/) — see [Stability](README.md#stability).

## [Unreleased]

## [1.2.0] — 2026-06-29

### Added
- **Reserved keywords are accepted as attribute names after `.`** — e.g.
  `t.order`, `t.group`, `t.limit`, `(x).order`. PostgreSQL allows any keyword
  (reserved included) in the `ColLabel` position following a `.`; pgparse
  previously rejected these with `expected name after '.'`. A bare unqualified
  reserved keyword (`SELECT order`) is still rejected, matching PostgreSQL.

### Changed
- **Performance: ~60% less memory allocated per parse, ~5–14% faster.** Two
  allocation cuts, no API or AST change:
  - The lexer's token slice is now drawn from a `sync.Pool` on the internal
    parse path, eliminating a per-parse backing-array allocation (it was ~59% of
    all bytes allocated). The token slice is pure scratch — the AST copies out
    every string it keeps — so reuse is safe, and `Parse` remains safe for
    concurrent use. The exported `Tokenize` is unchanged.
  - Dotted name parts are gathered in a stack buffer and the AST slice is sized
    once, removing the append-grow reallocation on qualified references like
    `t.col`.
  - Measured deltas (`go test -bench`): bytes/op −49% to −76% (geomean −61%),
    time −5% to −14% (geomean −7%), allocs/op down across the board.

## [1.1.1] — 2026-06-22

### Fixed
- **Stack overflow on adversarial input (security).** Prefix operators (`NOT`,
  unary `-`/`+`/`~`) and multidimensional `ARRAY[` literals recurse outside
  `parseExpr`, so they bypassed the recursion-depth guard: a single ≤16 MiB
  `Parse` call of e.g. `NOT NOT NOT …` or `- - - …` or `ARRAY[[[ …` could
  overflow the goroutine stack — a fatal runtime error that `recover()` cannot
  catch, crashing the process. These paths now enforce `maxNestingDepth` like
  every other recursion, closing a hole in the "safe on untrusted input"
  guarantee. Added regression tests for each vector and fuzz seeds. Thanks to an
  external security review for the report.

## [1.1.0] — 2026-06-22

Grammar coverage of the PostgreSQL regression suite rises from 97.8% to **99.4%**.
All additions are backward-compatible (new node/field, no removals).

### Added
- `FieldExpr` node: field selection on a composite expression — `(x).a`,
  `arr[1].field`, `func(...).f`, and `(x).*`.
- INSERT column targets may now carry subscript/field indirection:
  `INSERT INTO t (f2[1], f4[1].g) VALUES (...)`.
- `ON CONFLICT` targets may be expressions, e.g. `ON CONFLICT (lower(name))`.
- `JoinExpr.Alias`: join aliases — `a JOIN b USING (i) AS x` and
  `(a JOIN b) AS x`.
- Set-returning-function column-definition lists: `f(...) AS x(a int, b text)`
  and `f(...) AS (a int)`. `FuncTable.ColumnsText` keeps the verbatim list so the
  column types survive a deparse.
- Non-reserved keywords are accepted as column aliases after `AS` (e.g.
  `count(*) AS desc`).
- More built-in type names take a typed string literal (geometric types
  `point`/`box`/`circle`/…, `bit`, `int4`, `oid`, `xml`, …).

## [1.0.0] — 2026-06-21

First stable release. The public API is now covered by Semantic Versioning.

### Added
- `Walk(Node, func(Node) bool)` — depth-first pre-order AST traversal (returning
  false skips a node's children).
- Runnable godoc examples for `Parse`, `Deparse`, `Mutates`, and `Walk`.
- `CONTRIBUTING.md`.

### Fixed
- `position(substr IN string)` parsed the substring at full expression
  precedence and swallowed the `IN`; it now stops below comparison level.

### Changed
- **API freeze for 1.0.** Unexported internal types that had no usable public
  surface: `Parser`, `Keyword`, and the `Token.Kw` field. The public API is
  `Parse` / `ParseOne` / `Tokenize` / `Deparse` / `Walk` / `Classify` /
  `Mutates`, the AST node types, `Lexer` / `Token`, `SyntaxError`, and
  `MaxInputBytes`.
- Documented that `Parse` is safe for concurrent use and that `Deparse` assumes
  an acyclic tree.

## [0.3.2] — 2026-06-21

### Fixed
- **Stack overflow on deeply nested input.** Recursive descent now enforces a
  nesting-depth limit (`maxNestingDepth`); pathological input returns a
  `SyntaxError` instead of a fatal, unrecoverable stack overflow. Reachable on
  untrusted input (the browser playground, servers parsing user SQL).
- **Deparse fidelity.** Recursive-CTE `SEARCH`/`CYCLE` clauses and window-frame
  `EXCLUDE` clauses, previously consumed but dropped, are now preserved verbatim
  (`CTE.Aux`, `WindowFrame.Exclude`). Deparse is faithful, not just idempotent.

### Added
- `MaxInputBytes` — caps input size to prevent unbounded allocation on untrusted
  input (default 16 MiB).
- `SyntaxError` now reports `Line` and `Col` (1-based) in addition to `Pos`, and
  `Error()` renders the line/column.
- `SECURITY.md`, `CHANGELOG.md`, and a documented stability policy.

### Changed
- Documented that `Mutates`/`Classify` is a syntactic hint, **not** a security
  boundary (cannot detect side effects inside functions).

## [0.3.1] — 2026-06-21
- Docs/tooling refresh so pkg.go.dev reflects the current README (no library
  code change). Browser playground redesign, logos, social card; CI coverage
  (Codecov), `govulncheck`, Dependabot; corpus coverage test (~89%); benchmark
  targets bumped to latest (`pg_query_go` v6.2.2, GoSQLX v1.14.0).

## [0.3.0] — 2026-06-20
### Added
- Browser WebAssembly playground.
- `ClassTransaction` for transaction-control statements (non-mutating).

## [0.2.0] — 2026-06-20
### Added
- DDL: `CREATE TABLE`/`VIEW`/`INDEX`, `ALTER TABLE`, `DROP`.
- Grammar to ~97.8% of the PostgreSQL regression suite: JSON/array/bitwise/regex
  operators, `ANY`/`ALL`, `ARRAY`, `IS DISTINCT FROM`, `VALUES`, set-returning
  functions in `FROM`, `LATERAL`/`NATURAL` joins, `GROUP BY` grouping sets,
  `FETCH`/`FOR UPDATE`, window frames/`FILTER`/`WITHIN GROUP`, `WITH` everywhere,
  `SELECT … INTO`, and utility/admin statements as `RawStmt`.
- `Mutates`/`Classify` read/write classifier.
- AST→SQL deparser with idempotent round-trip testing.

## [0.1.0] — 2026-06-20
### Added
- Initial pure-Go PostgreSQL parser: SELECT/INSERT/UPDATE/DELETE, joins, CTEs,
  subqueries, set operations, window functions, the scalar expression grammar,
  and an idiomatic typed AST. No cgo, no WebAssembly runtime.

[Unreleased]: https://github.com/zkrebbekx/pgparse/compare/v1.1.1...HEAD
[1.1.1]: https://github.com/zkrebbekx/pgparse/compare/v1.1.0...v1.1.1
[1.1.0]: https://github.com/zkrebbekx/pgparse/compare/v1.0.0...v1.1.0
[1.0.0]: https://github.com/zkrebbekx/pgparse/compare/v0.3.2...v1.0.0
[0.3.2]: https://github.com/zkrebbekx/pgparse/compare/v0.3.1...v0.3.2
[0.3.1]: https://github.com/zkrebbekx/pgparse/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/zkrebbekx/pgparse/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/zkrebbekx/pgparse/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/zkrebbekx/pgparse/releases/tag/v0.1.0
