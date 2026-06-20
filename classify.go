package pgparse

import "strings"

// StmtClass categorises a statement by its effect on the database.
type StmtClass uint8

const (
	// ClassReadOnly is a pure read: SELECT/VALUES without a data-modifying CTE,
	// and utility statements that change neither data nor schema (SHOW, SET,
	// transaction control, cursor commands, ANALYZE/VACUUM, ...).
	ClassReadOnly StmtClass = iota
	// ClassWrite changes table data: INSERT/UPDATE/DELETE, TRUNCATE, MERGE, or a
	// SELECT carrying a data-modifying CTE.
	ClassWrite
	// ClassDDL changes schema or catalog: CREATE/ALTER/DROP, GRANT/REVOKE,
	// COMMENT, REINDEX, ...
	ClassDDL
	// ClassUtility is a recognised utility/admin statement whose effect pgparse
	// does not model and which may write (COPY, EXPLAIN, DO, CALL, ...).
	ClassUtility
)

// rawReadOnly holds RawStmt leading keywords that neither change data nor
// schema. Anything not listed here or in rawDDL is treated conservatively as
// possibly-mutating.
var rawReadOnly = map[string]bool{
	"show": true, "set": true, "reset": true, "begin": true, "start": true,
	"commit": true, "end": true, "rollback": true, "abort": true,
	"savepoint": true, "release": true, "fetch": true, "move": true,
	"close": true, "declare": true, "listen": true, "unlisten": true,
	"discard": true, "checkpoint": true, "deallocate": true, "prepare": true,
	"lock": true, "analyze": true, "analyse": true, "vacuum": true, "load": true,
}

// rawDDL holds RawStmt leading keywords that change schema or catalog state.
var rawDDL = map[string]bool{
	"create": true, "alter": true, "drop": true,
	"comment": true, "grant": true, "revoke": true, "reindex": true,
	"cluster": true, "refresh": true, "security": true, "import": true,
	"reassign": true,
}

// Classify reports the effect class of a single statement.
func Classify(s Stmt) StmtClass {
	switch x := s.(type) {
	case *SelectStmt:
		if x.Into != nil {
			return ClassDDL // SELECT ... INTO creates a table
		}
		if hasModifyingCTE(x.With) {
			return ClassWrite
		}
		return ClassReadOnly
	case *InsertStmt:
		return ClassWrite
	case *UpdateStmt:
		return ClassWrite
	case *DeleteStmt:
		return ClassWrite
	case *CreateTableStmt, *CreateViewStmt, *CreateIndexStmt, *DropStmt, *AlterTableStmt:
		return ClassDDL
	case *RawStmt:
		kw := strings.ToLower(x.Keyword)
		switch {
		case rawReadOnly[kw]:
			return ClassReadOnly
		case rawDDL[kw]:
			return ClassDDL
		case kw == "truncate" || kw == "merge":
			return ClassWrite
		default:
			return ClassUtility
		}
	}
	return ClassUtility
}

// Mutates reports whether the statement changes data or schema, or — for
// utility/admin statements whose effect pgparse does not model — might. Pure
// reads return false; everything that could write or alter returns true.
//
// This is intended as a conservative guard (e.g. for read-replica routing or a
// read-only permission check): when in doubt it returns true. Note that EXPLAIN
// is treated as possibly-mutating because EXPLAIN ANALYZE executes its argument.
func Mutates(s Stmt) bool {
	switch Classify(s) {
	case ClassReadOnly:
		return false
	default:
		return true
	}
}

// hasModifyingCTE reports whether any CTE in the list (transitively) writes.
func hasModifyingCTE(ctes []*CTE) bool {
	for _, c := range ctes {
		if c.Stmt != nil && Mutates(c.Stmt) {
			return true
		}
	}
	return false
}

// Mutates reports whether any statement in the result changes data or schema
// (or might, for unmodelled utility statements). A nil result is read-only.
func (r *ParseResult) Mutates() bool {
	if r == nil {
		return false
	}
	for _, s := range r.Stmts {
		if Mutates(s) {
			return true
		}
	}
	return false
}

// ReadOnly reports whether every statement in the result is a pure read.
func (r *ParseResult) ReadOnly() bool { return !r.Mutates() }
