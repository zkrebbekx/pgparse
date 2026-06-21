package pgparse

import (
	"fmt"
	"strings"
)

// Lexer is a single-pass, byte-oriented scanner over a SQL string. It performs
// no regular-expression matching and produces tokens whose Val fields alias the
// source (no per-token heap allocation).
type Lexer struct {
	src string
	pos int // current byte offset
}

// NewLexer returns a Lexer positioned at the start of src.
func NewLexer(src string) *Lexer { return &Lexer{src: src} }

// MaxInputBytes is the largest input Parse and Tokenize accept. Larger inputs
// return a *SyntaxError instead of allocating unbounded memory — a guard for
// callers parsing untrusted SQL. Raise or lower it at startup if you handle
// legitimately larger (or want to cap smaller) statements; it is read without
// synchronisation, so set it before concurrent use.
var MaxInputBytes = 16 << 20 // 16 MiB

// Tokenize scans the entire input into a token slice terminated by TokenEOF.
// The slice is pre-sized from the input length to minimise reallocation.
func (l *Lexer) Tokenize() ([]Token, error) {
	if len(l.src) > MaxInputBytes {
		return nil, &SyntaxError{Pos: 0, Msg: fmt.Sprintf("input of %d bytes exceeds MaxInputBytes (%d)", len(l.src), MaxInputBytes)}
	}
	toks := make([]Token, 0, len(l.src)/4+8)
	for {
		t := l.Next()
		if t.Type == TokenError {
			return toks, newSyntaxError(l.src, t.Pos, t.Val)
		}
		toks = append(toks, t)
		if t.Type == TokenEOF {
			return toks, nil
		}
	}
}

func (l *Lexer) errTok(pos int, msg string) Token {
	return Token{Type: TokenError, Val: msg, Pos: pos}
}

// Next returns the next token, skipping whitespace and comments.
func (l *Lexer) Next() Token {
	l.skipTrivia()
	if l.pos >= len(l.src) {
		return Token{Type: TokenEOF, Pos: l.pos}
	}
	start := l.pos
	c := l.src[l.pos]

	switch {
	case c == '\'':
		return l.scanString(start)
	case c == '"':
		return l.scanQuotedIdent(start)
	case c == '$':
		return l.scanDollar(start)
	case isDigit(c):
		return l.scanNumber(start)
	case c == '.' && l.pos+1 < len(l.src) && isDigit(l.src[l.pos+1]):
		return l.scanNumber(start)
	case isStringPrefix(c) && l.pos+1 < len(l.src) && l.src[l.pos+1] == '\'':
		// Prefixed string literal: E'...' (escape), B'...' (bit), X'...' (hex).
		return l.scanPrefixedString(start, c)
	case isIdentStart(c):
		return l.scanIdent(start)
	}
	return l.scanOperator(start)
}

func (l *Lexer) skipTrivia() {
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v':
			l.pos++
		case c == '-' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '-':
			for l.pos < len(l.src) && l.src[l.pos] != '\n' {
				l.pos++
			}
		case c == '/' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '*':
			l.skipBlockComment()
		default:
			return
		}
	}
}

// skipBlockComment consumes a /* ... */ comment, honouring Postgres nesting.
func (l *Lexer) skipBlockComment() {
	depth := 0
	for l.pos < len(l.src) {
		if l.src[l.pos] == '/' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '*' {
			depth++
			l.pos += 2
			continue
		}
		if l.src[l.pos] == '*' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '/' {
			depth--
			l.pos += 2
			if depth == 0 {
				return
			}
			continue
		}
		l.pos++
	}
}

// scanString reads a '...' literal, treating ” as an embedded quote.
func (l *Lexer) scanString(start int) Token {
	l.pos++ // opening quote
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if c == '\'' {
			if l.pos+1 < len(l.src) && l.src[l.pos+1] == '\'' {
				l.pos += 2
				continue
			}
			l.pos++
			return Token{Type: TokenString, Val: l.src[start:l.pos], Pos: start}
		}
		l.pos++
	}
	return l.errTok(start, "unterminated string literal")
}

