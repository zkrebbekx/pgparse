package pgparse

import (
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestSyntaxErrorPosition(t *testing.T) {
	Convey("Given multi-line SQL with an error on line 3", t, func() {
		sql := "SELECT a\nFROM t\nWHERE x ==="
		Convey("When parsed", func() {
			_, err := Parse(sql)
			Convey("Then the SyntaxError reports line and column", func() {
				So(err, ShouldNotBeNil)
				se, ok := err.(*SyntaxError)
				So(ok, ShouldBeTrue)
				So(se.Line, ShouldEqual, 3)
				So(se.Col, ShouldBeGreaterThan, 0)
				So(se.Error(), ShouldContainSubstring, "line 3")
			})
		})
	})
}

func TestMaxInputBytes(t *testing.T) {
	Convey("Given an input larger than MaxInputBytes", t, func() {
		orig := MaxInputBytes
		MaxInputBytes = 64
		defer func() { MaxInputBytes = orig }()
		big := "SELECT " + strings.Repeat("x", 200)
		Convey("When parsed", func() {
			_, err := Parse(big)
			Convey("Then it is rejected without parsing", func() {
				So(err, ShouldNotBeNil)
				So(err.Error(), ShouldContainSubstring, "MaxInputBytes")
			})
		})
		Convey("And a small input under the limit still parses", func() {
			_, err := Parse("SELECT 1")
			So(err, ShouldBeNil)
		})
	})
}
