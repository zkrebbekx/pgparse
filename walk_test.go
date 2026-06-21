package pgparse

import (
	"sort"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestWalk(t *testing.T) {
	Convey("Given a query that references several tables and columns", t, func() {
		s, err := ParseOne(`
			SELECT u.id, o.total
			FROM users u
			JOIN orders o ON o.user_id = u.id
			WHERE u.id IN (SELECT id FROM banned)
		`)
		So(err, ShouldBeNil)

		Convey("When walked to collect table names", func() {
			var tables []string
			Walk(s, func(n Node) bool {
				if tn, ok := n.(*TableName); ok {
					tables = append(tables, tn.Name)
				}
				return true
			})
			sort.Strings(tables)
			Convey("Then every table — including in the subquery — is found", func() {
				So(tables, ShouldResemble, []string{"banned", "orders", "users"})
			})
		})

		Convey("When fn returns false at the IN predicate, descent stops there", func() {
			cols := 0
			Walk(s, func(n Node) bool {
				if _, ok := n.(*InExpr); ok {
					return false // skip the IN predicate and its subquery
				}
				if _, ok := n.(*ColumnRef); ok {
					cols++
				}
				return true
			})
			Convey("Then only columns outside the IN predicate are counted", func() {
				// SELECT u.id, o.total + ON o.user_id, u.id — not the WHERE/subquery
				So(cols, ShouldEqual, 4)
			})
		})
	})

	Convey("Given nil input", t, func() {
		Convey("When walked", func() {
			Convey("Then it does not panic", func() {
				So(func() { Walk(nil, func(Node) bool { return true }) }, ShouldNotPanic)
			})
		})
	})
}
