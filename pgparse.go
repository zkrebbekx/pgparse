// Package pgparse is a pure-Go PostgreSQL SQL parser. It has no cgo dependency
// and no WebAssembly runtime, so it imposes no per-process memory overhead at
// startup and links cleanly into any Go program.
//
// It parses a pragmatic, widely-used subset of PostgreSQL: SELECT (with joins,
// CTEs, subqueries, set operations, window functions, ORDER BY/LIMIT/OFFSET),
// INSERT (with ON CONFLICT and RETURNING), UPDATE, and DELETE, plus the full
// scalar expression grammar (CASE, CAST/::, IN, BETWEEN, LIKE/ILIKE, IS NULL,
// function calls, parameters). The result is an idiomatic, allocation-light Go
// AST rather than a protobuf node tree.
//
// Basic usage:
//
//	res, err := pgparse.Parse("SELECT id, name FROM users WHERE id = $1")
//	if err != nil {
//	        log.Fatal(err)
//	}
//	sel := res.Stmts[0].(*pgparse.SelectStmt)
package pgparse

import (
	"fmt"
	"sync"
)

// tokBufPool recycles the token backing array across parses. The token slice is
// pure scratch: the AST copies out the strings it keeps (identText / unquote),
// so no Token value escapes a single Parse call and the buffer is safe to reuse.
var tokBufPool = sync.Pool{New: func() any {
	b := make([]Token, 0, 64)
	return &b
}}

// ParseResult holds the statements produced from one input string.
type ParseResult struct {
	Stmts []Stmt
}

// Parse lexes and parses a SQL string containing one or more semicolon-
// separated statements. It returns a *SyntaxError on the first failure.
//
// Parse is safe on untrusted input: any internal panic is recovered and
// returned as an error; a depth limit bounds the AST it builds — rejecting both
// deeply nested and long left-associative-chain input (a+a+…+a, t JOIN t JOIN …,
// SELECT…UNION…) that would otherwise produce a tree too deep to traverse — so
// that recursive consumers of the result (Deparse, Walk) cannot overflow the
// stack; and MaxInputBytes and MaxNodes bound input size and node count. A stack
// overflow is a fatal error that recover cannot catch, which is why the depth
// bound is enforced during parsing rather than relied on afterwards. Parse holds
// no shared state and is safe to call concurrently from multiple goroutines.
func Parse(sql string) (res *ParseResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			res, err = nil, &SyntaxError{Msg: fmt.Sprintf("internal parser panic: %v", r)}
		}
	}()
	return parseInternal(sql)
}

// parseInternal does the real work without the panic guard, so the fuzz target
// can surface any panic as a test failure rather than masking it.
func parseInternal(sql string) (*ParseResult, error) {
	bufp := tokBufPool.Get().(*[]Token)
	toks, err := NewLexer(sql).tokenizeInto((*bufp)[:0])
	// toks may have been reallocated to a larger backing array by append; keep
	// whichever array we ended with so the pool retains the grown capacity.
	defer func() {
		*bufp = toks[:0]
		tokBufPool.Put(bufp)
	}()
	if err != nil {
		return nil, err
	}
	p := newParser(sql, toks)
	stmts, err := p.parseStatements()
	if err != nil {
		return nil, err
	}
	return &ParseResult{Stmts: stmts}, nil
}

// ParseOne parses a SQL string that must contain exactly one statement and
// returns that statement directly.
func ParseOne(sql string) (Stmt, error) {
	res, err := Parse(sql)
	if err != nil {
		return nil, err
	}
	if len(res.Stmts) != 1 {
		return nil, &SyntaxError{Msg: "expected exactly one statement"}
	}
	return res.Stmts[0], nil
}

// Tokenize exposes the lexer for callers that only need a token stream (for
// example, syntax highlighting). The returned slice ends with a TokenEOF.
func Tokenize(sql string) ([]Token, error) {
	return NewLexer(sql).Tokenize()
}
