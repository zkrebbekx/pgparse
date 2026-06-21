# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/), and the project follows
[Semantic Versioning](https://semver.org/) — see [Stability](README.md#stability).

## [Unreleased]

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

[Unreleased]: https://github.com/zkrebbekx/pgparse/compare/v0.3.2...HEAD
[0.3.2]: https://github.com/zkrebbekx/pgparse/compare/v0.3.1...v0.3.2
[0.3.1]: https://github.com/zkrebbekx/pgparse/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/zkrebbekx/pgparse/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/zkrebbekx/pgparse/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/zkrebbekx/pgparse/releases/tag/v0.1.0
