package pgparse

import (
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

// TestDepthGuardChains locks in the fix for the stack-overflow DoS: input that
// builds a very deep AST *iteratively* (long left-associative operator, join, and
// set-operation chains) must be rejected by Parse, not accepted and left for a
// recursive consumer (Deparse, Walk, a caller's visitor) to crash on. A stack
// overflow is a fatal runtime error that recover() cannot catch, so the only
// place to stop it is before the tree is returned.
func TestDepthGuardChains(t *testing.T) {
	Convey("Given input that builds a pathologically deep AST via chaining", t, func() {
		const n = 200_000
		chains := map[string]string{
			"arithmetic +":    "SELECT 1" + strings.Repeat("+1", n),
			"OR chain":        "SELECT 1" + strings.Repeat(" OR 1", n),
			"AND chain":       "SELECT 1" + strings.Repeat(" AND 1", n),
			"concat ||":       "SELECT 1" + strings.Repeat("||1", n),
			"comparison <":    "SELECT 1" + strings.Repeat("<1", n),
			"cast :: chain":   "SELECT 1" + strings.Repeat("::int", n),
			"subscript chain": "SELECT a" + strings.Repeat("[1]", n),
			"JOIN chain":      "SELECT 1 FROM t" + strings.Repeat(" JOIN t ON true", n),
			"UNION chain":     "SELECT 1" + strings.Repeat(" UNION SELECT 1", n),
		}
		for name, sql := range chains {
			Convey("Parse rejects the "+name+" instead of crashing", func() {
				res, err := Parse(sql)
				So(err, ShouldNotBeNil)
				So(res, ShouldBeNil)
				So(err.Error(), ShouldContainSubstring, "nesting depth")
			})
		}
	})

	Convey("Given an operator chain just under the depth limit", t, func() {
		sql := "SELECT 1" + strings.Repeat("+1", maxNestingDepth-50)
		res, err := Parse(sql)
		Convey("It parses and its recursive consumers do not overflow", func() {
			So(err, ShouldBeNil)
			So(res.Stmts, ShouldHaveLength, 1)
			// Both Deparse and Walk recurse over the tree; neither may crash.
			So(func() { _ = Deparse(res.Stmts[0]) }, ShouldNotPanic)
			So(func() {
				Walk(res.Stmts[0], func(Node) bool { return true })
			}, ShouldNotPanic)
		})
	})
}

// TestDepthGuardAllowsWide confirms the depth guard does not misfire on breadth:
// long lists (IN, VALUES, projection) are wide, not deep, and must still parse.
func TestDepthGuardAllowsWide(t *testing.T) {
	Convey("Given wide-but-shallow input", t, func() {
		list := strings.TrimSuffix(strings.Repeat("1,", 10_000), ",")
		wide := map[string]string{
			"large IN list":    "SELECT 1 WHERE x IN (" + list + ")",
			"large projection": "SELECT " + list,
			"many VALUES rows": "INSERT INTO t VALUES " + strings.TrimSuffix(strings.Repeat("(1),", 10_000), ","),
			"deep-nested parens is rejected only past the limit": "SELECT " + strings.Repeat("(", 500) + "1" + strings.Repeat(")", 500),
		}
		for name, sql := range wide {
			Convey("Parse accepts the "+name, func() {
				_, err := Parse(sql)
				So(err, ShouldBeNil)
			})
		}
	})
}

// TestNodeBudget locks in the memory-amplification backstop: input that packs a
// huge number of atoms into few bytes is capped by MaxNodes.
func TestNodeBudget(t *testing.T) {
	Convey("Given a lowered MaxNodes", t, func() {
		orig := MaxNodes
		MaxNodes = 1000
		defer func() { MaxNodes = orig }()

		Convey("Input exceeding the node budget is rejected", func() {
			sql := "SELECT " + strings.TrimSuffix(strings.Repeat("1,", 5000), ",")
			res, err := Parse(sql)
			So(err, ShouldNotBeNil)
			So(res, ShouldBeNil)
			So(err.Error(), ShouldContainSubstring, "MaxNodes")
		})

		Convey("Input within the node budget still parses", func() {
			_, err := Parse("SELECT a, b, c FROM t WHERE id = $1")
			So(err, ShouldBeNil)
		})
	})
}
