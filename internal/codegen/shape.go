package codegen

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/amisonnet8/weave/internal/ast"
)

// ShapeInfo mirrors sema's own (internal/sema/shape.go) — see its doc
// comment for shape(...)'s full reasoning and shapeHintKinds' vocabulary.
type ShapeInfo struct {
	Fields map[string]string // property name -> type hint
}

// genShapeDecl mirrors sema's recognition of `name = shape(...)`
// (sema/shape.go's checkShapeDecl) — like gotype/gofunc, this is
// compile-time-only and emits no IR (weave_spec.md §4.3).
func genShapeDecl(fg *funcGen, name string, call *ast.CallExpr) (bool, error) {
	callee, ok := call.Callee.(*ast.Ident)
	if !ok || callee.Name != "shape" {
		return false, nil
	}
	obj := call.Args[0].(*ast.ObjectLit)
	fields := map[string]string{}
	for _, f := range obj.Fields {
		fields[f.Name] = f.Value.(*ast.StringLit).Value
	}
	if fg.ctx.shapes == nil {
		fg.ctx.shapes = map[string]*ShapeInfo{}
	}
	fg.ctx.shapes[name] = &ShapeInfo{Fields: fields}
	return true, nil
}

// shapeHintGoType maps a shape(...) type hint to the "?xxx"-shaped Go
// reference genAssertOrTypeError expects (goasset.go) — a shape check
// ultimately wants the exact same "ASSERT to this concrete type,
// TypeError on failure" behavior a typed Go-asset boundary already has,
// just aimed at Weave's own value representations instead of an
// external Go type. "object" asserts to weavert.Object specifically — a
// real named Go type (weavert/object.go's `type Object map[string]any`,
// consistently what NewObject/ObjSet/ObjGet always produce), not a bare
// unnamed map, so this is a meaningful check, not a tautology.
// "function" has no entry: see genCheckShapeCall's own doc comment for
// why it can't go through ASSERT at all.
var shapeHintGoType = map[string]string{
	"number": "?float64",
	"string": "?string",
	"bool":   "?bool",
	"object": "?weavert.Object",
}

// genCheckShapeCall lowers `checkShape(ShapeName, value)` (weave_spec.md
// §4.3) into an inline sequence of per-field checks — sema has already
// validated ShapeName resolves to a shape(...) declaration, so this just
// re-derives it and expands, field by field (sorted by name for
// deterministic IR — Go map iteration order is randomized, and nothing
// here needs it to vary between builds; weave_spec.md's own for-in over
// an object sorts for the identical reason, Step 8's design decision).
// Each field's value is read via ObjGet (a plain map lookup, not
// reflect — weavert/object.go), then checked:
//
//   - number/string/bool/object: a genuine ASSERT, exactly like a typed
//     gotype/gomethod/gofunc boundary (goasset.go's genAssertOrTypeError)
//   - function: ASSERT can't express this one at all. Every Weave
//     closure is a bare (unnamed) `func(any) any` (closure.go's
//     genFuncLit emits `CLOS %x ^any : ^any` — never through a
//     pre-declared FNTYPE), and Go's type assertion requires the
//     interface's dynamic type to match *exactly*; an unnamed func
//     value can never satisfy an assertion to any named type, no matter
//     how the signature lines up. Rather than changing how every
//     closure in the program is represented just for this one check,
//     "function" instead calls weavert.IsWeaveFunc — a minimal,
//     reflect.Kind()-only helper. This is *not* the same "reflect tax"
//     genNativeGoMethodCall was designed to avoid (that was a full
//     reflect.Value.Call on every invocation); IsWeaveFunc never calls
//     the value, it just asks what kind it is, once, at check time
//
// A failed check panics via weavert.TypeError, exactly like a typed
// Go-asset boundary — checkShape is meant to fail fast and clearly on
// the first mismatch, not accumulate every field's errors into a report.
func genCheckShapeCall(fg *funcGen, call *ast.CallExpr) (string, error) {
	if len(call.Args) != 2 {
		return "", fmt.Errorf("line %d: checkShape(...) takes exactly two arguments, got %d", call.Line, len(call.Args))
	}
	shapeName := call.Args[0].(*ast.Ident).Name
	info := fg.ctx.shapes[shapeName]

	objVal, err := genExpr(fg, call.Args[1])
	if err != nil {
		return "", err
	}
	objVal = ensureVariable(fg, objVal)

	fields := make([]string, 0, len(info.Fields))
	for field := range info.Fields {
		fields = append(fields, field)
	}
	sort.Strings(fields)

	for _, field := range fields {
		hint := info.Fields[field]
		val := fg.newTemp("^any")
		fmt.Fprintf(&fg.body, "\tCALL\t%s\t:\t?weavert.ObjGet\t%s\t%s\n", val, objVal, strconv.Quote(field))
		desc := fmt.Sprintf("property %q of shape %s", field, shapeName)

		if goType, ok := shapeHintGoType[hint]; ok {
			genAssertOrTypeError(fg, val, goType, desc)
			continue
		}
		// hint == "function" (sema already rejected any other value).
		ok := fg.newTemp("^bool")
		fmt.Fprintf(&fg.body, "\tCALL\t%s\t:\t?weavert.IsWeaveFunc\t%s\n", ok, val)
		notOk := fg.newTemp("^bool")
		fmt.Fprintf(&fg.body, "\tNOT\t%s\t%s\n", notOk, ok)
		fmt.Fprintf(&fg.body, "\tIF\t%s\n", notOk)
		fmt.Fprintf(&fg.body, "\tCALL\t:\t?weavert.TypeError\t%s\t%s\t%s\n", strconv.Quote(desc), strconv.Quote("function"), val)
		fg.body.WriteString("\tENDIF\n")
	}
	return "nil", nil
}
