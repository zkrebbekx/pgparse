# PostgreSQL regression SQL corpus

These `*.sql` files are a subset of the PostgreSQL regression test suite,
`src/test/regress/sql/`, taken from the `REL_16_STABLE` branch:

https://github.com/postgres/postgres/tree/REL_16_STABLE/src/test/regress/sql

They are used by `TestRegressCompleteness` as a broad, real-world corpus to
measure parser completeness. They are **not** modified.

PostgreSQL is distributed under the PostgreSQL License (a permissive,
BSD-style licence). Copyright © 1996–2024, The PostgreSQL Global Development
Group, and © 1994, The Regents of the University of California. See
https://www.postgresql.org/about/licence/ for the full text.
