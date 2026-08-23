package codegen

import (
	"fmt"
	"strconv"

	"github.com/amisonnet8/weave/internal/ast"
)

// genObjectLit lowers `{ x: 1, y: 2 }` (weave_spec.md §3, §4.1) to a
// weavert.NewObject call followed by one weavert.ObjSet per field, in
// source order. Every Weave value — a number, string, bool, nil,
// another object, or a function value from genFuncLit — is already
// `any`, so it drops straight into ObjSet's value parameter with no
// extra handling: this is what lets the same object mix scalar and
// function-valued properties (weave_spec.md §4.2's `greet: fn(self)
// {...}` shape, exercised without Step 7's prototype/self-injection
// sugar — a plain `obj.greet(x)` already works as an ordinary property
// read followed by a call, see genPropExpr/genGeneralCall).
func genObjectLit(fg *funcGen, lit *ast.ObjectLit) (string, error) {
	obj := fg.newTemp("^any")
	fmt.Fprintf(&fg.body, "\tCALL\t%s\t:\t?weavert.NewObject\n", obj)
	for _, field := range lit.Fields {
		val, err := genExpr(fg, field.Value)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&fg.body, "\tCALL\t:\t?weavert.ObjSet\t%s\t%s\t%s\n", obj, quoteKey(field.Name), val)
	}
	return obj, nil
}

// genPropExpr lowers a property read `obj.name` (weave_spec.md §4.1) to
// weavert.ObjGet — see weavert/object.go's package doc comment for why
// this needs a runtime call rather than AMIVM's native MGET (obj is
// `^any`, and Go doesn't allow map indexing on an interface-typed
// variable).
func genPropExpr(fg *funcGen, e *ast.PropExpr) (string, error) {
	obj, err := genExpr(fg, e.Obj)
	if err != nil {
		return "", err
	}
	tmp := fg.newTemp("^any")
	fmt.Fprintf(&fg.body, "\tCALL\t%s\t:\t?weavert.ObjGet\t%s\t%s\n", tmp, obj, quoteKey(e.Prop))
	return tmp, nil
}

// genPropAssignStmt lowers a property write `obj.name = value`
// (weave_spec.md §4.1) to weavert.ObjSet, discarding its nil result.
func genPropAssignStmt(fg *funcGen, s *ast.PropAssignStmt) error {
	obj, err := genExpr(fg, s.Obj)
	if err != nil {
		return err
	}
	val, err := genExpr(fg, s.Value)
	if err != nil {
		return err
	}
	fmt.Fprintf(&fg.body, "\tCALL\t:\t?weavert.ObjSet\t%s\t%s\t%s\n", obj, quoteKey(s.Prop), val)
	return nil
}

// genIndexExpr lowers `list[index]` (weave_spec.md §3.1) to
// weavert.ObjAt — identical IR to at(list, index) (genBuiltinCall's own
// "at" case), since IndexExpr means exactly the same thing whenever it
// isn't the target of an assignment (see ast.IndexExpr's doc comment).
func genIndexExpr(fg *funcGen, e *ast.IndexExpr) (string, error) {
	obj, err := genExpr(fg, e.X)
	if err != nil {
		return "", err
	}
	idx, err := genExpr(fg, e.Index)
	if err != nil {
		return "", err
	}
	tmp := fg.newTemp("^any")
	fmt.Fprintf(&fg.body, "\tCALL\t%s\t:\t?weavert.ObjAt\t%s\t%s\n", tmp, obj, idx)
	return tmp, nil
}

// genIndexAssignStmt lowers `list[index] = value` (weave_spec.md §3.1)
// to weavert.ObjSetAt, discarding its nil result — the write-side
// counterpart to genIndexExpr, requiring the index to already exist
// (weavert.ObjSetAt's own doc comment explains why).
func genIndexAssignStmt(fg *funcGen, s *ast.IndexAssignStmt) error {
	obj, err := genExpr(fg, s.Obj)
	if err != nil {
		return err
	}
	idx, err := genExpr(fg, s.Index)
	if err != nil {
		return err
	}
	val, err := genExpr(fg, s.Value)
	if err != nil {
		return err
	}
	fmt.Fprintf(&fg.body, "\tCALL\t:\t?weavert.ObjSetAt\t%s\t%s\t%s\n", obj, idx, val)
	return nil
}

// quoteKey renders a property name as a Go string literal token — the
// same shape genExpr's *ast.StringLit case produces, since a property
// name is exactly a Weave string value at the weavert boundary
// (weavert.keyOf asserts it back).
func quoteKey(name string) string {
	return strconv.Quote(name)
}
