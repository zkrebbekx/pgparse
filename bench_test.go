package pgparse

import "testing"

// Representative queries spanning simple to complex, used by the benchmarks.
var benchQueries = map[string]string{
	"simple": `SELECT id, name FROM users WHERE id = $1`,

	"join": `SELECT u.id, u.name, o.total
	         FROM users u
	         JOIN orders o ON o.user_id = u.id
	         LEFT JOIN payments p ON p.order_id = o.id
	         WHERE u.active = true AND o.total > 100
	         ORDER BY o.total DESC
	         LIMIT 50`,

	"cte_window": `WITH ranked AS (
	         SELECT dept_id, employee_id, salary,
	                row_number() OVER (PARTITION BY dept_id ORDER BY salary DESC) AS rn
	         FROM employees
	       )
	       SELECT dept_id, employee_id, salary
	       FROM ranked
	       WHERE rn <= 3
	       ORDER BY dept_id, salary DESC`,

	"expr_heavy": `SELECT
	         CASE WHEN a > 0 THEN 'p' WHEN a < 0 THEN 'n' ELSE 'z' END,
	         (b + c) * d / 2 ::numeric(10,2),
	         coalesce(x, y, z),
	         a IN (1,2,3,4,5) AND b BETWEEN 10 AND 20 AND c LIKE 'foo%'
	       FROM t
	       WHERE id IS NOT NULL`,

	"insert": `INSERT INTO events (id, name, payload, created_at)
	       VALUES ($1, $2, $3, now()), ($4, $5, $6, now())
	       ON CONFLICT (id) DO UPDATE SET payload = excluded.payload
	       RETURNING id`,
}

func BenchmarkParse(b *testing.B) {
	for name, q := range benchQueries {
		q := q
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(q)))
			for i := 0; i < b.N; i++ {
				if _, err := Parse(q); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkTokenize(b *testing.B) {
	q := benchQueries["join"]
	b.ReportAllocs()
	b.SetBytes(int64(len(q)))
	for i := 0; i < b.N; i++ {
		if _, err := Tokenize(q); err != nil {
			b.Fatal(err)
		}
	}
}
