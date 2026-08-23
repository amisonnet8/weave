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
	"print":        true,
	"has":          true,
	"remove":       true,
	"list":         true,
	"len":          true,
	"string":       true,
	"spawn":        true,
	"send":         true,
	"ask":          true,
	"reply":        true,
	"at":           true,
	"raiseIfError": true,
}

// codegenCtx is shared by every funcGen created during one Generate()
// call — weave_main's own funcGen and every nested closure's (see
// genFuncLit) — so nested CLOS instances get globally unique name
// prefixes (funcGen.namePrefix) regardless of how deeply they nest.
type codegenCtx struct {
	closureCount int // total CLOS instances compiled so far; also each one's own unique id

	// goTypes/goFuncs are populated as `gotype(...)`/`gofunc(...)`
	// declarations are compiled (goasset.go) and consulted by later
	// references — see genGoDecl's doc comment.
	goTypes map[string]*GoTypeInfo
	goFuncs map[string]*GoFuncInfo
	goVars  map[string]*GoVarInfo

	// shapes are populated as `shape(...)` declarations are compiled
	// (shape.go) and consulted by later checkShape(...) calls.
	shapes map[string]*ShapeInfo

	// goFnTypeCount mints unique names for the FNTYPE declarations a
	// typed gomethod(...) call needs (goasset.go's genNativeGoMethodCall)
	// — one per call site, never deduplicated by signature (simpler, and
	// the cost of a handful of extra top-level type decls is negligible).
	goFnTypeCount int

	// goBytesTypeEmitted tracks whether the one shared `SLTYPE ^GoBytes
	// ^byte` declaration (goTypeToken's "?[]byte" special case,
	// weave_spec.md §15.4) has already been added to topLevelDecls —
	// unlike goFnTypeCount above, this IS deduplicated: every "?[]byte"
	// hint anywhere in the program means exactly the same Go type, so
	// there's no reason to declare it more than once (contrast
	// newGoFnType, where each call site's signature can genuinely
	// differ).
	goBytesTypeEmitted bool

	// topLevelDecls collects IR lines that must sit outside any FUNC —
	// the FNTYPE lines above, plus the SLTYPE below. Emitted by
	// Generate() before FUNC !weave_main, since funcGen.body/decls are
	// both scoped inside one FUNC/CLOS and have nowhere to put a
	// package-level declaration.
	topLevelDecls []string
}

// goBytesType returns the `^GoBytes` type token backing every "?[]byte"
// type hint (goTypeToken's own doc comment has the full reasoning for
// why a named SLTYPE is needed at all), emitting its one shared SLTYPE
// declaration into topLevelDecls the first time it's needed.
func (ctx *codegenCtx) goBytesType() string {
	if !ctx.goBytesTypeEmitted {
		ctx.topLevelDecls = append(ctx.topLevelDecls, "SLTYPE\t^GoBytes\t^byte\n")
		ctx.goBytesTypeEmitted = true
	}
	return "^GoBytes"
}

// newGoFnType mints and records a fresh top-level FNTYPE declaration
// (paramTypes -> returnTypes, all already-converted `^`-prefixed AMIVM
// type tokens — returnTypes has exactly one entry per the method's
// declared goReturns(...) position, 0 or more, uniformly; weave_spec.md
// §15.2's "every Go call always returns a list" rule) and returns its
// deftype name, ready to use in an FGET.
func (ctx *codegenCtx) newGoFnType(paramTypes, returnTypes []string) string {
	name := fmt.Sprintf("^GoFn%d", ctx.goFnTypeCount)
	ctx.goFnTypeCount++
	ctx.topLevelDecls = append(ctx.topLevelDecls,
		fmt.Sprintf("FNTYPE\t%s%s\t:%s\n", name, argSuffix(paramTypes), argSuffix(returnTypes)))
	return name
}

