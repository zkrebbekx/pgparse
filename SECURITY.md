# Security Policy

## Supported versions

pgparse is distributed as a Go module; fixes are released against the latest
`v0.x` (and the latest `v1.x` once published). Please use the most recent
release.

## Reporting a vulnerability

Please report security issues **privately**, not via public issues or pull
requests:

- Preferred: open a [GitHub Security Advisory](https://github.com/zkrebbekx/pgparse/security/advisories/new).
- Alternatively, email the maintainer at the address on the GitHub profile.

Include a description, affected version, and ideally a minimal reproducer. You
can expect an acknowledgement within a few days and a fix or mitigation plan for
confirmed issues.

## Scope and threat model

pgparse parses SQL text into an AST. It is designed to be safe to run on
**untrusted input**:

- `Parse` never panics — internal panics are recovered and returned as errors.
- A recursion-depth limit rejects pathologically nested input instead of
  overflowing the stack.
- `MaxInputBytes` bounds input size to prevent unbounded memory allocation.
- It holds no shared mutable state and is safe for concurrent use.

**Not in scope:** pgparse is **not a security control**. It parses a pragmatic
subset of PostgreSQL and is not the authoritative parser, and its read/write
classification (`Mutates`/`Classify`) is purely syntactic — it cannot see side
effects inside functions (e.g. `SELECT nextval('s')`). Do not use pgparse as the
sole authority for SQL firewalling, read-only gating, or access control; enforce
those with server-side controls (roles, `default_transaction_read_only`,
read-replica connections).
