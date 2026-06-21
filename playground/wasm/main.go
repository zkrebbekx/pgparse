//go:build js && wasm

// Command wasm is the browser entry point for the pgparse playground. It exposes
// pgparse's parse, classify, deparse, and a reflective AST dump to JavaScript,
// plus an in-wasm micro-benchmark so the page can show real parse latency
// without JS↔wasm boundary noise.
package main

import (
	"reflect"
	"strings"
	"time"

	"syscall/js"

	"github.com/zkrebbekx/pgparse"
)

func main() {
	js.Global().Set("pgparseAnalyze", js.FuncOf(analyze))
	js.Global().Set("pgparseBench", js.FuncOf(bench))
	js.Global().Set("pgparseMaxInput", js.ValueOf(pgparse.MaxInputBytes))
	js.Global().Set("pgparseReady", js.ValueOf(true))
	// Notify the page that wasm is live.
	if cb := js.Global().Get("onPgparseReady"); cb.Type() == js.TypeFunction {
		cb.Invoke()
	}
	select {} // keep the Go runtime alive
}

// analyze(sql) -> { ok, error?, mutates, statements: [{class, classLabel, deparsed, ast}] }
func analyze(_ js.Value, args []js.Value) any {
	if len(args) == 0 {
		return map[string]any{"ok": false, "error": "no input"}
	}
	sql := args[0].String()
	res, err := pgparse.Parse(sql)
	if err != nil {
		out := map[string]any{"ok": false, "error": err.Error()}
		if se, isSyntax := err.(*pgparse.SyntaxError); isSyntax {
			out["offset"] = se.Pos
			out["line"] = se.Line
			out["col"] = se.Col
			out["message"] = se.Msg
		}
		return out
	}
	stmts := make([]any, 0, len(res.Stmts))
	for _, s := range res.Stmts {
		cls := pgparse.Classify(s)
		stmts = append(stmts, map[string]any{
			"class":    int(cls),
			"label":    classLabel(cls),
			"emoji":    classEmoji(cls),
			"mutates":  pgparse.Mutates(s),
			"deparsed": pgparse.Deparse(s),
			"ast":      nodeToJS(s),
		})
	}
	return map[string]any{
		"ok":         true,
		"count":      len(res.Stmts),
		"mutates":    res.Mutates(),
		"statements": stmts,
	}
}

// bench(sql, iters) -> nanoseconds per parse (0 on error).
func bench(_ js.Value, args []js.Value) any {
	sql := args[0].String()
	iters := 2000
	if len(args) > 1 {
		iters = args[1].Int()
	}
	if _, err := pgparse.Parse(sql); err != nil {
		return 0.0
	}
	start := time.Now()
	for i := 0; i < iters; i++ {
		_, _ = pgparse.Parse(sql)
	}
	return float64(time.Since(start).Nanoseconds()) / float64(iters)
}

func classLabel(c pgparse.StmtClass) string {
	switch c {
	case pgparse.ClassReadOnly:
		return "Read-only"
	case pgparse.ClassWrite:
		return "Writes data"
	case pgparse.ClassDDL:
		return "Changes schema (DDL)"
	case pgparse.ClassTransaction:
		return "Transaction control"
	default:
		return "Utility / admin"
	}
}

func classEmoji(c pgparse.StmtClass) string {
	switch c {
	case pgparse.ClassReadOnly:
		return "🔒"
	case pgparse.ClassWrite:
		return "✏️"
	case pgparse.ClassDDL:
		return "🏗️"
	case pgparse.ClassTransaction:
		return "🔁"
	default:
		return "⚙️"
	}
}

// nodeToJS converts an AST node to a JS-friendly tree using reflection. Each
// struct becomes { _kind: TypeName, field: value, ... } with zero-valued fields
// omitted to keep the tree readable.
func nodeToJS(v any) any { return refToAny(reflect.ValueOf(v)) }

func refToAny(v reflect.Value) any {
	switch v.Kind() {
	case reflect.Invalid:
		return nil
	case reflect.Ptr, reflect.Interface:
		if v.IsNil() {
			return nil
		}
		return refToAny(v.Elem())
	case reflect.Struct:
		m := map[string]any{"_kind": v.Type().Name()}
		t := v.Type()
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if f.PkgPath != "" { // unexported
				continue
			}
			fv := v.Field(i)
			if fv.IsZero() {
				continue
			}
			m[f.Name] = refToAny(fv)
		}
		return m
	case reflect.Slice, reflect.Array:
		if v.Len() == 0 {
			return nil
		}
		arr := make([]any, v.Len())
		for i := 0; i < v.Len(); i++ {
			arr[i] = refToAny(v.Index(i))
		}
		return arr
	case reflect.String:
		return v.String()
	case reflect.Bool:
		return v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return int(v.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return int(v.Uint())
	case reflect.Float32, reflect.Float64:
		return v.Float()
	default:
		return strings.TrimSpace(v.String())
	}
}
