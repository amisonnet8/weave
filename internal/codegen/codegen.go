package codegen

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/amisonnet8/weave/internal/ast"
)

// weaveMainFunc/exitCodeTemp are codegen's own internal names — see
// sema.go's reservedNames (kept in sync manually; CLAUDE.md's "確定した
// 設計判断" documents why).
const (
	// weaveMainFunc is the internal AMIVM function name the user's
	// `main` compiles to. Go's own main can't take Weave main's `int`
	// return value directly (func main must have no arguments and no
	// return values), so the generated `!main` is a thin wrapper that
	// calls !weave_main and turns its returned exit code into os.Exit —
	// the same bridge pattern Seed (!seed_main) and Cascade
	// (!cascade_main) use (weave_spec.md §12).
	weaveMainFunc = "weave_main"

	// exitCodeTemp is the hoisted ^int temp that main's `return <expr>`
	// converts through (see genReturnStmt/weavert.ExitCode).
	exitCodeTemp = "__exitcode"
)

// funcGen accumulates one AMIVM FUNC body's VAR declarations (hoisted to
// the top, in first-declared order) and its SET/CALL/RET instructions
// (left in source order). Weave has no block scoping yet (that arrives
// with control flow in Step 4), so a single flat `declared` set is
// enough for now — see seed_implementation_notes.md §1 for why VARs
// must be hoisted ahead of any future GOTO regardless.
type funcGen struct {
	decls    []string
	declared map[string]bool
	body     strings.Builder
}

func newFuncGen() *funcGen {
	return &funcGen{declared: map[string]bool{}}
}

// declare emits a hoisted `VAR %name irType` line the first time name is
// seen; later calls for the same name are no-ops (an AMIVM VAR may only
// be declared once per function).
func (fg *funcGen) declare(name, irType string) {
	if fg.declared[name] {
		return
	}
	fg.declared[name] = true
	fg.decls = append(fg.decls, fmt.Sprintf("\tVAR\t%%%s\t%s\n", name, irType))
}

