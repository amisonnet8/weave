package modloader

import (
	"fmt"

	"github.com/amisonnet8/weave/internal/ast"
)

// rewriter carries one package's own view of the two things modloader
// needs to resolve syntactically, before sema/codegen ever run:
//
//   - renames: this package's own bare top-level name -> already-renamed
//     flat name (weave_spec.md §17.4). Empty/nil for the root package,
//     whose own bindings are never renamed.
//   - quals: this package's own `name = package("...")` bindings,
//     resolved to the already-loaded target package (weave_spec.md
//     §17.2), keyed by their *original* (pre-rename) name — see
//     extractPackageDecls's doc for why.
//
// rewriteStmts walks a statement list (and everything reachable from it —
// nested blocks, closures, object literals) exactly once, doing two
// things in the same pass: renaming references to this package's own
// top-level bindings (renames), and resolving `qualifier.name` into a
// direct reference to the target package's flat name (quals, checking
// that the member is exported). This walk is purely syntactic (it runs
// before sema builds any real scope information): a local binding
// genuinely named the same as a qualifier, or as one of this package's
// own renamed top-level bindings, would be silently mis-rewritten too —
// accepted as a known limitation (matching Cascade's own pkgloader, and
// — since the module redesign that dropped the original `import`/`pub`
// syntax — matching how sema/codegen's own goStaticVars quietly forgets
// a gofunc-tracked variable's static typing on reassignment rather than
// erroring, CLAUDE.md's 確定した設計判断).
type rewriter struct {
	renames map[string]string
	quals   map[string]*loadedPackage
}

func (rw *rewriter) rewriteStmts(stmts []ast.Stmt) error {
	for _, s := range stmts {
		if err := rw.rewriteStmt(s); err != nil {
			return err
		}
	}
	return nil
}

func (rw *rewriter) rewriteStmt(stmt ast.Stmt) error {
	switch s := stmt.(type) {
	case *ast.AssignStmt:
		v, err := rw.rewriteExpr(s.Value)
		if err != nil {
			return err
		}
		s.Value = v
		return nil
	case *ast.ExprStmt:
		v, err := rw.rewriteExpr(s.X)
		if err != nil {
			return err
		}
		s.X = v
		return nil
	case *ast.ReturnStmt:
		if s.Value == nil {
			return nil
		}
		v, err := rw.rewriteExpr(s.Value)
		if err != nil {
			return err
		}
		s.Value = v
		return nil
	case *ast.IfStmt:
		for i := range s.Clauses {
			c, err := rw.rewriteExpr(s.Clauses[i].Cond)
			if err != nil {
				return err
			}
			s.Clauses[i].Cond = c
			if err := rw.rewriteStmts(s.Clauses[i].Body); err != nil {
				return err
			}
		}
		if s.Else != nil {
			if err := rw.rewriteStmts(s.Else); err != nil {
				return err
			}
		}
		return nil
	case *ast.WhileStmt:
		c, err := rw.rewriteExpr(s.Cond)
		if err != nil {
			return err
		}
		s.Cond = c
		return rw.rewriteStmts(s.Body)
	case *ast.BreakStmt, *ast.ContinueStmt:
		return nil
	case *ast.PropAssignStmt:
		o, err := rw.rewriteExpr(s.Obj)
		if err != nil {
			return err
		}
		s.Obj = o
		v, err := rw.rewriteExpr(s.Value)
		if err != nil {
			return err
		}
		s.Value = v
		return nil
	case *ast.ForStmt:
		o, err := rw.rewriteExpr(s.Obj)
		if err != nil {
			return err
		}
		s.Obj = o
		return rw.rewriteStmts(s.Body)
	default:
		return fmt.Errorf("modloader: unsupported statement %T", stmt)
	}
}

