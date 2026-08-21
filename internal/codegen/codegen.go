package codegen

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/amisonnet8/weave/internal/ast"
)

// weaveMainFunc is the internal AMIVM function name the user's `main`
// compiles to. Go's own main can't take Weave main's `int` return value
// directly (func main must have no arguments and no return values), so
// the generated `!main` is a thin wrapper that calls !weave_main and
// turns its returned exit code into os.Exit — the same bridge pattern
// Seed (!seed_main) and Cascade (!cascade_main) use (weave_spec.md §12;
// see those projects' CLAUDE.md "確定した設計判断" for the precedent).
const weaveMainFunc = "weave_main"

// Generate lowers a checked *ast.File into AMIVM-IR text.
func Generate(file *ast.File) (string, error) {
	var body strings.Builder
	for _, stmt := range file.Main.Body {
		if err := genStmt(&body, stmt); err != nil {
			return "", err
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "FUNC\t!%s\t:\t^int\n", weaveMainFunc)
	b.WriteString(body.String())
	b.WriteString("ENDFUNC\n")

	b.WriteString("FUNC\t!main\t:\n")
	b.WriteString("\tVAR\t%exitcode\t^int\n")
	fmt.Fprintf(&b, "\tCALL\t%%exitcode\t:\t!%s\n", weaveMainFunc)
	b.WriteString("\tCALL\t:\t?os.Exit\t%exitcode\n")
	b.WriteString("\tRET\n")
	b.WriteString("ENDFUNC\n")

	return b.String(), nil
}

func genStmt(b *strings.Builder, stmt ast.Stmt) error {
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		return genExprStmt(b, s)
	case *ast.ReturnStmt:
		return genReturnStmt(b, s)
	default:
		return fmt.Errorf("codegen: unsupported statement %T", stmt)
	}
}

func genExprStmt(b *strings.Builder, s *ast.ExprStmt) error {
	call, ok := s.X.(*ast.CallExpr)
	if !ok {
		return fmt.Errorf("line %d: only call expressions are supported as statements (Step 1 scope)", exprLine(s.X))
	}
	callee, ok := call.Callee.(*ast.Ident)
	if !ok {
		return fmt.Errorf("line %d: unsupported call target (Step 1 scope)", call.Line)
	}
	switch callee.Name {
	case "print":
		return genPrintCall(b, call)
	default:
		return fmt.Errorf("line %d: builtin %q not yet implemented", call.Line, callee.Name)
	}
}

// genPrintCall lowers print("literal") to a direct ?fmt.Println call.
// This is deliberately narrow: print's real signature takes any Weave
// value (weave_spec.md §11) and needs the dynamic value representation
// that Step 2 introduces (CLAUDE.md's design question 1). For Step 1's
// hello-world bootstrap, only a single string-literal argument is
// supported.
func genPrintCall(b *strings.Builder, call *ast.CallExpr) error {
	if len(call.Args) != 1 {
		return fmt.Errorf("line %d: print(...) with %d arguments not yet implemented (Step 1 supports a single string literal)", call.Line, len(call.Args))
	}
	lit, ok := call.Args[0].(*ast.StringLit)
	if !ok {
		return fmt.Errorf("line %d: print(...) of a non-string-literal argument not yet implemented", call.Line)
	}
	fmt.Fprintf(b, "\tCALL\t:\t?fmt.Println\t%s\n", strconv.Quote(lit.Value))
	return nil
}

func genReturnStmt(b *strings.Builder, s *ast.ReturnStmt) error {
	if s.Value == nil {
		return fmt.Errorf("line %d: main must return an int exit code (weave_spec.md §12)", s.Line)
	}
	lit, ok := s.Value.(*ast.NumberLit)
	if !ok {
		return fmt.Errorf("line %d: return value not yet implemented (Step 1 supports a literal integer exit code)", exprLine(s.Value))
	}
	if lit.Value != float64(int64(lit.Value)) {
		return fmt.Errorf("line %d: main's exit code must be a whole number, got %v", lit.Line, lit.Value)
	}
	fmt.Fprintf(b, "\tRET\t%d\n", int64(lit.Value))
	return nil
}

func exprLine(x ast.Expr) int {
	switch x := x.(type) {
	case *ast.Ident:
		return x.Line
	case *ast.NumberLit:
		return x.Line
	case *ast.StringLit:
		return x.Line
	case *ast.CallExpr:
		return x.Line
	default:
		return 0
	}
}
