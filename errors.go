package pgparse

import "strconv"

// SyntaxError describes a lexing or parsing failure, including the byte offset
// in the source where the problem was detected.
type SyntaxError struct {
	Pos int    // byte offset into the input
	Msg string // human-readable description
}

func (e *SyntaxError) Error() string {
	return "pgparse: syntax error at offset " + strconv.Itoa(e.Pos) + ": " + e.Msg
}
