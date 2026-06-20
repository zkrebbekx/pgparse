package pgparse

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestCreateTable(t *testing.T) {
	Convey("Given a CREATE TABLE with column and table constraints", t, func() {
		sql := `CREATE TABLE IF NOT EXISTS app.users (
			id bigint PRIMARY KEY,
			email text NOT NULL UNIQUE,
			org_id int REFERENCES orgs (id),
			status text DEFAULT 'active',
			age int CHECK (age >= 0),
			CONSTRAINT uq_email UNIQUE (email),
			FOREIGN KEY (org_id) REFERENCES orgs (id)
		)`
		Convey("When parsed", func() {
			s, err := ParseOne(sql)
			Convey("Then the table, columns, and constraints are modelled", func() {
				So(err, ShouldBeNil)
				ct := s.(*CreateTableStmt)
				So(ct.IfNotExists, ShouldBeTrue)
				So(ct.Table.Schema, ShouldEqual, "app")
				So(ct.Table.Name, ShouldEqual, "users")
				So(len(ct.Columns), ShouldEqual, 5)
				So(ct.Columns[0].Constraints[0].Kind, ShouldEqual, ColConstraintPrimaryKey)
				So(ct.Columns[1].Constraints[0].Kind, ShouldEqual, ColConstraintNotNull)
				So(ct.Columns[2].Constraints[0].Kind, ShouldEqual, ColConstraintReferences)
				So(ct.Columns[2].Constraints[0].RefTable.Name, ShouldEqual, "orgs")
				So(len(ct.Constraints), ShouldEqual, 2)
				So(ct.Constraints[0].Kind, ShouldEqual, TableConstraintUnique)
				So(ct.Constraints[1].Kind, ShouldEqual, TableConstraintForeignKey)
			})
		})
	})

	Convey("Given non-reserved DDL words used as column names in DML", t, func() {
		Convey("When 'SELECT key, type, view, constraint FROM t' is parsed", func() {
			s, err := ParseOne("SELECT key, type, view, constraint FROM t")
			Convey("Then they remain valid identifiers (not keywords)", func() {
				So(err, ShouldBeNil)
				So(len(s.(*SelectStmt).Columns), ShouldEqual, 4)
			})
		})
	})
}

func TestCreateViewIndexDrop(t *testing.T) {
	Convey("Given CREATE OR REPLACE VIEW", t, func() {
		Convey("When parsed", func() {
			s, err := ParseOne("CREATE OR REPLACE VIEW v (a, b) AS SELECT x, y FROM t WHERE x > 0")
			Convey("Then replace, columns, and the select are captured", func() {
				So(err, ShouldBeNil)
				v := s.(*CreateViewStmt)
				So(v.OrReplace, ShouldBeTrue)
				So(v.Columns, ShouldResemble, []string{"a", "b"})
				So(v.Select, ShouldNotBeNil)
			})
		})
	})

	Convey("Given a partial UNIQUE INDEX USING a method", t, func() {
		Convey("When parsed", func() {
			s, err := ParseOne("CREATE UNIQUE INDEX idx ON t USING btree (a, lower(b) DESC) WHERE a IS NOT NULL")
			Convey("Then uniqueness, method, columns, and predicate are captured", func() {
				So(err, ShouldBeNil)
				ix := s.(*CreateIndexStmt)
				So(ix.Unique, ShouldBeTrue)
				So(ix.Using, ShouldEqual, "btree")
				So(len(ix.Columns), ShouldEqual, 2)
				So(ix.Columns[1].Desc, ShouldBeTrue)
				So(ix.Where, ShouldNotBeNil)
			})
		})
	})

	Convey("Given DROP TABLE IF EXISTS ... CASCADE", t, func() {
		Convey("When parsed", func() {
			s, err := ParseOne("DROP TABLE IF EXISTS a, b CASCADE")
			Convey("Then the object, names, and flags are captured", func() {
				So(err, ShouldBeNil)
				d := s.(*DropStmt)
				So(d.Object, ShouldEqual, "TABLE")
				So(d.IfExists, ShouldBeTrue)
				So(len(d.Names), ShouldEqual, 2)
				So(d.Cascade, ShouldBeTrue)
			})
		})
	})
}

func TestAlterTable(t *testing.T) {
	Convey("Given ALTER TABLE with multiple actions", t, func() {
		sql := `ALTER TABLE t
			ADD COLUMN c text NOT NULL,
			DROP COLUMN d,
			ALTER COLUMN e TYPE bigint,
			ALTER COLUMN f SET DEFAULT 0,
			ADD CONSTRAINT pk PRIMARY KEY (id),
			RENAME COLUMN g TO h`
		Convey("When parsed", func() {
			s, err := ParseOne(sql)
			Convey("Then each action is modelled in order", func() {
				So(err, ShouldBeNil)
				at := s.(*AlterTableStmt)
				So(len(at.Actions), ShouldEqual, 6)
				So(at.Actions[0].Kind, ShouldEqual, AlterAddColumn)
				So(at.Actions[1].Kind, ShouldEqual, AlterDropColumn)
				So(at.Actions[2].Kind, ShouldEqual, AlterColumnType)
				So(at.Actions[3].Kind, ShouldEqual, AlterColumnSetDefault)
				So(at.Actions[4].Kind, ShouldEqual, AlterAddConstraint)
				So(at.Actions[5].Kind, ShouldEqual, AlterRenameColumn)
			})
		})
	})
}