// Generate lowers a checked *ast.File into AMIVM-IR text.
func Generate(file *ast.File) (string, error) {
	fg := newFuncGen()
	for _, stmt := range file.Main.Body {
		if err := genStmt(fg, stmt); err != nil {
			return "", err
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "FUNC\t!%s\t:\t^int\n", weaveMainFunc)
	for _, d := range fg.decls {
		b.WriteString(d)
	}
	b.WriteString(fg.body.String())
	b.WriteString("ENDFUNC\n")

	b.WriteString("FUNC\t!main\t:\n")
	b.WriteString("\tVAR\t%exitcode\t^int\n")
	fmt.Fprintf(&b, "\tCALL\t%%exitcode\t:\t!%s\n", weaveMainFunc)
	b.WriteString("\tCALL\t:\t?os.Exit\t%exitcode\n")
	b.WriteString("\tRET\n")
	b.WriteString("ENDFUNC\n")

	return b.String(), nil
}

func genStmt(fg *funcGen, stmt ast.Stmt) error {
	switch s := stmt.(type) {
	case *ast.AssignStmt:
		return genAssignStmt(fg, s)
	case *ast.ExprStmt:
		return genExprStmt(fg, s)
	case *ast.ReturnStmt:
		return genReturnStmt(fg, s)
	default:
		return fmt.Errorf("codegen: unsupported statement %T", stmt)
	}
}

// genAssignStmt lowers `name = value` to a hoisted `VAR %name ^any`
// (first occurrence only) plus a `SET` in source order. Every Weave
// variable is Go `any` (see genValue's doc comment) — and Go's own zero
// value for `any` is nil, which already matches weave_spec.md §2's "a
// variable is nil until assigned" without needing Seed/Cascade's
// separate `_isset` flag.
func genAssignStmt(fg *funcGen, s *ast.AssignStmt) error {
	val, err := genValue(s.Value)
	if err != nil {
		return err
	}
	fg.declare(s.Name, "^any")
	fmt.Fprintf(&fg.body, "\tSET\t%%%s\t%s\n", s.Name, val)
	return nil
}

func genExprStmt(fg *funcGen, s *ast.ExprStmt) error {
	call, ok := s.X.(*ast.CallExpr)
	if !ok {
		return fmt.Errorf("line %d: only call expressions are supported as statements (Step 1-2 scope)", exprLine(s.X))
	}
	callee, ok := call.Callee.(*ast.Ident)
	if !ok {
		return fmt.Errorf("line %d: unsupported call target (Step 1-2 scope)", call.Line)
	}
	switch callee.Name {
	case "print":
		return genPrintCall(fg, call)
	default:
		return fmt.Errorf("line %d: builtin %q not yet implemented", call.Line, callee.Name)
	}
}

// genPrintCall lowers print(x) to a ?weavert.Print call, which owns the
// nil→"nil" rendering Go's own fmt.Println doesn't provide (Go's zero
// value for `any` prints as "<nil>"; weave_spec.md §2's nil should read
// as "nil" — see weavert/weavert.go).
func genPrintCall(fg *funcGen, call *ast.CallExpr) error {
	if len(call.Args) != 1 {
		return fmt.Errorf("line %d: print(...) with %d arguments not yet implemented (Step 1-2 support a single argument)", call.Line, len(call.Args))
	}
	val, err := genValue(call.Args[0])
	if err != nil {
		return err
	}
	fmt.Fprintf(&fg.body, "\tCALL\t:\t?weavert.Print\t%s\n", val)
	return nil
}

// genReturnStmt lowers `return <expr>` in main through weavert.ExitCode
// (a runtime type assertion + conversion to Go int — see its doc
// comment) into a hoisted ^int temp, then RETs that temp. main's
// declared return type is the one place Weave's dynamic value model
// touches a native Go type instead of `any`.
func genReturnStmt(fg *funcGen, s *ast.ReturnStmt) error {
	if s.Value == nil {
		return fmt.Errorf("line %d: main must return an int exit code (weave_spec.md §12)", s.Line)
	}
	val, err := genValue(s.Value)
	if err != nil {
		return err
	}
	fg.declare(exitCodeTemp, "^int")
	fmt.Fprintf(&fg.body, "\tCALL\t%%%s\t:\t?weavert.ExitCode\t%s\n", exitCodeTemp, val)
	fmt.Fprintf(&fg.body, "\tRET\t%%%s\n", exitCodeTemp)
	return nil
}

// genValue returns the AMIVM `value` token for expr: either a literal
// token or a `%name` variable reference. Every Weave value is
// represented as Go `any` (weave_spec.md §2 — see CLAUDE.md's Weave特有
// の設計課題 1, resolved in Step 2), so this is the single place that
// turns a literal AST node into IR text.
func genValue(expr ast.Expr) (string, error) {
	switch e := expr.(type) {
	case *ast.Ident:
		return "%" + e.Name, nil
	case *ast.NumberLit:
		return formatNumberLiteral(e.Value), nil
	case *ast.StringLit:
		return strconv.Quote(e.Value), nil
	case *ast.BoolLit:
		if e.Value {
			return "true", nil
		}
		return "false", nil
	case *ast.NilLit:
		return "nil", nil
	default:
		return "", fmt.Errorf("line %d: expression not yet implemented", exprLine(expr))
	}
}

// formatNumberLiteral renders v as a Go float literal that is
// unambiguously untyped-float-shaped (i.e. always contains a `.` or an
// exponent marker). This matters because Weave does not distinguish
// integers from floats (weave_spec.md §2: unified as float64) — but an
// AMIVM `value` token like `1234` embeds verbatim as `1234` in the
// generated Go, and an untyped integer constant assigned into an `any`
// defaults to Go's `int`, not `float64` (confirmed by direct probe — see
// CLAUDE.md's Step 2 "確定した設計判断"). Forcing a decimal point makes
// Go infer the untyped-float default instead, which does convert to
// float64.
func formatNumberLiteral(v float64) string {
	s := strconv.FormatFloat(v, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

func exprLine(x ast.Expr) int {
	switch x := x.(type) {
	case *ast.Ident:
		return x.Line
	case *ast.NumberLit:
		return x.Line
	case *ast.StringLit:
		return x.Line
	case *ast.BoolLit:
		return x.Line
	case *ast.NilLit:
		return x.Line
	case *ast.CallExpr:
		return x.Line
	default:
		return 0
	}
}
