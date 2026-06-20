package pgparse

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestLexer(t *testing.T) {
	Convey("Given a SQL string with mixed lexical elements", t, func() {
		src := `SELECT a, "B", 'x''y', 42, 3.14, $1 FROM t -- trailing`

		Convey("When it is tokenized", func() {
			toks, err := Tokenize(src)

			Convey("Then there is no error and the stream ends in EOF", func() {
				So(err, ShouldBeNil)
				So(toks[len(toks)-1].Type, ShouldEqual, TokenEOF)
			})

			Convey("Then keywords, identifiers and literals are classified", func() {
				So(toks[0].Type, ShouldEqual, TokenKeyword)
				So(toks[0].Kw, ShouldEqual, kwSelect)
				So(toks[1].Type, ShouldEqual, TokenIdent)
				So(toks[3].Type, ShouldEqual, TokenIdent)  // "B" quoted
				So(toks[5].Type, ShouldEqual, TokenString) // 'x''y'
				So(toks[7].Type, ShouldEqual, TokenNumber) // 42
				So(toks[9].Type, ShouldEqual, TokenNumber) // 3.14
				So(toks[11].Type, ShouldEqual, TokenParam) // $1
			})
		})
	})

	Convey("Given keywords in mixed case", t, func() {
		Convey("When tokenized", func() {
			toks, _ := Tokenize("SeLeCt WHERE")
			Convey("Then folding is case-insensitive", func() {
				So(toks[0].Kw, ShouldEqual, kwSelect)
				So(toks[1].Kw, ShouldEqual, kwWhere)
			})
		})
	})

	Convey("Given comments and dollar-quoted strings", t, func() {
		Convey("When tokenized", func() {
			toks, err := Tokenize("/* c */ $tag$ raw 'body' $tag$ /* nested /* x */ */ 1")
			Convey("Then comments are skipped and the body is one string token", func() {
				So(err, ShouldBeNil)
				So(toks[0].Type, ShouldEqual, TokenString)
				So(toks[1].Type, ShouldEqual, TokenNumber)
			})
		})
	})

	Convey("Given an unterminated string", t, func() {
		Convey("When tokenized", func() {
			_, err := Tokenize("SELECT 'oops")
			Convey("Then a syntax error is returned", func() {
				So(err, ShouldNotBeNil)
				So(err, ShouldHaveSameTypeAs, &SyntaxError{})
			})
		})
	})
}
