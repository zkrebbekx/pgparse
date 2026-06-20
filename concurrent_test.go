package pgparse

import (
	"sync"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

// TestConcurrentParse exercises Parse, Deparse, and Mutates from many goroutines
// at once. pgparse holds no shared mutable state (the keyword/word tables are
// read-only), so this must be race-free under `go test -race`.
func TestConcurrentParse(t *testing.T) {
	Convey("Given a parser with no shared mutable state", t, func() {
		queries := []string{
			"SELECT u.id, count(*) FROM users u JOIN orders o ON o.uid = u.id GROUP BY u.id",
			"INSERT INTO t (a, b) VALUES (1, 2) ON CONFLICT (a) DO UPDATE SET b = 3",
			"WITH x AS (UPDATE t SET a = 1 RETURNING id) SELECT * FROM x",
			"CREATE TABLE t (id bigint PRIMARY KEY, name text NOT NULL)",
			"SELECT a -> 'k' @> b FROM t WHERE id = ANY (ARRAY[1, 2, 3])",
		}
		Convey("When parsed concurrently from many goroutines", func() {
			const workers = 64
			var wg sync.WaitGroup
			errs := make([]error, workers)
			for w := 0; w < workers; w++ {
				wg.Add(1)
				go func(w int) {
					defer wg.Done()
					for i := 0; i < 500; i++ {
						q := queries[(w+i)%len(queries)]
						res, err := Parse(q)
						if err != nil {
							errs[w] = err
							return
						}
						_ = res.Mutates()
						_ = Deparse(res.Stmts[0])
					}
				}(w)
			}
			wg.Wait()
			Convey("Then every parse succeeds with no data race or error", func() {
				for _, e := range errs {
					So(e, ShouldBeNil)
				}
			})
		})
	})
}
