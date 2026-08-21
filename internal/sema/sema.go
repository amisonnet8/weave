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
//     code conversion, and the `__t0`, `__t1`, ... operator evaluation
//     temps introduced in Step 3 — see codegen.go's funcGen.newTemp).
//     A prefix rule scales better than enumerating every generated name
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

// Check validates file ahead of code generation.
//
// Step 2 scope adds tracking of which names have been assigned earlier
// in main's body — weave_spec.md §2 has no declaration keyword, so the
// first assignment introduces a name (§2's "代入されるまでnil" is about
// a name's *value* before assignment, not about skipping this check:
// Weave still requires some assignment to exist before a read, just not
// a dedicated `var`/`let` keyword). Reading an identifier before any
// assignment, or assigning to a reserved name, is an error. Full block
// scoping/shadowing (§10) arrives with control flow in Step 4.
//
// Per CLAUDE.md's "意味検証の責任分担", this stays narrower than
// Seed/Cascade's sema overall — most value-type errors are a runtime
// concern here (see weavert.ExitCode) — but name/scope rules like this
// one are exactly what sema is still responsible for.
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

	declared := map[string]bool{}
	for _, stmt := range file.Main.Body {
		if err := checkStmt(stmt, declared); err != nil {
			return err
		}
	}
	return nil
}

func checkStmt(stmt ast.Stmt, declared map[string]bool) error {
	switch s := stmt.(type) {
	case *ast.AssignStmt:
		if err := checkExpr(s.Value, declared); err != nil {
			return err
		}
		if why, ok := reservedName(s.Name); ok {
			return fmt.Errorf("line %d: %q is a reserved name (%s)", s.Line, s.Name, why)
		}
		declared[s.Name] = true
		return nil
	case *ast.ExprStmt:
		return checkExpr(s.X, declared)
	case *ast.ReturnStmt:
		if s.Value == nil {
			return nil
		}
		return checkExpr(s.Value, declared)
	default:
		return fmt.Errorf("sema: unsupported statement %T", stmt)
	}
}

// checkExpr validates identifier references. A CallExpr's own Callee is
// deliberately not checked here: through Step 2 it is always a builtin
// name (e.g. `print`), a reserved word rather than a variable (see
// lexer.go's doc comment on structural vs. builtin keywords) — codegen
// separately rejects any callee it doesn't recognize.
func checkExpr(expr ast.Expr, declared map[string]bool) error {
	switch e := expr.(type) {
	case *ast.Ident:
		if !declared[e.Name] {
			return fmt.Errorf("line %d: undefined name %q", e.Line, e.Name)
		}
		return nil
	case *ast.NumberLit, *ast.StringLit, *ast.BoolLit, *ast.NilLit:
		return nil
	case *ast.CallExpr:
		for _, arg := range e.Args {
			if err := checkExpr(arg, declared); err != nil {
				return err
			}
		}
		return nil
	case *ast.BinaryExpr:
		if err := checkExpr(e.X, declared); err != nil {
			return err
		}
		return checkExpr(e.Y, declared)
	case *ast.UnaryExpr:
		return checkExpr(e.X, declared)
	default:
		return fmt.Errorf("sema: unsupported expression %T", expr)
	}
}
