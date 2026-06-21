package pgparse

import "reflect"

var nodeType = reflect.TypeOf((*Node)(nil)).Elem()

// Walk traverses the AST rooted at n in depth-first, pre-order, calling fn for
// every Node it visits (starting with n itself). If fn returns false, Walk does
// not descend into that node's children; returning true continues into them.
//
// Walk is the idiomatic way to inspect or collect from a parse tree — for
// example, to gather every referenced table:
//
//	var tables []string
//	pgparse.Walk(stmt, func(n pgparse.Node) bool {
//	        if t, ok := n.(*pgparse.TableName); ok {
//	                tables = append(tables, t.Name)
//	        }
//	        return true
//	})
//
// It discovers child nodes by reflection, so it covers every current and future
// node kind without special-casing.
func Walk(n Node, fn func(Node) bool) {
	if n == nil {
		return
	}
	rv := reflect.ValueOf(n)
	if rv.Kind() == reflect.Ptr && rv.IsNil() {
		return
	}
	if !fn(n) {
		return
	}
	// Scan the node's underlying value for descendant nodes, without revisiting
	// n itself.
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}
	walkScan(rv, fn)
}

// asNode returns v as a Node if it concretely implements Node and is non-nil.
func asNode(v reflect.Value) (Node, bool) {
	if !v.IsValid() || !v.CanInterface() || !v.Type().Implements(nodeType) {
		return nil, false
	}
	if v.Kind() == reflect.Ptr && v.IsNil() {
		return nil, false
	}
	nd, ok := v.Interface().(Node)
	return nd, ok && nd != nil
}

// walkScan descends through a value looking for child nodes, calling Walk on
// each one it finds (which invokes fn and recurses).
func walkScan(v reflect.Value, fn func(Node) bool) {
	switch v.Kind() {
	case reflect.Ptr:
		if v.IsNil() {
			return
		}
		if nd, ok := asNode(v); ok {
			Walk(nd, fn)
			return
		}
		walkScan(v.Elem(), fn)
	case reflect.Interface:
		if v.IsNil() {
			return
		}
		iv := v.Elem()
		if nd, ok := asNode(iv); ok {
			Walk(nd, fn)
			return
		}
		walkScan(iv, fn)
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			if t.Field(i).PkgPath != "" { // unexported
				continue
			}
			walkScan(v.Field(i), fn)
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			walkScan(v.Index(i), fn)
		}
	}
}
