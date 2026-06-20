package pgparse

import "strings"

// typeWords are bare identifiers that may introduce a typed string literal,
// e.g. date '2020-01-01' or timestamp '...'. They are not reserved keywords in
// pgparse, so the lexer yields them as identifiers.
var typeWords = map[string]bool{
	"date": true, "time": true, "timestamp": true, "timestamptz": true,
	"numeric": true, "decimal": true, "real": true, "money": true,
	"uuid": true, "json": true, "jsonb": true, "bytea": true, "bool": true,
	"boolean": true, "inet": true, "cidr": true, "macaddr": true,
}

func isTypeWord(s string) bool {
	if typeWords[s] {
		return true
	}
	return typeWords[strings.ToLower(s)]
}

// intervalUnits are the trailing qualifiers allowed after INTERVAL 'literal'.
var intervalUnits = map[string]bool{
	"year": true, "years": true, "month": true, "months": true,
	"day": true, "days": true, "hour": true, "hours": true,
	"minute": true, "minutes": true, "second": true, "seconds": true,
	"week": true, "weeks": true, "decade": true, "century": true,
	"millennium": true, "microsecond": true, "microseconds": true,
	"millisecond": true, "milliseconds": true, "to": true,
}

func isIntervalUnit(t Token) bool {
	if t.Type != TokenIdent {
		return false
	}
	return intervalUnits[strings.ToLower(t.Val)]
}

// specialFuncs use SQL keyword-delimited argument syntax rather than a plain
// comma-separated list.
var specialFuncs = map[string]bool{
	"extract": true, "substring": true, "position": true, "trim": true,
	"overlay": true,
}

func isSpecialFunc(name string) bool { return specialFuncs[strings.ToLower(name)] }

// identIs reports whether t is an identifier equal (case-insensitively) to s.
// It is used for the non-reserved words that appear inside special functions
// (from, for, placing) which the lexer treats as ordinary identifiers.
func identIs(t Token, s string) bool {
	return t.Type == TokenIdent && strings.EqualFold(t.Val, s)
}

// parseSpecialArgs parses the keyword-delimited argument grammar of the SQL
// special functions, populating fc.Args. The caller has already consumed '('
// and is responsible for the closing ')'.
func (p *Parser) parseSpecialArgs(fc *FuncCall) error {
	switch strings.ToLower(fc.Name) {
	case "extract":
		// extract(field FROM source)
		field := p.cur()
		if field.Type != TokenIdent && field.Type != TokenKeyword {
			return p.errf(field, "expected field name in extract()")
		}
		p.advance()
		if !p.acceptKw(kwFrom) {
			return p.errf(p.cur(), "expected FROM in extract()")
		}
		src, err := p.parseExpr()
		if err != nil {
			return err
		}
		fc.Args = []Expr{&Literal{Kind: LitString, Val: strings.ToLower(field.Val)}, src}
		return nil

	case "position":
		// position(substr IN string)
		needle, err := p.parseExpr()
		if err != nil {
			return err
		}
		if !p.acceptKw(kwIn) {
			return p.errf(p.cur(), "expected IN in position()")
		}
		hay, err := p.parseExpr()
		if err != nil {
			return err
		}
		fc.Args = []Expr{needle, hay}
		return nil

	case "trim":
		// trim([leading|trailing|both] [chars] [FROM] string)
		if id := p.cur(); identIs(id, "leading") || identIs(id, "trailing") || identIs(id, "both") {
			p.advance()
		}
		first, err := p.parseExpr()
		if err != nil {
			return err
		}
		fc.Args = []Expr{first}
		if p.acceptKw(kwFrom) {
			s, err := p.parseExpr()
			if err != nil {
				return err
			}
			fc.Args = append(fc.Args, s)
		}
		return nil

	default:
		// substring / overlay: expr [FROM expr] [FOR expr] [PLACING expr],
		// or the plain comma-separated form.
		first, err := p.parseExpr()
		if err != nil {
			return err
		}
		fc.Args = []Expr{first}
		for {
			switch {
			case p.acceptType(TokenComma):
				e, err := p.parseExpr()
				if err != nil {
					return err
				}
				fc.Args = append(fc.Args, e)
			case p.acceptKw(kwFrom):
				e, err := p.parseExpr()
				if err != nil {
					return err
				}
				fc.Args = append(fc.Args, e)
			case identIs(p.cur(), "for"), identIs(p.cur(), "placing"):
				p.advance()
				e, err := p.parseExpr()
				if err != nil {
					return err
				}
				fc.Args = append(fc.Args, e)
			default:
				return nil
			}
		}
	}
}