// funcGen accumulates one AMIVM FUNC or CLOS body's VAR declarations
// (hoisted to the top, in first-declared order) and its
// SET/CALL/IF/LOOP/RET instructions (left in source order, nested
// IF/ELSE/ENDIF and LOOP/BREAK/CONTINUE/ENDLOOP blocks included — see
// genIfStmt/genWhileStmt/genForStmt). weave_main's own funcGen (parent
// == nil) maps to the top-level FUNC;
// every closure literal (weave_spec.md §5) gets its own funcGen (see
// genFuncLit) mapping to a CLOS nested inline at the point the literal
// appears, parented at whichever funcGen was active there — arbitrarily
// deep, since AMIVM's CLOS can now nest inside another CLOS's body.
//
// Weave allows block-scoped shadowing at the language level
// (weave_spec.md §10), but on the Go side, variables declared by
// ordinary (non-CLOS-crossing) if/while/for bodies stay in one flat
// namespace within their own funcGen — sema.go's scope already resolves
// which Weave name maps to which binding at that granularity, so a flat
// `declared` set per funcGen is still correct there (see CLAUDE.md's
// Step 4 "確定した設計判断"). Crossing a CLOS boundary is different: each
// nested CLOS is a real, independently-scoped Go func literal, so a name
// declared in one must never collide with the *same* AMIVM naming
// (`関数名_amivm_function_xxx`, which does not vary by CLOS nesting depth
// — only `&N`'s own Go name does, via `&L-N`) chosen by another nested
// CLOS instance. namePrefix (unique per funcGen instance, empty only for
// weave_main's own top-level funcGen) is how each instance's own %-names
// stay distinct; resolve walks the parent chain to find which instance
// (and therefore which prefix) a name was actually bound in, letting a
// nested closure reference an enclosing one's variable — or reassign it,
// per §10's "参照で捕捉する" — by the same %-token the enclosing scope
// itself uses. See genFuncLit's doc comment for the closure-compilation
// side of this.
//
// Every VAR — no matter how deeply nested the if/while/for that
// introduces it — is still hoisted to the very top of its funcGen,
// ahead of any IF/LOOP block. This is no longer required to dodge Go's
// "goto cannot jump over a variable declaration" rule (amivm's IF/LOOP
// are real nested Go blocks now, not goto-based — see genIfStmt/
// genWhileStmt's own doc comments, and ignored/weave_implementation_notes.md's
// note on the AMIVM block-form migration) — but it is still required
// for a different reason: the flat, single `declared` set this funcGen
// doc comment describes above (needed for Weave's own "reassign
// whichever enclosing binding is visible" scoping, §10) only stays
// correct if every VAR it tracks is *actually* reachable as a plain Go
// variable from anywhere else in the same funcGen. A VAR declared
// in-place inside a nested IF/LOOP body would be confined to that
// block's own Go scope, making it invisible (a go/types "undefined")
// from a later sibling block that Weave's own scoping rules say should
// still see it. Hoisting to the function top keeps every VAR in one
// scope broad enough for the flat reuse scheme to keep working; crossing
// into a nested CLOS starts a fresh hoisting region, since that's a
// separate Go func literal with its own scope.
type funcGen struct {
	ctx        *codegenCtx
	parent     *funcGen // nil only for weave_main's own top-level funcGen
	namePrefix string   // unique per funcGen instance; "" for weave_main itself
	isMain     bool     // true only for weave_main's own funcGen — see genReturnStmt
	decls      []string
	declared   map[string]bool // names (bare, unprefixed) declared directly in THIS funcGen
	body       strings.Builder
	tempCount  int

	// goStaticVars/goListShapes mirror sema's own tracking (internal/sema/
	// goasset.go's trackGoAssetResult) — kept per-funcGen, not on
	// codegenCtx, since they track ordinary variable identities that
	// don't cross function boundaries (unlike goTypes/goFuncs, true
	// compile-time constants). A closure does not inherit an enclosing
	// scope's static Go-typing knowledge (a narrow, pre-existing
	// limitation, unchanged here).
	goStaticVars map[string]string
	goListShapes map[string][]string
}

func newFuncGen(ctx *codegenCtx) *funcGen {
	return &funcGen{ctx: ctx, declared: map[string]bool{}}
}