// scanPrefixedString reads E'...'/B'...'/X'...'. For escape strings (E/e) a
// backslash escapes the next character, including a quote.
func (l *Lexer) scanPrefixedString(start int, prefix byte) Token {
	escape := prefix == 'e' || prefix == 'E'
	l.pos = start + 1 // at opening quote
	l.pos++           // consume opening quote
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if escape && c == '\\' && l.pos+1 < len(l.src) {
			l.pos += 2
			continue
		}
		if c == '\'' {
			if l.pos+1 < len(l.src) && l.src[l.pos+1] == '\'' {
				l.pos += 2
				continue
			}
			l.pos++
			return Token{Type: TokenString, Val: l.src[start:l.pos], Pos: start}
		}
		l.pos++
	}
	return l.errTok(start, "unterminated string literal")
}

// scanQuotedIdent reads a "..." delimited identifier ("" is an embedded quote).
func (l *Lexer) scanQuotedIdent(start int) Token {
	l.pos++
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if c == '"' {
			if l.pos+1 < len(l.src) && l.src[l.pos+1] == '"' {
				l.pos += 2
				continue
			}
			l.pos++
			return Token{Type: TokenIdent, Val: l.src[start:l.pos], Pos: start}
		}
		l.pos++
	}
	return l.errTok(start, "unterminated quoted identifier")
}

// scanDollar handles both positional parameters ($1) and dollar-quoted strings
// ($tag$ body $tag$).
func (l *Lexer) scanDollar(start int) Token {
	p := l.pos + 1
	if p < len(l.src) && isDigit(l.src[p]) {
		for p < len(l.src) && isDigit(l.src[p]) {
			p++
		}
		l.pos = p
		return Token{Type: TokenParam, Val: l.src[start:p], Pos: start}
	}
	// dollar-quoted: read optional tag up to the closing '$'. A tag is a run of
	// identifier characters excluding '$' itself.
	tagEnd := p
	for tagEnd < len(l.src) && isDollarTagChar(l.src[tagEnd]) {
		tagEnd++
	}
	if tagEnd >= len(l.src) || l.src[tagEnd] != '$' {
		return l.errTok(start, "invalid dollar quote")
	}
	tag := l.src[start : tagEnd+1] // includes both $
	bodyStart := tagEnd + 1
	idx := strings.Index(l.src[bodyStart:], tag)
	if idx < 0 {
		return l.errTok(start, "unterminated dollar-quoted string")
	}
	l.pos = bodyStart + idx + len(tag)
	return Token{Type: TokenString, Val: l.src[start:l.pos], Pos: start}
}

// scanNumber reads an integer, decimal, or scientific-notation literal.
func (l *Lexer) scanNumber(start int) Token {
	for l.pos < len(l.src) && isDigit(l.src[l.pos]) {
		l.pos++
	}
	if l.pos < len(l.src) && l.src[l.pos] == '.' {
		l.pos++
		for l.pos < len(l.src) && isDigit(l.src[l.pos]) {
			l.pos++
		}
	}
	if l.pos < len(l.src) && (l.src[l.pos] == 'e' || l.src[l.pos] == 'E') {
		l.pos++
		if l.pos < len(l.src) && (l.src[l.pos] == '+' || l.src[l.pos] == '-') {
			l.pos++
		}
		for l.pos < len(l.src) && isDigit(l.src[l.pos]) {
			l.pos++
		}
	}
	return Token{Type: TokenNumber, Val: l.src[start:l.pos], Pos: start}
}

// scanIdent reads an unquoted identifier and resolves it against the keyword
// table. keyword folding is case-insensitive without allocating when the source
// is already lowercase.
func (l *Lexer) scanIdent(start int) Token {
	for l.pos < len(l.src) && isIdentPart(l.src[l.pos]) {
		l.pos++
	}
	raw := l.src[start:l.pos]
	key := raw
	if hasUpper(raw) {
		key = strings.ToLower(raw)
	}
	if kw := lookupKeyword(key); kw != kwNone {
		return Token{Type: TokenKeyword, Val: raw, kw: kw, Pos: start}
	}
	return Token{Type: TokenIdent, Val: raw, Pos: start}
}

