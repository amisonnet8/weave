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

// builtinNames are call targets resolved as ordinary Go-native calls
// (via a dedicated genXxxCall function) rather than Weave function
// values applied through weavert.Call — kept in sync manually with
// sema.go's own copy (CLAUDE.md's "確定した設計判断").
var builtinNames = map[string]bool{
	"print":  true,
	"has":    true,
	"remove": true,
}

// codegenCtx is shared by every funcGen created during one Generate()
// call — main's own funcGen and every nested closure's (see
// genFuncLit) — so closure FUNCs get globally unique names and all end
// up collected in one place to emit alongside weave_main/main.
type codegenCtx struct {
	closureCount int
	closureFuncs []string // accumulated `FUNC ... ENDFUNC` blocks, compilation order
}

// funcGen accumulates one AMIVM FUNC body's VAR declarations (hoisted to
// the top, in first-declared order) and its SET/CALL/GOTO/LABEL/IF/RET
// instructions (left in source order).
//
// Weave allows block-scoped shadowing at the language level
// (weave_spec.md §10), but on the Go side generated variables stay in
// one completely flat namespace regardless of nesting — sema.go's scope
// already resolves which Weave name maps to which binding and rejects
// anything codegen shouldn't be asked to generate (see its "確定した
// 設計判断" for why a flat `declared` set here is still correct: sema
// guarantees every Ident it lets through already refers to a valid,
// visible binding, so codegen never needs its own notion of scope).
// This also means every VAR — no matter how deeply nested the if/while
// that introduces it — ends up hoisted ahead of any GOTO/LABEL in the
// same function, which is required by Go's "goto cannot jump over a
// variable declaration" rule (seed_implementation_notes.md §1).
type funcGen struct {
	ctx           *codegenCtx
	isMain        bool // true only for weave_main's own funcGen — see genReturnStmt
	decls         []string
	declared      map[string]bool
	body          strings.Builder
	tempCount     int
	labelCount    int
	breakStack    []string // target label for `break`, innermost last
	continueStack []string // target label for `continue`, innermost last
}

