package codegen

import (
	"fmt"
	"strings"

	"github.com/amisonnet8/weave/internal/ast"
)

// genFuncLit compiles a function literal (weave_spec.md §5) into a
// fresh, independent top-level FUNC — never a CLOS — plus a
// weavert.NewClosure call at the point the literal appears, capturing
// its free variables (see freeVars) into a weavert.Env snapshot.
//
// See weavert/closure.go's package doc comment for why this, rather
// than AMIVM's own CLOS, is how every Weave function literal compiles:
// CLOS cannot nest inside another CLOS's body (amivm_spec.md §2.1), but
// currying (`fn(a) fn(b) {...}`, a closure whose body itself evaluates
// to another closure that captures `a`) needs exactly that nesting if
// translated naively.
//
// Nested currying falls out of this for free: when a curried FuncLit's
// body is (after parser.go's flattening) itself a `return`ed FuncLit,
// that inner literal's own genFuncLit call runs while the OUTER
// literal's own funcGen (`inner` below, from the outer call) is the
// active one — so the inner closure's captured variables, which may
// include the outer's own Param, are read exactly like any other
// %-token already sitting in that scope by the time the inner literal's
// call site runs. No special-casing is needed for arbitrary curry depth.
func genFuncLit(fg *funcGen, lit *ast.FuncLit) (string, error) {
	captured := freeVars(lit)

	inner := newFuncGen(fg.ctx)
	inner.declare(lit.Param, "^any")
	fmt.Fprintf(&inner.body, "\tSET\t%%%s\t$2\n", lit.Param)
	for i, name := range captured {
		inner.declare(name, "^any")
		fmt.Fprintf(&inner.body, "\tAGET\t%%%s\t$1\t%d\n", name, i)
	}
	if err := genBlock(inner, lit.Body); err != nil {
		return "", err
	}

	closureName := fmt.Sprintf("closure%d", fg.ctx.closureCount)
	fg.ctx.closureCount++

	var fn strings.Builder
	fmt.Fprintf(&fn, "FUNC\t!%s\t^%s\t^any\t:\t^any\n", closureName, envType)
	for _, d := range inner.decls {
		fn.WriteString(d)
	}
	fn.WriteString(inner.body.String())
	fn.WriteString("ENDFUNC\n")
	fg.ctx.closureFuncs = append(fg.ctx.closureFuncs, fn.String())

	envVar := fg.newTemp("^" + envType)
	fmt.Fprintf(&fg.body, "\tSLMAKE\t%s\t^%s\t%d\n", envVar, envType, len(captured))
	for i, name := range captured {
		fmt.Fprintf(&fg.body, "\tASET\t%s\t%d\t%%%s\n", envVar, i, name)
	}

	result := fg.newTemp("^any")
	fmt.Fprintf(&fg.body, "\tCALL\t%s\t:\t?weavert.NewClosure\t!%s\t%s\n", result, closureName, envVar)
	return result, nil
}

// envType is the name of the single, program-wide SLTYPE (`[]any`,
// declared once in Generate if any closures exist) every closure FUNC's
// env parameter uses. It must be a plain local type — SLMAKE's deftype
// operand rejects a package-qualified type like `weavert.Env` outright
// (confirmed by amivm's own error message), which is why weavert.Call
// resorts to reflection instead of a statically-typed function value —
// see weavert/closure.go's package doc comment.
const envType = "WeaveEnv"

// freeVars returns lit's free variables — identifiers read in its body
// (transitively, through nested statements and nested FuncLits) that
// resolve to neither lit's own Param nor a name assigned earlier within
// reach of that read — in deterministic first-use order. These are
// exactly the names genFuncLit must capture into the closure's
// weavert.Env.
//
// This mirrors sema.scope's own chain resolution, but only to classify
// bound vs. free: by the time codegen runs, sema has already guaranteed
// every identifier resolves somewhere, so unlike sema this never needs
// to report an error.
func freeVars(lit *ast.FuncLit) []string {
	w := &freeVarWalker{
		bound: []map[string]bool{{lit.Param: true}},
		seen:  map[string]bool{},
	}
	for _, stmt := range lit.Body {
		w.stmt(stmt)
	}
	return w.free
}

type freeVarWalker struct {
	bound []map[string]bool // scope chain, innermost last
	free  []string
	seen  map[string]bool
}

func (w *freeVarWalker) isBound(name string) bool {
	for _, b := range w.bound {
		if b[name] {
			return true
		}
	}
	return false
}

func (w *freeVarWalker) markFree(name string) {
	if !w.isBound(name) && !w.seen[name] {
		w.seen[name] = true
		w.free = append(w.free, name)
	}
}

// block walks stmts in a fresh child scope, mirroring
// sema.checker.checkBlock (weave_spec.md §10: if/while bodies are their
// own scope).
func (w *freeVarWalker) block(stmts []ast.Stmt) {
	w.bound = append(w.bound, map[string]bool{})
	for _, stmt := range stmts {
		w.stmt(stmt)
	}
	w.bound = w.bound[:len(w.bound)-1]
}

func (w *freeVarWalker) stmt(stmt ast.Stmt) {
	switch s := stmt.(type) {
	case *ast.AssignStmt:
		w.expr(s.Value)
		w.bound[len(w.bound)-1][s.Name] = true
	case *ast.ExprStmt:
		w.expr(s.X)
	case *ast.ReturnStmt:
		if s.Value != nil {
			w.expr(s.Value)
		}
	case *ast.IfStmt:
		for _, clause := range s.Clauses {
			w.expr(clause.Cond)
			w.block(clause.Body)
		}
		if s.Else != nil {
			w.block(s.Else)
		}
	case *ast.WhileStmt:
		w.expr(s.Cond)
		w.block(s.Body)
	case *ast.PropAssignStmt:
		w.expr(s.Obj)
		w.expr(s.Value)
	case *ast.BreakStmt, *ast.ContinueStmt:
		// no identifiers
	}
}

func (w *freeVarWalker) expr(expr ast.Expr) {
	switch e := expr.(type) {
	case *ast.Ident:
		w.markFree(e.Name)
	case *ast.NumberLit, *ast.StringLit, *ast.BoolLit, *ast.NilLit:
		// no identifiers
	case *ast.CallExpr:
		if callee, ok := e.Callee.(*ast.Ident); !ok || !builtinNames[callee.Name] {
			w.expr(e.Callee)
		}
		for _, a := range e.Args {
			w.expr(a)
		}
	case *ast.BinaryExpr:
		w.expr(e.X)
		w.expr(e.Y)
	case *ast.UnaryExpr:
		w.expr(e.X)
	case *ast.FuncLit:
		// A nested literal's own free variables, aside from whatever it
		// finds already bound at this point, are free variables of THIS
		// literal too — the transitive capture that makes arbitrarily
		// deep currying work (see genFuncLit's doc comment).
		for _, name := range freeVars(e) {
			w.markFree(name)
		}
	case *ast.ObjectLit:
		for _, field := range e.Fields {
			w.expr(field.Value)
		}
	case *ast.PropExpr:
		w.expr(e.Obj)
	}
}
