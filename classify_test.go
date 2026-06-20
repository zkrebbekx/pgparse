package pgparse

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestMutates(t *testing.T) {
	Convey("Given statements of each effect class", t, func() {
		cases := []struct {
			sql     string
			mutates bool
			class   StmtClass
		}{
			{"SELECT * FROM users WHERE id = $1", false, ClassReadOnly},
			{"VALUES (1), (2)", false, ClassReadOnly},
			{"SELECT * FROM t FOR UPDATE", false, ClassReadOnly},
			{"INSERT INTO t (a) VALUES (1)", true, ClassWrite},
			{"UPDATE t SET a = 1 WHERE id = $1", true, ClassWrite},
			{"DELETE FROM t WHERE id = $1", true, ClassWrite},
			{"WITH u AS (UPDATE t SET a = 1 RETURNING id) SELECT * FROM u", true, ClassWrite},
			{"CREATE TABLE t (id int)", true, ClassDDL},
			{"DROP TABLE t", true, ClassDDL},
			{"ALTER TABLE t ADD COLUMN c int", true, ClassDDL},
			{"TRUNCATE t", true, ClassWrite},
			{"GRANT SELECT ON t TO bob", true, ClassDDL},
			{"COPY t FROM stdin", true, ClassUtility},
			{"SET search_path TO public", false, ClassReadOnly},
			{"SHOW all", false, ClassReadOnly},
			{"BEGIN", false, ClassTransaction},
			{"COMMIT", false, ClassTransaction},
			{"ROLLBACK", false, ClassTransaction},
		}
		Convey("When classified", func() {
			Convey("Then Mutates and Classify match expectations", func() {
				for _, c := range cases {
					res, err := Parse(c.sql)
					So(err, ShouldBeNil)
					So(res.Mutates(), ShouldEqual, c.mutates)
					So(Classify(res.Stmts[0]), ShouldEqual, c.class)
				}
			})
		})
	})

	Convey("Given a read query and a write query in one string", t, func() {
		Convey("When the whole result is classified", func() {
			res, _ := Parse("SELECT 1; UPDATE t SET a = 1")
			Convey("Then Mutates is true and ReadOnly is false", func() {
				So(res.Mutates(), ShouldBeTrue)
				So(res.ReadOnly(), ShouldBeFalse)
			})
		})
	})
}
