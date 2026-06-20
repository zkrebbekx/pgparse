package pgparse

import "strings"

// Lexer is a single-pass, byte-oriented scanner over a SQL string. It performs
// no regular-expression matching and produces tokens whose Val fields alias the
// source (no per-token heap allocation).
type Lexer struct {
	src string
	pos int // current byte offset
}

// NewLexer returns a Lexer positioned at the start of src.
func NewLexer(src string) *Lexer { return &Lexer{src: src} }

// Tokenize scans the entire input into a token slice terminated by TokenEOF.
// The slice is pre-sized from the input length to minimise reallocation.
func (l *Lexer) Tokenize() ([]Token, error) {
	toks := make([]Token, 0, len(l.src)/4+8)
	for {
		t := l.Next()
		if t.Type == TokenError {
			return toks, &SyntaxError{Pos: t.Pos, Msg: t.Val}
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
// table. Keyword folding is case-insensitive without allocating when the source
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
		return Token{Type: TokenKeyword, Val: raw, Kw: kw, Pos: start}
	}
	return Token{Type: TokenIdent, Val: raw, Pos: start}
}

// scanOperator reads punctuation and operators. Beyond the arithmetic and
// comparison operators (which have dedicated token types so the parser can give
// them fixed precedence), it recognises PostgreSQL's open-ended operator class —
// JSON/array (-> ->> #> @> <@ ?), bitwise (& | # << >>), and regex (~ ~* !~) —
// emitting TokenOp with the operator text.
func (l *Lexer) scanOperator(start int) Token {
	c := l.src[start]
	two, three := byte(0), byte(0)
	if start+1 < len(l.src) {
		two = l.src[start+1]
	}
	if start+2 < len(l.src) {
		three = l.src[start+2]
	}
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
	case '*':
		return mk(TokenStar, 1)
	case '+':
		return mk(TokenPlus, 1)
	case '-':
		if two == '>' && three == '>' {
			return mk(TokenOp, 3) // ->>
		}
		if two == '>' {
			return mk(TokenOp, 2) // ->
		}
		return mk(TokenMinus, 1)
	case '/':
		return mk(TokenSlash, 1)
	case '%':
		return mk(TokenPercent, 1)
	case '^':
		return mk(TokenCaret, 1)
	case '=':
		return mk(TokenEq, 1)
	case '!':
		if two == '=' {
			return mk(TokenNeq, 2)
		}
		if two == '~' && three == '*' {
			return mk(TokenOp, 3) // !~*
		}
		if two == '~' {
			return mk(TokenOp, 2) // !~
		}
	case '<':
		if two == '<' {
			return mk(TokenOp, 2) // <<
		}
		if two == '@' {
			return mk(TokenOp, 2) // <@
		}
		if two == '>' {
			return mk(TokenNeq, 2)
		}
		if two == '=' {
			return mk(TokenLte, 2)
		}
		return mk(TokenLt, 1)
	case '>':
		if two == '>' {
			return mk(TokenOp, 2) // >>
		}
		if two == '=' {
			return mk(TokenGte, 2)
		}
		return mk(TokenGt, 1)
	case '|':
		if two == '|' {
			return mk(TokenConcat, 2)
		}
		return mk(TokenOp, 1) // bitwise OR
	case '&':
		if two == '&' {
			return mk(TokenOp, 2) // &&
		}
		return mk(TokenOp, 1) // bitwise AND
	case '#':
		if two == '>' && three == '>' {
			return mk(TokenOp, 3) // #>>
		}
		if two == '>' {
			return mk(TokenOp, 2) // #>
		}
		return mk(TokenOp, 1) // bitwise XOR
	case '@':
		if two == '>' {
			return mk(TokenOp, 2) // @>
		}
		if two == '@' {
			return mk(TokenOp, 2) // @@
		}
		return mk(TokenOp, 1)
	case '~':
		if two == '*' {
			return mk(TokenOp, 2) // ~*
		}
		return mk(TokenOp, 1) // regex match / bitwise NOT
	case '?':
		if two == '|' || two == '&' {
			return mk(TokenOp, 2) // ?| ?&
		}
		return mk(TokenOp, 1) // ? JSON key-exists
	case ':':
		if two == ':' {
			return mk(TokenCast, 2)
		}
		return mk(TokenColon, 1)
	}
	return l.errTok(start, "unexpected character "+string(c))
}

func isDigit(c byte) bool      { return c >= '0' && c <= '9' }
func isIdentStart(c byte) bool { return c == '_' || (c|0x20 >= 'a' && c|0x20 <= 'z') || c >= 0x80 }
func isIdentPart(c byte) bool  { return isIdentStart(c) || isDigit(c) || c == '$' }

// isDollarTagChar reports whether c may appear in a dollar-quote tag (no '$').
func isDollarTagChar(c byte) bool { return isIdentStart(c) || isDigit(c) }

func hasUpper(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 'A' && s[i] <= 'Z' {
			return true
		}
	}
	return false
}