// declare emits a hoisted `VAR %token irType` line the first time name is
// seen in THIS funcGen specifically (an AMIVM VAR may only be declared
// once per FUNC/CLOS body), and returns its full, namePrefix-qualified
// token. Used for bindings that are always fresh at this exact level —
// a closure's own parameter, a for-in loop's key/value — never for an
// ordinary Weave assignment, which must check resolve first (see
// genAssignStmt) so that reassigning a name visible from an enclosing
// scope updates that binding instead of shadowing it, per weave_spec.md
// §10's "代入は既存を再代入優先" (CLAUDE.md's Step 4 "確定した設計判断").
func (fg *funcGen) declare(name, irType string) string {
	tok := fg.namePrefix + name
	if fg.declared[name] {
		return tok
	}
	fg.declared[name] = true
	fg.decls = append(fg.decls, fmt.Sprintf("\tVAR\t%%%s\t%s\n", tok, irType))
	return tok
}

// resolve looks up name starting at fg and walking outward through
// parent funcGens (innermost first), returning the full token of
// whichever funcGen instance actually declared it. This is what lets a
// nested closure read — or, via genAssignStmt, reassign — an enclosing
// scope's variable using Go's own lexical capture (native CLOS nesting,
// see genFuncLit), rather than a local shadow copy.
func (fg *funcGen) resolve(name string) (string, bool) {
	for f := fg; f != nil; f = f.parent {
		if f.declared[name] {
			return f.namePrefix + name, true
		}
	}
	return "", false
}

// newTemp declares and returns the `%token` of a fresh compiler temp of
// the given AMIVM type, for holding an intermediate expression or
// condition result (see genBinaryExpr/genUnaryExpr/genCond). The `__`
// prefix is reserved wholesale for the compiler's own names (see
// sema.go's reservedName), so the bare name can never collide with a
// user-assigned Weave variable — namePrefix (see declare) additionally
// keeps it from colliding with the *same* bare temp name generated by a
// different funcGen instance (a sibling or ancestor/descendant closure).
func (fg *funcGen) newTemp(irType string) string {
	name := fmt.Sprintf("__t%d", fg.tempCount)
	fg.tempCount++
	return "%" + fg.declare(name, irType)
}

// Generate lowers a checked *ast.File into AMIVM-IR text.
//
// main's own parameter (weave_spec.md §12's command-line-arguments list,
// ast.FuncDecl.Param) is bound as weave_main's own ordinary FUNC-level
// argument (`$1`) — weave_main stays one flat top-level FUNC exactly as
// before (main = fn(args) {...} changed only the *surface syntax* for
// declaring the entry point, not this underlying mechanism; see
// ast.FuncDecl's own doc comment) — copied into an ordinary %-declared
// local right at the start of the body, mirroring genFuncLit's identical
// `&1`-to-%-local copy for an ordinary closure's own parameter (see its
// doc comment) — just using `$1` (a plain FUNC argument) instead of `&1`
// (a CLOS argument), since weave_main was never itself a CLOS.
func Generate(file *ast.File) (string, error) {
	ctx := &codegenCtx{}
	fg := newFuncGen(ctx)
	fg.isMain = true
	argsTok := fg.declare(file.Main.Param, "^any")
	fmt.Fprintf(&fg.body, "\tSET\t%%%s\t$1\n", argsTok)
	if err := genBlock(fg, file.TopLevel); err != nil {
		return "", err
	}
	if err := genBlock(fg, file.Main.Body); err != nil {
		return "", err
	}

	var b strings.Builder
	// FNTYPE declarations (typed gomethod(...) calls, goasset.go's
	// newGoFnType) and the shared SLTYPE for "?[]byte" hints
	// (codegenCtx.goBytesType) must sit outside any FUNC — emit them
	// first.
	for _, d := range ctx.topLevelDecls {
		b.WriteString(d)
	}
	// Every closure literal compiled while generating weave_main is now
	// an inline, nested CLOS block already sitting inside fg.body (see
	// genFuncLit) — nothing to emit separately here.
	fmt.Fprintf(&b, "FUNC\t!%s\t^any\t:\t^int\n", weaveMainFunc)
	for _, d := range fg.decls {
		b.WriteString(d)
	}
	b.WriteString(fg.body.String())
	b.WriteString("ENDFUNC\n")

	b.WriteString("FUNC\t!main\t:\n")
	b.WriteString("\tVAR\t%exitcode\t^int\n")
	b.WriteString("\tVAR\t%args\t^any\n")
	b.WriteString("\tCALL\t%args\t:\t?weavert.Args\n")
	fmt.Fprintf(&b, "\tCALL\t%%exitcode\t:\t!%s\t%%args\n", weaveMainFunc)
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
	case *ast.ForStmt:
		return genForStmt(fg, s)
	case *ast.BreakStmt:
		// Native BREAK/CONTINUE (amivm's LOOP/ENDLOOP block form) always
		// target the innermost enclosing LOOP, exactly matching
		// weave_spec.md §7's own semantics — no label bookkeeping needed
		// at all (see genWhileStmt/genForStmt's doc comments).
		fg.body.WriteString("\tBREAK\n")
		return nil
	case *ast.ContinueStmt:
		fg.body.WriteString("\tCONTINUE\n")
		return nil
	case *ast.PropAssignStmt:
		return genPropAssignStmt(fg, s)
	default:
		return fmt.Errorf("codegen: unsupported statement %T", stmt)
	}
}

