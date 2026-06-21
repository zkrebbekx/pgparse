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

import "fmt"

// ParseResult holds the statements produced from one input string.
type ParseResult struct {
	Stmts []Stmt
}

// Parse lexes and parses a SQL string containing one or more semicolon-
// separated statements. It returns a *SyntaxError on the first failure.
//
// Parse is safe on untrusted input: any internal panic is recovered and
// returned as an error, and a recursion-depth limit rejects pathologically
// nested input (which would otherwise overflow the stack — a crash recover
// cannot catch) with an ordinary error.
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
	toks, err := NewLexer(sql).Tokenize()
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