func newFuncGen(ctx *codegenCtx) *funcGen {
	return &funcGen{ctx: ctx, declared: map[string]bool{}}
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

// newTemp declares and returns the `%__tN` token of a fresh compiler
// temp of the given AMIVM type, for holding an intermediate expression
// or condition result (see genBinaryExpr/genUnaryExpr/genCond). The
// `__` prefix is reserved wholesale for the compiler's own names (see
// sema.go's reservedName), so these can never collide with a
// user-assigned Weave variable.
func (fg *funcGen) newTemp(irType string) string {
	name := fmt.Sprintf("__t%d", fg.tempCount)
	fg.tempCount++
	fg.declare(name, irType)
	return "%" + name
}

// newLabel returns a fresh, unique `#name` label token prefixed with
// prefix (e.g. "if_then3"). Labels need no VAR-style hoisting or
// reservation: they're never user-facing (weave_spec.md has no goto),
// so they can't collide with anything sema tracks.
func (fg *funcGen) newLabel(prefix string) string {
	name := fmt.Sprintf("%s%d", prefix, fg.labelCount)
	fg.labelCount++
	return name
}

// Generate lowers a checked *ast.File into AMIVM-IR text.
func Generate(file *ast.File) (string, error) {
	ctx := &codegenCtx{}
	fg := newFuncGen(ctx)
	fg.isMain = true
	if err := genBlock(fg, file.Main.Body); err != nil {
		return "", err
	}

	var b strings.Builder
	// envType (closure.go) is declared once, program-wide, only if any
	// closure literal actually needed it.
	if ctx.closureCount > 0 {
		fmt.Fprintf(&b, "SLTYPE\t^%s\t^any\n", envType)
	}
	// Every closure literal compiled while generating weave_main (or,
	// transitively, another closure) added its own top-level FUNC here
	// — see genFuncLit's doc comment on why it can't be nested inline.
	for _, cf := range ctx.closureFuncs {
		b.WriteString(cf)
	}
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

// genBlock lowers a statement list in source order. It does not open a
// new codegen scope — see funcGen's doc comment for why a flat variable
// namespace is safe even though Weave's own blocks are lexically scoped.
func genBlock(fg *funcGen, stmts []ast.Stmt) error {
	for _, stmt := range stmts {
		if err := genStmt(fg, stmt); err != nil {
			return err
		}
	}
	return nil
}

func genStmt(fg *funcGen, stmt ast.Stmt) error {
	switch s := stmt.(type) {
	case *ast.AssignStmt:
		return genAssignStmt(fg, s)
	case *ast.ExprStmt:
		return genExprStmt(fg, s)
	case *ast.ReturnStmt:
		return genReturnStmt(fg, s)
	case *ast.IfStmt:
		return genIfStmt(fg, s)
	case *ast.WhileStmt:
		return genWhileStmt(fg, s)
	case *ast.BreakStmt:
		fmt.Fprintf(&fg.body, "\tGOTO\t#%s\n", fg.breakStack[len(fg.breakStack)-1])
		return nil
	case *ast.ContinueStmt:
		fmt.Fprintf(&fg.body, "\tGOTO\t#%s\n", fg.continueStack[len(fg.continueStack)-1])
		return nil
	case *ast.PropAssignStmt:
		return genPropAssignStmt(fg, s)
	default:
		return fmt.Errorf("codegen: unsupported statement %T", stmt)
	}
}

// genIfStmt lowers `if cond {...} elif cond2 {...} else {...}`
// (weave_spec.md §7). Each clause becomes: evaluate its condition,
// jump into its body if true, otherwise fall through to the next
// clause's check; every body ends by jumping to the shared end label.
// Mirrors Seed/Cascade's genIfStmt pattern (cascade_implementation_notes.md
// "パーサの実装方式" / Step 5 note).
func genIfStmt(fg *funcGen, s *ast.IfStmt) error {
	endLabel := fg.newLabel("if_end")
	for _, clause := range s.Clauses {
		cond, err := genCond(fg, clause.Cond)
		if err != nil {
			return err
		}
		thenLabel := fg.newLabel("if_then")
		nextLabel := fg.newLabel("if_next")
		fmt.Fprintf(&fg.body, "\tIF\t%s\t#%s\n", cond, thenLabel)
		fmt.Fprintf(&fg.body, "\tGOTO\t#%s\n", nextLabel)
		fmt.Fprintf(&fg.body, "\tLABEL\t#%s\n", thenLabel)
		if err := genBlock(fg, clause.Body); err != nil {
			return err
		}
		fmt.Fprintf(&fg.body, "\tGOTO\t#%s\n", endLabel)
		fmt.Fprintf(&fg.body, "\tLABEL\t#%s\n", nextLabel)
	}
	if s.Else != nil {
		if err := genBlock(fg, s.Else); err != nil {
			return err
		}
	}
	fmt.Fprintf(&fg.body, "\tLABEL\t#%s\n", endLabel)
	return nil
}

// genWhileStmt lowers `while cond {...}` (weave_spec.md §7). break/
// continue targets are pushed for the body and popped afterward, so
// nested loops each resolve to their own innermost labels.
func genWhileStmt(fg *funcGen, s *ast.WhileStmt) error {
	startLabel := fg.newLabel("while_start")
	bodyLabel := fg.newLabel("while_body")
	endLabel := fg.newLabel("while_end")

	fmt.Fprintf(&fg.body, "\tLABEL\t#%s\n", startLabel)
	cond, err := genCond(fg, s.Cond)
	if err != nil {
		return err
	}
	fmt.Fprintf(&fg.body, "\tIF\t%s\t#%s\n", cond, bodyLabel)
	fmt.Fprintf(&fg.body, "\tGOTO\t#%s\n", endLabel)
	fmt.Fprintf(&fg.body, "\tLABEL\t#%s\n", bodyLabel)

	fg.breakStack = append(fg.breakStack, endLabel)
	fg.continueStack = append(fg.continueStack, startLabel)
	err = genBlock(fg, s.Body)
	fg.breakStack = fg.breakStack[:len(fg.breakStack)-1]
	fg.continueStack = fg.continueStack[:len(fg.continueStack)-1]
	if err != nil {
		return err
	}

	fmt.Fprintf(&fg.body, "\tGOTO\t#%s\n", startLabel)
	fmt.Fprintf(&fg.body, "\tLABEL\t#%s\n", endLabel)
	return nil
}

// genCond lowers a condition expression and asserts it down to a
// Go-native ^bool temp via weavert.CheckBool. AMIVM's IF instruction
// compiles to a bare Go `if cond { goto label }`, which requires cond to
// be concretely bool-typed — an `any` holding a bool does not
// type-check there, so every branch condition (if/elif/while, and the
// short-circuit && / || in genShortCircuit) must go through this.
func genCond(fg *funcGen, expr ast.Expr) (string, error) {
	val, err := genExpr(fg, expr)
	if err != nil {
		return "", err
	}
	tmp := fg.newTemp("^bool")
	fmt.Fprintf(&fg.body, "\tCALL\t%s\t:\t?weavert.CheckBool\t%s\n", tmp, val)
	return tmp, nil
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
		return fmt.Errorf("line %d: only call expressions are supported as statements", exprLine(s.X))
	}
	_, err := genCallExpr(fg, call)
	return err
}

// genCallExpr lowers a call expression to the AMIVM value token holding
// its result, dispatching between a fixed-arity Go-native builtin
// (genBuiltinCall) and a general call to a Weave function value
// (genGeneralCall) — see builtinNames.
func genCallExpr(fg *funcGen, call *ast.CallExpr) (string, error) {
	if callee, ok := call.Callee.(*ast.Ident); ok && builtinNames[callee.Name] {
		return genBuiltinCall(fg, callee.Name, call)
	}
	return genGeneralCall(fg, call)
}

func genBuiltinCall(fg *funcGen, name string, call *ast.CallExpr) (string, error) {
	switch name {
	case "print":
		return genPrintCall(fg, call)
	case "has":
		return genTwoArgObjBuiltin(fg, call, "weavert.ObjHas")
	case "remove":
		return genTwoArgObjBuiltin(fg, call, "weavert.ObjRemove")
	default:
		return "", fmt.Errorf("line %d: builtin %q not yet implemented", call.Line, name)
	}
}

// genTwoArgObjBuiltin lowers has(obj, name)/remove(obj, name)
// (weave_spec.md §11) to the given weavert function, which both take
// exactly the same (obj, key) shape.
func genTwoArgObjBuiltin(fg *funcGen, call *ast.CallExpr, fn string) (string, error) {
	if len(call.Args) != 2 {
		return "", fmt.Errorf("line %d: %s(...) takes exactly two arguments, got %d", call.Line, fn, len(call.Args))
	}
	obj, err := genExpr(fg, call.Args[0])
	if err != nil {
		return "", err
	}
	key, err := genExpr(fg, call.Args[1])
	if err != nil {
		return "", err
	}
	tmp := fg.newTemp("^any")
	fmt.Fprintf(&fg.body, "\tCALL\t%s\t:\t?%s\t%s\t%s\n", tmp, fn, obj, key)
	return tmp, nil
}

// genPrintCall lowers print(x) to a ?weavert.Print call, which owns the
// nil→"nil" rendering Go's own fmt.Println doesn't provide (Go's zero
// value for `any` prints as "<nil>"; weave_spec.md §2's nil should read
// as "nil" — see weavert/weavert.go). print returns nil (weave_spec.md
// §11's signature `(value) -> nil`).
func genPrintCall(fg *funcGen, call *ast.CallExpr) (string, error) {
	if len(call.Args) != 1 {
		return "", fmt.Errorf("line %d: print(...) takes exactly one argument, got %d", call.Line, len(call.Args))
	}
	val, err := genExpr(fg, call.Args[0])
	if err != nil {
		return "", err
	}
	fmt.Fprintf(&fg.body, "\tCALL\t:\t?weavert.Print\t%s\n", val)
	return "nil", nil
}

// genGeneralCall lowers a call to a general (non-builtin) callee —
// always a Weave function value — via weavert.Call. Multiple arguments
// (`f(a, b, c)`) are curry-sugar for `f(a)(b)(c)` (weave_spec.md §5):
// applied one at a time, threading each intermediate result (itself
// possibly another partially-applied closure) into the next
// weavert.Call.
func genGeneralCall(fg *funcGen, call *ast.CallExpr) (string, error) {
	if len(call.Args) == 0 {
		return "", fmt.Errorf("line %d: a call needs at least one argument (weave_spec.md §5: every Weave function takes exactly one)", call.Line)
	}
	result, err := genExpr(fg, call.Callee)
	if err != nil {
		return "", err
	}
	for _, arg := range call.Args {
		argVal, err := genExpr(fg, arg)
		if err != nil {
			return "", err
		}
		tmp := fg.newTemp("^any")
		fmt.Fprintf(&fg.body, "\tCALL\t%s\t:\t?weavert.Call\t%s\t%s\n", tmp, result, argVal)
		result = tmp
	}
	return result, nil
}

// genReturnStmt lowers `return <expr>`, which means two different
// things depending on which funcGen is active: inside weave_main
// (fg.isMain) it's main's own `int` exit code (weave_spec.md §12 — the
// one place Weave's dynamic value model touches a native Go type
// instead of `any`); inside a function literal's own funcGen (see
// genFuncLit) it's an ordinary `^any` closure result. Conflating these
// was a real Step 5 bug caught by running examples/closures.weave
// through amivm→go build→execution: every `return` inside a closure
// body was routing through weavert.ExitCode and panicking on the very
// first non-numeric closure result.
func genReturnStmt(fg *funcGen, s *ast.ReturnStmt) error {
	if fg.isMain {
		return genMainReturnStmt(fg, s)
	}
	return genClosureReturnStmt(fg, s)
}

func genMainReturnStmt(fg *funcGen, s *ast.ReturnStmt) error {
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

func genClosureReturnStmt(fg *funcGen, s *ast.ReturnStmt) error {
	if s.Value == nil {
		return fmt.Errorf("line %d: a function literal must return a value (weave_spec.md §5)", s.Line)
	}
	val, err := genExpr(fg, s.Value)
	if err != nil {
		return err
	}
	fmt.Fprintf(&fg.body, "\tRET\t%s\n", val)
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
	case *ast.CallExpr:
		return genCallExpr(fg, e)
	case *ast.FuncLit:
		return genFuncLit(fg, e)
	case *ast.ObjectLit:
		return genObjectLit(fg, e)
	case *ast.PropExpr:
		return genPropExpr(fg, e)
	default:
		return "", fmt.Errorf("line %d: expression not yet implemented", exprLine(expr))
	}
}

// binOpFuncs maps weave_spec.md §8's binary operators to their
// weavert.* implementation, for every operator except `&&`/`||` (see
// genShortCircuit — those need inline branching, not a plain call).
// Operators route through a runtime function rather than a native
// AMIVM instruction (ADD/EQ/...) because operands are `any`-typed and
// Go does not allow e.g. `any + any` directly — see weavert/ops.go's
// package doc comment. Using weavert uniformly (even for `==`/`!=`,
// where Go's own `==` on two `any` values would in fact compile
// directly) is a deliberate simplicity choice over chasing a
// native-instruction fast path this early — see CLAUDE.md's Step 3
// "確定した設計判断".
var binOpFuncs = map[string]string{
	"+": "Add", "-": "Sub", "*": "Mul", "/": "Div", "%": "Mod",
	"==": "Eq", "!=": "Neq",
	"<": "Lt", "<=": "Lte", ">": "Gt", ">=": "Gte",
}

// genBinaryExpr lowers a two-operand operator expression.
func genBinaryExpr(fg *funcGen, e *ast.BinaryExpr) (string, error) {
	if e.Op == "&&" || e.Op == "||" {
		return genShortCircuit(fg, e)
	}
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
	tmp := fg.newTemp("^any")
	fmt.Fprintf(&fg.body, "\tCALL\t%s\t:\t?weavert.%s\t%s\t%s\n", tmp, fn, x, y)
	return tmp, nil
}

// genShortCircuit lowers `&&`/`||` with real short-circuit evaluation:
// the right operand's code only runs along the branch where it actually
// matters. This replaces the eager weavert.And/Or call Step 3 used (see
// CLAUDE.md's Step 3 "確定した設計判断", which flagged this as a known
// gap to fix once branching existed) — And/Or were removed from
// weavert/ops.go since nothing calls them anymore.
//
// The result temp doubles as the left operand's already-checked ^bool:
// genCond leaves `x`'s truth value sitting in a fresh temp, and
// whichever branch below is taken either leaves it alone (the
// short-circuited case) or overwrites it with the right operand's
// checked value.
func genShortCircuit(fg *funcGen, e *ast.BinaryExpr) (string, error) {
	result, err := genCond(fg, e.X)
	if err != nil {
		return "", err
	}

	endLabel := fg.newLabel("sc_end")
	if e.Op == "&&" {
		// x false -> short-circuit to false (skip evaluating y entirely).
		rhsLabel := fg.newLabel("sc_rhs")
		fmt.Fprintf(&fg.body, "\tIF\t%s\t#%s\n", result, rhsLabel)
		fmt.Fprintf(&fg.body, "\tGOTO\t#%s\n", endLabel)
		fmt.Fprintf(&fg.body, "\tLABEL\t#%s\n", rhsLabel)
	} else {
		// x true -> short-circuit to true (skip evaluating y entirely).
		fmt.Fprintf(&fg.body, "\tIF\t%s\t#%s\n", result, endLabel)
	}

	y, err := genExpr(fg, e.Y)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(&fg.body, "\tCALL\t%s\t:\t?weavert.CheckBool\t%s\n", result, y)
	fmt.Fprintf(&fg.body, "\tLABEL\t#%s\n", endLabel)
	return result, nil
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
	tmp := fg.newTemp("^any")
	switch e.Op {
	case "-":
		fmt.Fprintf(&fg.body, "\tCALL\t%s\t:\t?weavert.Sub\t0.0\t%s\n", tmp, x)
	case "!":
		fmt.Fprintf(&fg.body, "\tCALL\t%s\t:\t?weavert.Not\t%s\n", tmp, x)
	default:
		return "", fmt.Errorf("line %d: unary operator %q not yet implemented", e.Line, e.Op)
	}
	return tmp, nil
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
	case *ast.FuncLit:
		return x.Line
	case *ast.ObjectLit:
		return x.Line
	case *ast.PropExpr:
		return x.Line
	default:
		return 0
	}
}
