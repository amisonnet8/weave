package sema

import (
	"fmt"
	"strings"

	"github.com/amisonnet8/weave/internal/ast"
)

// reservedName reports whether name is off-limits for a user assignment
// because it would collide with a name codegen generates for itself.
//
//   - "weave_main" is the internal bridge name for the compiled `main`
//     (weave_spec.md §12; kept in sync manually with codegen.go's
//     weaveMainFunc — see CLAUDE.md's "確定した設計判断").
//   - Any name starting with "__" is reserved wholesale for codegen's
//     own compiler-generated temporaries (e.g. the `__exitcode` exit
//     code conversion, and the `__t0`, `__t1`, ... operator/condition
//     evaluation temps — see codegen.go's funcGen.newTemp). A prefix
//     rule scales better than enumerating every generated name
//     individually, since the temp count grows with expression
//     complexity and isn't a fixed set.
func reservedName(name string) (string, bool) {
	if name == "weave_main" {
		return "reserved for the compiled entry point (weave_spec.md §12)", true
	}
	if strings.HasPrefix(name, "__") {
		return "names starting with `__` are reserved for the compiler's own use", true
	}
	return "", false
}

// scope is one block's set of freshly-introduced names, chained to its
// enclosing block (weave_spec.md §10: "if/while/for/func/関数リテラルの
// 本体ごとに新しいスコープ").
//
// weave_spec.md §10 says shadowing is allowed but doesn't spell out how
// assignment picks between "reassign an existing outer binding" and
// "introduce a new inner one" — and the two readings disagree on §7's
// own `while i < 10 { i = i + 1 }` example: if same-name assignment
// always shadowed, that loop would never terminate (the inner `i` would
// be a fresh binding each iteration, and the outer `i` tested by the
// condition would never change). So Check resolves it the only way
// consistent with that example: assignment reuses the nearest existing
// binding anywhere in the enclosing chain, and only introduces a new
// (block-local) binding when the name isn't visible anywhere yet — see
// declareIfNew. This is recorded as a resolved design question in
// CLAUDE.md's Step 4 "確定した設計判断".
type scope struct {
	parent   *scope
	declared map[string]bool
}

func newScope(parent *scope) *scope {
	return &scope{parent: parent, declared: map[string]bool{}}
}

// has reports whether name is visible in sc or any enclosing scope.
func (sc *scope) has(name string) bool {
	for s := sc; s != nil; s = s.parent {
		if s.declared[name] {
			return true
		}
	}
	return false
}

// declareIfNew records name as a fresh binding of sc itself, but only if
// it isn't already visible somewhere in the enclosing chain (see scope's
// doc comment) — an assignment to an already-visible name is always a
// plain reassignment of that existing binding, not a new one.
func (sc *scope) declareIfNew(name string) {
	if !sc.has(name) {
		sc.declared[name] = true
	}
}

// checker carries state that spans a single Check call but isn't scoped
// to one block — currently just loop nesting, for validating break/
// continue (weave_spec.md §7: valid only inside a loop).
type checker struct {
	loopDepth int
}

// Check validates file ahead of code generation.
//
// weave_spec.md §2 has no declaration keyword, so the first assignment
// to a name (in whichever scope reaches it first) introduces it; reading
// an identifier that isn't visible anywhere in the enclosing scope
// chain, assigning to a reserved name, or using break/continue outside
// a loop are all errors.
//
// Per CLAUDE.md's "意味検証の責任分担", this stays narrower than
// Seed/Cascade's sema overall — most value-type errors are a runtime
// concern here (see weavert.ExitCode, weavert.CheckBool) — but name/
// scope rules like these are exactly what sema is still responsible for.
func Check(file *ast.File) error {
	if file.Main == nil {
		return fmt.Errorf("missing entry point: expected `func main(): int { ... }`")
	}
	if file.Main.Name != "main" {
		return fmt.Errorf("line %d: `func` may only declare `main` (weave_spec.md §12), got %q", file.Main.Line, file.Main.Name)
	}
	if file.Main.ReturnType != "int" {
		return fmt.Errorf("line %d: main must return `int`, got %q", file.Main.Line, file.Main.ReturnType)
	}

	c := &checker{}
	root := newScope(nil)
	for _, stmt := range file.Main.Body {
		if err := c.checkStmt(stmt, root); err != nil {
			return err
		}
	}
	return nil
}

// checkBlock checks stmts in a fresh child scope of parent, so names
// introduced only inside this block (and not already visible in an
// enclosing scope) stop being visible once the block ends.
func (c *checker) checkBlock(stmts []ast.Stmt, parent *scope) error {
	child := newScope(parent)
	for _, stmt := range stmts {
		if err := c.checkStmt(stmt, child); err != nil {
			return err
		}
	}
	return nil
}

func (c *checker) checkStmt(stmt ast.Stmt, sc *scope) error {
	switch s := stmt.(type) {
	case *ast.AssignStmt:
		if err := checkExpr(s.Value, sc); err != nil {
			return err
		}
		if why, ok := reservedName(s.Name); ok {
			return fmt.Errorf("line %d: %q is a reserved name (%s)", s.Line, s.Name, why)
		}
		sc.declareIfNew(s.Name)
		return nil
	case *ast.ExprStmt:
		return checkExpr(s.X, sc)
	case *ast.ReturnStmt:
		if s.Value == nil {
			return nil
		}
		return checkExpr(s.Value, sc)
	case *ast.IfStmt:
		for _, clause := range s.Clauses {
			if err := checkExpr(clause.Cond, sc); err != nil {
				return err
			}
			if err := c.checkBlock(clause.Body, sc); err != nil {
				return err
			}
		}
		if s.Else != nil {
			if err := c.checkBlock(s.Else, sc); err != nil {
				return err
			}
		}
		return nil
	case *ast.WhileStmt:
		if err := checkExpr(s.Cond, sc); err != nil {
			return err
		}
		c.loopDepth++
		err := c.checkBlock(s.Body, sc)
		c.loopDepth--
		return err
	case *ast.BreakStmt:
		if c.loopDepth == 0 {
			return fmt.Errorf("line %d: `break` outside of a loop (weave_spec.md §7)", s.Line)
		}
		return nil
	case *ast.ContinueStmt:
		if c.loopDepth == 0 {
			return fmt.Errorf("line %d: `continue` outside of a loop (weave_spec.md §7)", s.Line)
		}
		return nil
	default:
		return fmt.Errorf("sema: unsupported statement %T", stmt)
	}
}

// checkExpr validates identifier references. A CallExpr's own Callee is
// deliberately not checked here: through Step 4 it is always a builtin
// name (e.g. `print`), a reserved word rather than a variable (see
// lexer.go's doc comment on structural vs. builtin keywords) — codegen
// separately rejects any callee it doesn't recognize.
func checkExpr(expr ast.Expr, sc *scope) error {
	switch e := expr.(type) {
	case *ast.Ident:
		if !sc.has(e.Name) {
			return fmt.Errorf("line %d: undefined name %q", e.Line, e.Name)
		}
		return nil
	case *ast.NumberLit, *ast.StringLit, *ast.BoolLit, *ast.NilLit:
		return nil
	case *ast.CallExpr:
		for _, arg := range e.Args {
			if err := checkExpr(arg, sc); err != nil {
				return err
			}
		}
		return nil
	case *ast.BinaryExpr:
		if err := checkExpr(e.X, sc); err != nil {
			return err
		}
		return checkExpr(e.Y, sc)
	case *ast.UnaryExpr:
		return checkExpr(e.X, sc)
	default:
		return fmt.Errorf("sema: unsupported expression %T", expr)
	}
}