// genIfStmt lowers `if cond {...} elif cond2 {...} else {...}`
// (weave_spec.md §7) to amivm's block-form IF/ELSE/ENDIF (never the
// ELIF token): each further clause becomes a nested IF inside the
// previous one's ELSE, exactly mirroring how amivm's own parser
// desugars ELIF internally. This is deliberate, not just a style
// choice — ELIF's own condition operand must already be a plain value
// by the time that line is reached, with no room to emit the CALL
// instructions Weave's genCond always needs (even `if true {}` routes
// through weavert.CheckBool). Nesting instead means each clause's
// condition is only computed once every earlier one's ELSE branch is
// actually entered, preserving elif's normal lazy/short-circuit
// evaluation (ignored/seed_implementation_notes.md §2.1).
func genIfStmt(fg *funcGen, s *ast.IfStmt) error {
	return genIfClauses(fg, s.Clauses, s.Else)
}

func genIfClauses(fg *funcGen, clauses []ast.IfClause, elseBody []ast.Stmt) error {
	if len(clauses) == 0 {
		return genBlock(fg, elseBody)
	}
	cond, err := genCond(fg, clauses[0].Cond)
	if err != nil {
		return err
	}
	fmt.Fprintf(&fg.body, "\tIF\t%s\n", cond)
	if err := genBlock(fg, clauses[0].Body); err != nil {
		return err
	}
	rest := clauses[1:]
	if len(rest) > 0 || elseBody != nil {
		fg.body.WriteString("\tELSE\n")
		if err := genIfClauses(fg, rest, elseBody); err != nil {
			return err
		}
	}
	fg.body.WriteString("\tENDIF\n")
	return nil
}

// genWhileStmt lowers `while cond {...}` (weave_spec.md §7) to amivm's
// block-form LOOP: the condition is (re)checked at the very top of each
// iteration, so a native CONTINUE landing back at LOOP's start re-enters
// exactly there — no GOTO/LABEL needed for either break or continue
// (contrast genForStmt, which needs its own per-iteration bookkeeping
// done *before* the check for the same reason to stay label-free too).
func genWhileStmt(fg *funcGen, s *ast.WhileStmt) error {
	fg.body.WriteString("\tLOOP\n")
	cond, err := genCond(fg, s.Cond)
	if err != nil {
		return err
	}
	genBreakUnless(fg, cond)
	if err := genBlock(fg, s.Body); err != nil {
		return err
	}
	fg.body.WriteString("\tENDLOOP\n")
	return nil
}

// genBreakUnless emits `IF !cond { BREAK } ENDIF` — the `while cond
// {...}` -> `LOOP { <check> ... } ENDLOOP` translation amivm's own docs
// show for expressing a conditional loop with LOOP's unconditional
// `for {}` (amivm_spec.md §4.11). cond must already be a Go-native
// ^bool (genCond's job) — NOT is the plain native negation, no weavert
// call needed since the value is already boolean-typed.
func genBreakUnless(fg *funcGen, cond string) {
	notCond := fg.newTemp("^bool")
	fmt.Fprintf(&fg.body, "\tNOT\t%s\t%s\n", notCond, cond)
	fmt.Fprintf(&fg.body, "\tIF\t%s\n", notCond)
	fg.body.WriteString("\tBREAK\n")
	fg.body.WriteString("\tENDIF\n")
}

// genCond lowers a condition expression and asserts it down to a
// Go-native ^bool temp via weavert.CheckBool. amivm's IF/ELIF (and
// LOOP's own break-unless check) require their operand to already be
// concretely bool-typed — an `any` holding a bool does not type-check
// there, so every branch condition (if/elif/while/for-in, and the
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