func (rw *rewriter) rewriteExpr(expr ast.Expr) (ast.Expr, error) {
	switch e := expr.(type) {
	case *ast.Ident:
		if flat, ok := rw.renames[e.Name]; ok {
			e.Name = flat
		}
		return e, nil
	case *ast.NumberLit, *ast.StringLit, *ast.BoolLit, *ast.NilLit:
		return e, nil
	case *ast.CallExpr:
		callee, err := rw.rewriteExpr(e.Callee)
		if err != nil {
			return nil, err
		}
		e.Callee = callee
		for i, a := range e.Args {
			v, err := rw.rewriteExpr(a)
			if err != nil {
				return nil, err
			}
			e.Args[i] = v
		}
		return e, nil
	case *ast.BinaryExpr:
		x, err := rw.rewriteExpr(e.X)
		if err != nil {
			return nil, err
		}
		e.X = x
		y, err := rw.rewriteExpr(e.Y)
		if err != nil {
			return nil, err
		}
		e.Y = y
		return e, nil
	case *ast.UnaryExpr:
		x, err := rw.rewriteExpr(e.X)
		if err != nil {
			return nil, err
		}
		e.X = x
		return e, nil
	case *ast.FuncLit:
		if err := rw.rewriteStmts(e.Body); err != nil {
			return nil, err
		}
		return e, nil
	case *ast.ObjectLit:
		for i := range e.Fields {
			v, err := rw.rewriteExpr(e.Fields[i].Value)
			if err != nil {
				return nil, err
			}
			e.Fields[i].Value = v
		}
		return e, nil
	case *ast.PropExpr:
		resolved, handled, err := rw.tryResolveQualified(e)
		if err != nil {
			return nil, err
		}
		if handled {
			return resolved, nil
		}
		obj, err := rw.rewriteExpr(e.Obj)
		if err != nil {
			return nil, err
		}
		e.Obj = obj
		return e, nil
	default:
		return nil, fmt.Errorf("modloader: unsupported expression %T", expr)
	}
}

// tryResolveQualified checks whether e is a `qualifier.name` reference
// (weave_spec.md §17.2: Obj is a bare Ident matching one of this
// package's own live `package(...)` qualifiers) and, if so, resolves it
// directly to the target package's already-renamed flat name —
// returning it as a plain *ast.Ident, never as a PropExpr. This is what
// keeps §9's self-injection method-call sugar from ever seeing (and
// misinterpreting) a qualified reference: by the time genGeneralCall
// looks at `CallExpr.Callee`, `mathutil.Clamp` has already become the
// plain identifier `mathutil_Clamp`, not a PropExpr — so
// `mathutil.Clamp(15, 0, 10)` compiles as an ordinary curried call, never
// as `Clamp(mathutil, 15, 0, 10)`.
//
// handled is true whenever e.Obj matches a known qualifier at all (even
// if resolution then fails, e.g. an unknown or unexported member) — the
// caller must not fall through to ordinary property-access handling in
// that case, since a qualifier is never itself a real runtime value (see
// this package's own doc comment), so "obj.prop where obj happens to be
// a live qualifier" can only be a genuine qualified reference or a
// genuine error, never an ordinary property read.
func (rw *rewriter) tryResolveQualified(e *ast.PropExpr) (resolved ast.Expr, handled bool, err error) {
	id, ok := e.Obj.(*ast.Ident)
	if !ok {
		return nil, false, nil
	}
	target, ok := rw.quals[id.Name]
	if !ok {
		return nil, false, nil
	}
	if !isExported(e.Prop) {
		return nil, true, fmt.Errorf("line %d: %q is not exported from package %q (only names starting with an uppercase letter are visible outside their own package, weave_spec.md §17.2)", e.Line, e.Prop, target.Name)
	}
	flat, ok := target.Renames[e.Prop]
	if !ok {
		return nil, true, fmt.Errorf("line %d: package %q has no member %q", e.Line, target.Name, e.Prop)
	}
	return &ast.Ident{Name: flat, Line: e.Line}, true, nil
}
