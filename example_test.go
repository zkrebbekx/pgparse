package pgparse_test

import (
	"fmt"

	"github.com/zkrebbekx/pgparse"
)

func ExampleParse() {
	res, err := pgparse.Parse("SELECT id, name FROM users WHERE active")
	if err != nil {
		panic(err)
	}
	fmt.Printf("%d statement, %T\n", len(res.Stmts), res.Stmts[0])
	// Output: 1 statement, *pgparse.SelectStmt
}

func ExampleDeparse() {
	stmt, _ := pgparse.ParseOne("select   a,b   from t where a>1")
	fmt.Println(pgparse.Deparse(stmt))
	// Output: SELECT a, b FROM t WHERE a > 1
}

func ExampleMutates() {
	write, _ := pgparse.Parse("UPDATE accounts SET balance = 0 WHERE id = $1")
	read, _ := pgparse.Parse("SELECT * FROM accounts")
	fmt.Println(write.Mutates(), read.Mutates())
	// Output: true false
}

func ExampleWalk() {
	stmt, _ := pgparse.ParseOne("SELECT * FROM a JOIN b ON a.id = b.a")
	var tables []string
	pgparse.Walk(stmt, func(n pgparse.Node) bool {
		if t, ok := n.(*pgparse.TableName); ok {
			tables = append(tables, t.Name)
		}
		return true
	})
	fmt.Println(tables)
	// Output: [a b]
}
