package sema

import (
	"fmt"

	"github.com/amisonnet8/weave/internal/ast"
)

// reservedNames mirrors codegen's internal temp/bridge names — a user
// assignment to one of these would collide with generated code. Kept in
// sync manually with codegen.go's own copies (weaveMainFunc,
// exitCodeTemp), matching Seed's seed_main precedent (see CLAUDE.md's
// "確定した設計判断").
var reservedNames = map[string]string{
	"weave_main": "reserved for the compiled entry point (weave_spec.md §12)",
	"__exitcode": "reserved for `return`'s internal exit-code conversion",
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
		if why, ok := reservedNames[s.Name]; ok {
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
	default:
		return fmt.Errorf("sema: unsupported expression %T", expr)
	}
}
