package sema

import (
	"fmt"

	"github.com/amisonnet8/weave/internal/ast"
)

// ShapeInfo is what `X = shape({ propName: "typeHint", ... })` declares
// (weave_spec.md §4.3): a compile-time-only structural check for
// Weave's own dynamic objects — checked via `checkShape(X, obj)`
// (checkCheckShapeCall below). Unlike gotype/gofunc (§15), which
// describe an external Go value's real static type, a shape describes
// an expected *Weave-level* value category per property, so it uses a
// small fixed vocabulary (shapeHintKinds) rather than raw Go type
// strings — there is no Go interop involved at all here, only Weave's
// own dynamic values.
type ShapeInfo struct {
	Fields map[string]string // property name -> type hint (one of shapeHintKinds)
}

// shapeHintKinds are the type hints shape(...) accepts, matching
// weave_spec.md §2's value categories minus nil/actor references (see
// internal/codegen/shape.go's own doc comment for why "function" is
// checked differently from the other four, and why nil/actor refs
// aren't supported yet).
var shapeHintKinds = map[string]bool{
	"number": true, "string": true, "bool": true, "object": true, "function": true,
}

// checkShapeDecl checks `X = shape({ ... })` (weave_spec.md §4.3),
// mirroring checkGoDecl's own calling convention (called from checkStmt
// before ordinary assignment handling, returns (handled, err)).
func (c *checker) checkShapeDecl(name string, call *ast.CallExpr, line int) (bool, error) {
	callee, ok := call.Callee.(*ast.Ident)
	if !ok || callee.Name != "shape" {
		return false, nil
	}
	if why, ok := reservedName(name); ok {
		return true, fmt.Errorf("line %d: %q is a reserved name (%s)", line, name, why)
	}
	if len(call.Args) != 1 {
		return true, fmt.Errorf("line %d: shape(...) takes exactly one argument (a property-name -> type-hint object literal), got %d", call.Line, len(call.Args))
	}
	obj, ok := call.Args[0].(*ast.ObjectLit)
	if !ok {
		return true, fmt.Errorf("line %d: shape(...)'s argument must be an object literal", call.Line)
	}
	fields := map[string]string{}
	for _, f := range obj.Fields {
		lit, ok := f.Value.(*ast.StringLit)
		if !ok {
			return true, fmt.Errorf("line %d: shape(...) field %q must be a string literal type hint", call.Line, f.Name)
		}
		if !shapeHintKinds[lit.Value] {
			return true, fmt.Errorf("line %d: shape(...) field %q has unknown type hint %q (want one of: number, string, bool, object, function)", call.Line, f.Name, lit.Value)
		}
		fields[f.Name] = lit.Value
	}
	if c.shapes == nil {
		c.shapes = map[string]*ShapeInfo{}
	}
	c.shapes[name] = &ShapeInfo{Fields: fields}
	return true, nil
}

// checkCheckShapeCall checks `checkShape(ShapeName, value)`
// (weave_spec.md §4.3). Unlike a gomethod/gofunc call, there is no
// dynamic fallback to consider: shape(...) produces no runtime value at
// all (like gotype/gofunc, compile-time-only), so ShapeName must always
// resolve to a shape(...) declared earlier — checked here instead of
// through the ordinary scope (`sc`), exactly like gofunc's own proto
// argument (goasset.go's checkGoFuncDecl) never goes through sc either.
func (c *checker) checkCheckShapeCall(call *ast.CallExpr, sc *scope) error {
	if len(call.Args) != 2 {
		return fmt.Errorf("line %d: checkShape(...) takes exactly two arguments (a shape(...) name, then a value to check), got %d", call.Line, len(call.Args))
	}
	id, ok := call.Args[0].(*ast.Ident)
	if !ok || c.shapes[id.Name] == nil {
		return fmt.Errorf("line %d: checkShape(...)'s first argument must be a shape(...) declared earlier", call.Line)
	}
	return c.checkExpr(call.Args[1], sc)
}