// genAssignStmt lowers `name = value` to a hoisted `VAR %token ^any`
// (first occurrence only) plus a `SET` in source order. Every Weave
// variable is Go `any` (see genValue's doc comment) — and Go's own zero
// value for `any` is nil, which already matches weave_spec.md §2's "a
// variable is nil until assigned" without needing Seed/Cascade's
// separate `_isset` flag.
//
// Reassignment reuses whichever funcGen already declared name — checked
// via resolve, walking out through enclosing closures if this assignment
// is inside one — rather than always declaring fresh in fg. This is what
// makes weave_spec.md §10's "代入は既存を再代入優先" hold even across a
// CLOS boundary: `n = 1; f = fn(x) { n = n + 1; return n }` must mutate
// the *same* `n` main sees, not shadow it with a closure-local copy (see
// funcGen's doc comment). Only when name isn't visible anywhere in the
// enclosing chain does it become a genuinely new binding, declared in fg
// itself.
//
// Self-recursion mirrors sema.go's identical carve-out: when s.Value is
// directly a *ast.FuncLit, s.Name is declared in fg *before* compiling
// the literal's body (genFuncLit), instead of after alongside the SET —
// so a reference to s.Name from inside the closure (calling itself)
// resolves through fg.resolve to this VAR, already hoisted, rather than
// falling through genExpr's undefined-name fallback. This costs nothing
// for an ordinary non-recursive closure (declare is idempotent; the
// eventual SET still lands at the same source position, only the VAR's
// entry in fg.decls moves earlier — decls are all hoisted ahead of body
// text regardless of order), so no separate check for whether the body
// actually refers to itself is needed.
func genAssignStmt(fg *funcGen, s *ast.AssignStmt) error {
	if call, ok := s.Value.(*ast.CallExpr); ok {
		if handled, err := genGoDecl(fg, s.Name, call); handled {
			return err
		}
		if handled, err := genShapeDecl(fg, s.Name, call); handled {
			return err
		}
	}
	tok, bound := fg.resolve(s.Name)
	if !bound {
		if _, isFuncLit := s.Value.(*ast.FuncLit); isFuncLit {
			tok = fg.declare(s.Name, "^any")
			bound = true
		}
	}
	val, err := genExpr(fg, s.Value)
	if err != nil {
		return err
	}
	trackGoAssetResult(fg, s.Name, s.Value)
	if !bound {
		tok = fg.declare(s.Name, "^any")
	}
	fmt.Fprintf(&fg.body, "\tSET\t%%%s\t%s\n", tok, val)
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
	if callee, ok := call.Callee.(*ast.Ident); ok && callee.Name == "checkShape" {
		return genCheckShapeCall(fg, call)
	}
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
		return genTwoArgBuiltin(fg, call, "weavert.ObjHas")
	case "remove":
		return genTwoArgBuiltin(fg, call, "weavert.ObjRemove")
	case "list":
		return genListCall(fg, call)
	case "len":
		return genOneArgBuiltin(fg, call, "weavert.Len")
	case "string":
		return genOneArgBuiltin(fg, call, "weavert.ToString")
	case "spawn":
		return genOneArgBuiltin(fg, call, "weavert.Spawn")
	case "send":
		return genOneArgBuiltin(fg, call, "weavert.Send")
	case "ask":
		return genOneArgBuiltin(fg, call, "weavert.Ask")
	case "reply":
		return genTwoArgBuiltin(fg, call, "weavert.Reply")
	case "at":
		return genTwoArgBuiltin(fg, call, "weavert.ObjAt")
	case "raiseIfError":
		return genOneArgBuiltin(fg, call, "weavert.RaiseIfError")
	default:
		return "", fmt.Errorf("line %d: builtin %q not yet implemented", call.Line, name)
	}
}