// scanOperator reads punctuation and operators. Non-operator punctuation has
// dedicated single-character tokens. For operators it follows PostgreSQL: a
// maximal run of operator characters forms one operator. Runs that exactly match
// an arithmetic or comparison operator get a dedicated token type (so the parser
// can assign fixed precedence); every other run — JSON/array, bitwise, regex,
// and geometric operators (-> ->> #> @> <@ ?| << >> ~* !~ <<| &< <-> ...) —
// becomes a TokenOp carrying the operator text.
func (l *Lexer) scanOperator(start int) Token {
	c := l.src[start]
	mk := func(t TokenType, n int) Token {
		l.pos += n
		return Token{Type: t, Val: l.src[start : start+n], Pos: start}
	}
	switch c {
	case ',':
		return mk(TokenComma, 1)
	case ';':
		return mk(TokenSemicolon, 1)
	case '(':
		return mk(TokenLParen, 1)
	case ')':
		return mk(TokenRParen, 1)
	case '[':
		return mk(TokenLBracket, 1)
	case ']':
		return mk(TokenRBracket, 1)
	case '.':
		return mk(TokenDot, 1)
	case ':':
		if start+1 < len(l.src) && l.src[start+1] == ':' {
			return mk(TokenCast, 2)
		}
		return mk(TokenColon, 1)
	}
	if !isOpChar(c) {
		return l.errTok(start, "unexpected character "+string(c))
	}
	// Maximal run of operator characters.
	end := start
	for end < len(l.src) && isOpChar(l.src[end]) {
		end++
	}
	run := l.src[start:end]
	l.pos = end
	switch run {
	case "+":
		return Token{Type: TokenPlus, Val: run, Pos: start}
	case "-":
		return Token{Type: TokenMinus, Val: run, Pos: start}
	case "*":
		return Token{Type: TokenStar, Val: run, Pos: start}
	case "/":
		return Token{Type: TokenSlash, Val: run, Pos: start}
	case "%":
		return Token{Type: TokenPercent, Val: run, Pos: start}
	case "^":
		return Token{Type: TokenCaret, Val: run, Pos: start}
	case "=":
		return Token{Type: TokenEq, Val: run, Pos: start}
	case "<":
		return Token{Type: TokenLt, Val: run, Pos: start}
	case ">":
		return Token{Type: TokenGt, Val: run, Pos: start}
	case "<=":
		return Token{Type: TokenLte, Val: run, Pos: start}
	case ">=":
		return Token{Type: TokenGte, Val: run, Pos: start}
	case "<>", "!=":
		return Token{Type: TokenNeq, Val: run, Pos: start}
	case "||":
		return Token{Type: TokenConcat, Val: run, Pos: start}
	}
	return Token{Type: TokenOp, Val: run, Pos: start}
}

// isOpChar reports whether c is a PostgreSQL operator character.
func isOpChar(c byte) bool {
	switch c {
	case '+', '-', '*', '/', '<', '>', '=', '~', '!', '@', '#', '%', '^', '&', '|', '`', '?':
		return true
	}
	return false
}

func isDigit(c byte) bool      { return c >= '0' && c <= '9' }
func isIdentStart(c byte) bool { return c == '_' || (c|0x20 >= 'a' && c|0x20 <= 'z') || c >= 0x80 }
func isIdentPart(c byte) bool  { return isIdentStart(c) || isDigit(c) || c == '$' }

// isDollarTagChar reports whether c may appear in a dollar-quote tag (no '$').
func isDollarTagChar(c byte) bool { return isIdentStart(c) || isDigit(c) }

// isStringPrefix reports whether c introduces a prefixed string literal when
// immediately followed by a quote: E/e (escape), B/b (bit), X/x (hex).
func isStringPrefix(c byte) bool {
	switch c {
	case 'e', 'E', 'b', 'B', 'x', 'X':
		return true
	}
	return false
}

func hasUpper(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 'A' && s[i] <= 'Z' {
			return true
		}
	}
	return false
}
