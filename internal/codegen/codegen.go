package codegen

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/amisonnet8/weave/internal/ast"
)

// weaveMainFunc/exitCodeTemp are codegen's own internal names — see
// sema.go's reservedName (kept in sync manually; CLAUDE.md's "確定した
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
	decls     []string
	declared  map[string]bool
	body      strings.Builder
	tempCount int
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

// newTemp declares and returns the name (without a leading `%`) of a
// fresh `^any` temp for holding an intermediate expression result (e.g.
// one operand chain's worth of an operator evaluation — see
// genBinaryExpr/genUnaryExpr). The `__` prefix is reserved wholesale for
// the compiler's own names (see sema.go's reservedName), so these can
// never collide with a user-assigned Weave variable.
func (fg *funcGen) newTemp() string {
	name := fmt.Sprintf("__t%d", fg.tempCount)
	fg.tempCount++
	fg.declare(name, "^any")
	return name
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
	val, err := genExpr(fg, s.Value)
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
	val, err := genExpr(fg, call.Args[0])
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
	val, err := genExpr(fg, s.Value)
	if err != nil {
		return err
	}
	fg.declare(exitCodeTemp, "^int")
	fmt.Fprintf(&fg.body, "\tCALL\t%%%s\t:\t?weavert.ExitCode\t%s\n", exitCodeTemp, val)
	fmt.Fprintf(&fg.body, "\tRET\t%%%s\n", exitCodeTemp)
	return nil
}

// genExpr lowers expr and returns the AMIVM `value` token that holds its
// result: either a literal token, a `%name` variable reference, or (for
// a compound expression) a freshly declared `%__tN` temp that a CALL
// just wrote into. Every Weave value is represented as Go `any`
// (weave_spec.md §2 — see CLAUDE.md's Weave特有の設計課題 1, resolved in
// Step 2), so literals are the only place that turns an AST node
// directly into IR text; everything else routes through weavert (see
// genBinaryExpr/genUnaryExpr's doc comments for why).
func genExpr(fg *funcGen, expr ast.Expr) (string, error) {
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
	case *ast.BinaryExpr:
		return genBinaryExpr(fg, e)
	case *ast.UnaryExpr:
		return genUnaryExpr(fg, e)
	default:
		return "", fmt.Errorf("line %d: expression not yet implemented", exprLine(expr))
	}
}

// binOpFuncs maps weave_spec.md §8's binary operators to their
// weavert.* implementation. Every operator routes through a runtime
// function rather than a native AMIVM instruction (ADD/EQ/...) because
// operands are `any`-typed and Go does not allow e.g. `any + any`
// directly — see weavert/ops.go's package doc comment. Using weavert
// uniformly (even for `==`/`!=`, where Go's own `==` on two `any`
// values would in fact compile directly) is a deliberate simplicity
// choice over chasing a native-instruction fast path this early — see
// CLAUDE.md's Step 3 "確定した設計判断".
var binOpFuncs = map[string]string{
	"+": "Add", "-": "Sub", "*": "Mul", "/": "Div", "%": "Mod",
	"==": "Eq", "!=": "Neq",
	"<": "Lt", "<=": "Lte", ">": "Gt", ">=": "Gte",
	"&&": "And", "||": "Or",
}

// genBinaryExpr lowers a two-operand operator expression. Both operands
// are evaluated eagerly (as plain CALL arguments), which is correct for
// every arithmetic/comparison operator — but for `&&`/`||` it means
// Weave does not yet short-circuit (see weavert.And/Or's doc comment;
// CLAUDE.md's Step 3 "確定した設計判断" tracks fixing this in Step 4).
func genBinaryExpr(fg *funcGen, e *ast.BinaryExpr) (string, error) {
	fn, ok := binOpFuncs[e.Op]
	if !ok {
		return "", fmt.Errorf("line %d: binary operator %q not yet implemented", e.Line, e.Op)
	}
	x, err := genExpr(fg, e.X)
	if err != nil {
		return "", err
	}
	y, err := genExpr(fg, e.Y)
	if err != nil {
		return "", err
	}
	tmp := fg.newTemp()
	fmt.Fprintf(&fg.body, "\tCALL\t%%%s\t:\t?weavert.%s\t%s\t%s\n", tmp, fn, x, y)
	return "%" + tmp, nil
}

// genUnaryExpr lowers a prefix unary operator expression. Unary `-` has
// no dedicated AMIVM instruction (nor a weavert.Neg — see
// seed_implementation_notes.md §3's "-x is SUB tmp 0 x" trick), so it
// reuses weavert.Sub(0.0, x) rather than adding a redundant function.
func genUnaryExpr(fg *funcGen, e *ast.UnaryExpr) (string, error) {
	x, err := genExpr(fg, e.X)
	if err != nil {
		return "", err
	}
	tmp := fg.newTemp()
	switch e.Op {
	case "-":
		fmt.Fprintf(&fg.body, "\tCALL\t%%%s\t:\t?weavert.Sub\t0.0\t%s\n", tmp, x)
	case "!":
		fmt.Fprintf(&fg.body, "\tCALL\t%%%s\t:\t?weavert.Not\t%s\n", tmp, x)
	default:
		return "", fmt.Errorf("line %d: unary operator %q not yet implemented", e.Line, e.Op)
	}
	return "%" + tmp, nil
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
	case *ast.BinaryExpr:
		return x.Line
	case *ast.UnaryExpr:
		return x.Line
	default:
		return 0
	}
}