// genOneArgBuiltin lowers a single-argument builtin call (len(...)/
// string(...), weave_spec.md §11) to the given weavert function.
func genOneArgBuiltin(fg *funcGen, call *ast.CallExpr, fn string) (string, error) {
	if len(call.Args) != 1 {
		return "", fmt.Errorf("line %d: %s(...) takes exactly one argument, got %d", call.Line, fn, len(call.Args))
	}
	val, err := genExpr(fg, call.Args[0])
	if err != nil {
		return "", err
	}
	tmp := fg.newTemp("^any")
	fmt.Fprintf(&fg.body, "\tCALL\t%s\t:\t?%s\t%s\n", tmp, fn, val)
	return tmp, nil
}

// genListCall lowers `list(a, b, c)` (weave_spec.md §3, §11) to an
// object with sequential numeric-string keys ("0", "1", ...) — the same
// NewObject+ObjSet shape genObjectLit uses, since a Weave "array" is
// nothing but that sugar (§3: "連番の数値キーを持つ普通のオブジェクト").
func genListCall(fg *funcGen, call *ast.CallExpr) (string, error) {
	obj := fg.newTemp("^any")
	fmt.Fprintf(&fg.body, "\tCALL\t%s\t:\t?weavert.NewObject\n", obj)
	for i, arg := range call.Args {
		val, err := genExpr(fg, arg)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&fg.body, "\tCALL\t:\t?weavert.ObjSet\t%s\t%s\t%s\n", obj, quoteKey(strconv.Itoa(i)), val)
	}
	return obj, nil
}

