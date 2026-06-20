// Command example demonstrates parsing a few statements with pgparse.
package main

import (
	"fmt"
	"log"

	"github.com/zkrebbekx/pgparse"
)

func main() {
	queries := []string{
		`SELECT id, name FROM users WHERE id = $1`,
		`SELECT u.name, count(o.id) AS orders
		 FROM users u LEFT JOIN orders o ON o.user_id = u.id
		 GROUP BY u.name HAVING count(o.id) > 0 ORDER BY orders DESC LIMIT 5`,
		`INSERT INTO t (a, b) VALUES (1, 2) ON CONFLICT (a) DO NOTHING RETURNING id`,
		`WITH r AS (SELECT * FROM e) SELECT * FROM r WHERE x IS NOT NULL`,
	}

	for _, q := range queries {
		res, err := pgparse.Parse(q)
		if err != nil {
			log.Fatalf("parse failed: %v", err)
		}
		fmt.Printf("%-7T  parsed %d statement(s)\n", res.Stmts[0], len(res.Stmts))
	}
}
