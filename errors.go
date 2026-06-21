package pgparse

import (
	"fmt"
	"strconv"
	"strings"
)

// SyntaxError describes a lexing or parsing failure. Pos is the byte offset into
// the input; Line and Col are the 1-based line and column at that offset (0 when
// the error is not tied to a position).
type SyntaxError struct {
	Pos  int    // byte offset into the input
	Line int    // 1-based line, 0 when unknown
	Col  int    // 1-based column, 0 when unknown
	Msg  string // human-readable description
}

func (e *SyntaxError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("pgparse: syntax error at line %d, column %d: %s", e.Line, e.Col, e.Msg)
	}
	return "pgparse: syntax error at offset " + strconv.Itoa(e.Pos) + ": " + e.Msg
}

// newSyntaxError builds a SyntaxError with line/column resolved from the source.
func newSyntaxError(src string, pos int, msg string) *SyntaxError {
	line, col := lineCol(src, pos)
	return &SyntaxError{Pos: pos, Line: line, Col: col, Msg: msg}
}

// lineCol returns the 1-based line and column for a byte offset in src.
func lineCol(src string, pos int) (int, int) {
	if pos < 0 {
		return 0, 0
	}
	if pos > len(src) {
		pos = len(src)
	}
	line := 1 + strings.Count(src[:pos], "\n")
	col := pos - (strings.LastIndexByte(src[:pos], '\n') + 1) + 1
	return line, col
}
