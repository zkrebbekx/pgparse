package pgparse

// TokenType enumerates the lexical categories the scanner produces.
type TokenType uint8

const (
	// Special
	TokenEOF TokenType = iota
	TokenError

	// Literals & identifiers
	TokenIdent  // foo, "Foo", schema.table
	TokenString // 'abc'
	TokenNumber // 42, 3.14, 1e10
	TokenParam  // $1
	TokenKeyword

	// Punctuation
	TokenComma     // ,
	TokenSemicolon // ;
	TokenLParen    // (
	TokenRParen    // )
	TokenLBracket  // [
	TokenRBracket  // ]
	TokenDot       // .
	TokenStar      // *

	// Operators
	TokenPlus    // +
	TokenMinus   // -
	TokenSlash   // /
	TokenPercent // %
	TokenCaret   // ^
	TokenEq      // =
	TokenNeq     // <> or !=
	TokenLt      // <
	TokenLte     // <=
	TokenGt      // >
	TokenGte     // >=
	TokenConcat  // ||
	TokenCast    // ::
	TokenColon   // :
)

// Token is a single lexical unit. Val aliases the source string (no copy), so a
// Token costs no heap allocation beyond the slice header.
type Token struct {
	Type TokenType
	Val  string  // exact source text of the token
	Kw   Keyword // non-zero when Type == TokenKeyword
	Pos  int     // byte offset into the source
}

// Keyword is an interned SQL keyword id. Zero means "not a keyword".
type Keyword uint8

const (
	kwNone Keyword = iota
	kwSelect
	kwFrom
	kwWhere
	kwGroup
	kwBy
	kwHaving
	kwOrder
	kwLimit
	kwOffset
	kwAs
	kwAnd
	kwOr
	kwNot
	kwNull
	kwTrue
	kwFalse
	kwIs
	kwIn
	kwLike
	kwILike
	kwBetween
	kwCase
	kwWhen
	kwThen
	kwElse
	kwEnd
	kwCast
	kwDistinct
	kwAll
	kwJoin
	kwInner
	kwLeft
	kwRight
	kwFull
	kwCross
	kwOuter
	kwOn
	kwUsing
	kwUnion
	kwIntersect
	kwExcept
	kwWith
	kwRecursive
	kwInsert
	kwInto
	kwValues
	kwUpdate
	kwSet
	kwDelete
	kwReturning
	kwConflict
	kwDo
	kwNothing
	kwAsc
	kwDesc
	kwNulls
	kwFirst
	kwLast
	kwOver
	kwPartition
	kwExists
	kwAny
	kwSome
	kwArray
	kwInterval
	kwDefault
)

// keywords maps lowercase keyword text to its interned id. Lookup is the only
// allocation-free way to distinguish keywords from identifiers.
var keywords = map[string]Keyword{
	"select": kwSelect, "from": kwFrom, "where": kwWhere, "group": kwGroup,
	"by": kwBy, "having": kwHaving, "order": kwOrder, "limit": kwLimit,
	"offset": kwOffset, "as": kwAs, "and": kwAnd, "or": kwOr, "not": kwNot,
	"null": kwNull, "true": kwTrue, "false": kwFalse, "is": kwIs, "in": kwIn,
	"like": kwLike, "ilike": kwILike, "between": kwBetween, "case": kwCase,
	"when": kwWhen, "then": kwThen, "else": kwElse, "end": kwEnd, "cast": kwCast,
	"distinct": kwDistinct, "all": kwAll, "join": kwJoin, "inner": kwInner,
	"left": kwLeft, "right": kwRight, "full": kwFull, "cross": kwCross,
	"outer": kwOuter, "on": kwOn, "using": kwUsing, "union": kwUnion,
	"intersect": kwIntersect, "except": kwExcept, "with": kwWith,
	"recursive": kwRecursive, "insert": kwInsert, "into": kwInto,
	"values": kwValues, "update": kwUpdate, "set": kwSet, "delete": kwDelete,
	"returning": kwReturning, "conflict": kwConflict, "do": kwDo,
	"nothing": kwNothing, "asc": kwAsc, "desc": kwDesc, "nulls": kwNulls,
	"first": kwFirst, "last": kwLast, "over": kwOver, "partition": kwPartition,
	"exists": kwExists, "any": kwAny, "some": kwSome, "array": kwArray,
	"interval": kwInterval, "default": kwDefault,
}

// lookupKeyword returns the keyword id for a lowercase identifier, or kwNone.
func lookupKeyword(lower string) Keyword { return keywords[lower] }
