package pgparse

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

// splitSQL splits a script on semicolons that are outside single-quoted strings
// and line comments — enough for the regression corpus.
func splitSQL(sql string) []string {
	var out []string
	var b strings.Builder
	for i := 0; i < len(sql); i++ {
		c := sql[i]
		switch c {
		case '\'':
			b.WriteByte(c)
			for i++; i < len(sql); i++ {
				b.WriteByte(sql[i])
				if sql[i] == '\'' {
					break
				}
			}
		case '-':
			if i+1 < len(sql) && sql[i+1] == '-' {
				for i < len(sql) && sql[i] != '\n' {
					b.WriteByte(sql[i])
					i++
				}
				if i < len(sql) {
					b.WriteByte(sql[i])
				}
			} else {
				b.WriteByte(c)
			}
		case ';':
			out = append(out, b.String())
			b.Reset()
		default:
			b.WriteByte(c)
		}
	}
	if strings.TrimSpace(b.String()) != "" {
		out = append(out, b.String())
	}
	return out
}

// TestCorpusExercise drives the bundled PostgreSQL regression corpus through the
// whole pipeline — Parse, Classify, Mutates, Deparse, and a deparse round-trip —
// so the parser, expression, DDL, deparser, and error branches are all exercised
// on real, varied SQL (including the suite's deliberately-invalid statements,
// which cover the error paths). It asserts behaviour, not pg_query parity.
func TestCorpusExercise(t *testing.T) {
	Convey("Given the bundled PostgreSQL statement corpus", t, func() {
		files, _ := filepath.Glob("comparison/testdata/regress/*.sql")
		if len(files) == 0 {
			SkipConvey("corpus not present", func() {})
			return
		}

		Convey("When every statement is parsed, classified, deparsed, and re-parsed", func() {
			var parsed, failed, roundTrips int
			for _, f := range files {
				raw, err := os.ReadFile(f)
				if err != nil {
					continue
				}
				for _, stmt := range splitSQL(string(raw)) {
					s := strings.TrimSpace(stmt)
					if s == "" || strings.HasPrefix(s, "\\") {
						continue
					}
					// Exercise the lexer path independently.
					_, _ = Tokenize(s)

					res, perr := Parse(s)
					if perr != nil {
						failed++
						// Error path: SyntaxError carries a position.
						_ = perr.Error()
						continue
					}
					parsed++
					_ = res.Mutates()
					_ = res.ReadOnly()
					for _, st := range res.Stmts {
						_ = Classify(st)
						out := Deparse(st)
						// Deparse round-trip: re-parse and re-deparse.
						if r2, e2 := Parse(out); e2 == nil {
							for _, st2 := range r2.Stmts {
								if Deparse(st2) == Deparse(st) {
									roundTrips++
								}
							}
						}
					}
				}
			}
			t.Logf("corpus: %d parsed, %d rejected, %d idempotent round-trips", parsed, failed, roundTrips)

			Convey("Then a large majority parse, and parsing never panics", func() {
				So(parsed, ShouldBeGreaterThan, failed*3) // >75% parse
				So(roundTrips, ShouldBeGreaterThan, parsed/2)
			})
		})
	})
}