// genTwoArgBuiltin lowers has(obj, name)/remove(obj, name)
// (weave_spec.md §11) to the given weavert function, which both take
// exactly the same (obj, key) shape.
func genTwoArgBuiltin(fg *funcGen, call *ast.CallExpr, fn string) (string, error) {
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
	// obj.method(a, b) is sugar for obj.method(obj, a, b) (weave_spec.md
	// §9) — but only when the call is written directly against a
	// property access; a callee that's itself a call result (e.g. the
	// outer `(b)` in `obj.method(a)(b)`) is an ordinary curried
	// application with no further self-injection. See genMethodCall.
	if prop, ok := call.Callee.(*ast.PropExpr); ok {
		if handled, result, err := genGoMethodCall(fg, prop, call.Args); handled {
			return result, err
		}
		return genMethodCall(fg, prop, call.Args)
	}
	// A gofunc(...)-declared reference compiles straight to its real Go
	// function, bypassing weavert.Call's dynamic dispatch (weave_spec.md
	// §16) — see genGoFuncCall's doc comment.
	if callee, ok := call.Callee.(*ast.Ident); ok {
		if info := fg.ctx.goFuncs[callee.Name]; info != nil {
			return genGoFuncCall(fg, info, call)
		}
	}
	// Every Weave function takes exactly one argument (weave_spec.md
	// §5), so `f()` desugars to `f(nil)` here — deliberately at this
	// exact point, not in the parser: by now every reserved/builtin name
	// (gotype/gofunc/gomethod/goReturns/goParams/shape/checkShape/
	// package, and every genBuiltinCall-dispatched name like list/print)
	// has already had first claim on this call's own Args shape and
	// returned before reaching here, and the two branches above
	// (method-call sugar, direct gofunc calls) have also already ruled
	// themselves out — so nothing else's own "zero arguments" meaning
	// (goParams()'s "no parameters", list()'s "empty list", ...) can
	// ever be corrupted by this substitution. What's left is genuinely
	// always "an ordinary Weave function *value*, called with no
	// explicit argument" — parser.go's own parseCallArgs doc comment has
	// the full reasoning for why the desugar lives here and not there.
	args := call.Args
	if len(args) == 0 {
		args = []ast.Expr{&ast.NilLit{Line: call.Line}}
	}
	result, err := genExpr(fg, call.Callee)
	if err != nil {
		return "", err
	}
	for _, arg := range args {
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

// genMethodCall lowers `obj.method(args...)` as `obj.method(obj,
// args...)` (weave_spec.md §9): evaluate obj once, look up `method` via
// the prototype chain (weavert.ObjGet — see its own doc comment), then
// curry-apply starting with obj itself as the first argument, exactly
// like genGeneralCall's plain multi-arg case but with obj prepended.
// Zero explicit args is valid here (`alice.greet()`, §9's own example)
// since self alone is still one argument to apply.
func genMethodCall(fg *funcGen, prop *ast.PropExpr, args []ast.Expr) (string, error) {
	objVal, err := genExpr(fg, prop.Obj)
	if err != nil {
		return "", err
	}
	fn := fg.newTemp("^any")
	fmt.Fprintf(&fg.body, "\tCALL\t%s\t:\t?weavert.ObjGet\t%s\t%s\n", fn, objVal, quoteKey(prop.Prop))

	// self is always applied first (weave_spec.md §9, using objVal —
	// already evaluated above, so prop.Obj's side effects don't run
	// twice), then each explicit argument in order: the same
	// curry-chaining genGeneralCall uses, just with obj prepended.
	result := fn
	applyTo := func(argVal string) {
		tmp := fg.newTemp("^any")
		fmt.Fprintf(&fg.body, "\tCALL\t%s\t:\t?weavert.Call\t%s\t%s\n", tmp, result, argVal)
		result = tmp
	}
	applyTo(objVal)
	for _, arg := range args {
		argVal, err := genExpr(fg, arg)
		if err != nil {
			return "", err
		}
		applyTo(argVal)
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

// genClosureReturnStmt lowers `return`/`return <expr>` inside a
// function literal. A bare `return` (like falling off the end of the
// body with no `return` at all — see genFuncLit's trailing RET nil)
// yields nil: weave_spec.md §6.2's own mutator-style closures
// (`increment: fn(self, n) { self.count = self.count + n }`) rely on a
// function being usable purely for a side effect, with nothing
// meaningful to return.
func genClosureReturnStmt(fg *funcGen, s *ast.ReturnStmt) error {
	if s.Value == nil {
		fg.body.WriteString("\tRET\tnil\n")
		return nil
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
		// A govar(...)-declared name (weave_spec.md §15.5) never went
		// through VAR/SET in the first place — see genGoVarDecl/
		// genGoVarRead's own doc comments — so it must be checked before
		// falling through to the ordinary declared-variable path below.
		if info := fg.ctx.goVars[e.Name]; info != nil {
			return genGoVarRead(fg, info), nil
		}
		// resolve walks out through any enclosing closures this
		// reference sits inside (funcGen's doc comment), finding
		// whichever funcGen instance actually declared e.Name and
		// returning its namePrefix-qualified token. sema already
		// guarantees e.Name is visible somewhere in scope for any real
		// compiled program, so falling back to the bare (unprefixed)
		// name when resolve finds nothing is purely a safety net, never
		// expected to matter in practice — codegen trusts sema's own
		// validation rather than re-deriving it (CLAUDE.md's Step 4
		// "確定した設計判断").
		if tok, ok := fg.resolve(e.Name); ok {
			return "%" + tok, nil
		}
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
// the right operand's code (and any side effects it has) only runs
// along the branch where it actually matters — using amivm's block-form
// IF/ENDIF (there is no native AND/OR-with-short-circuit instruction:
// amivm's own AND/OR always evaluate both operands first, see
// amivm_spec.md §4.6). This replaces the eager weavert.And/Or call
// Step 3 used (see CLAUDE.md's Step 3 "確定した設計判断", which flagged
// this as a known gap to fix once branching existed) — And/Or were
// removed from weavert/ops.go since nothing calls them anymore.
//
// The result temp doubles as the left operand's already-checked ^bool:
// genCond leaves `x`'s truth value sitting in a fresh temp, and
// whichever branch below is taken either leaves it alone (the
// short-circuited case, IF's condition false) or overwrites it with the
// right operand's checked value (IF's condition true, body runs).
func genShortCircuit(fg *funcGen, e *ast.BinaryExpr) (string, error) {
	result, err := genCond(fg, e.X)
	if err != nil {
		return "", err
	}

	// `&&`: only evaluate y (and possibly flip result to false) when x
	// was true. `||`: only evaluate y (and possibly flip result to true)
	// when x was false — so guard on NOT result instead.
	guard := result
	if e.Op == "||" {
		guard = fg.newTemp("^bool")
		fmt.Fprintf(&fg.body, "\tNOT\t%s\t%s\n", guard, result)
	}
	fmt.Fprintf(&fg.body, "\tIF\t%s\n", guard)
	y, err := genExpr(fg, e.Y)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(&fg.body, "\tCALL\t%s\t:\t?weavert.CheckBool\t%s\n", result, y)
	fg.body.WriteString("\tENDIF\n")
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
