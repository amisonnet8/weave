package sema

import (
	"fmt"
	"strings"

	"github.com/amisonnet8/weave/internal/ast"
)

// builtinNames are call targets resolved as ordinary Go-native calls
// rather than Weave function values (weave_spec.md §11; lexer.go's doc
// comment on structural vs. builtin keywords). A CallExpr's Callee is
// only checked as an ordinary expression (i.e. required to resolve to a
// declared variable) when it ISN'T one of these — kept in sync manually
// with codegen.go's own copy (CLAUDE.md's "確定した設計判断" tracks
// this alongside the other manually-synced name lists).
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

// reservedName reports whether name is off-limits for a user assignment
// or function parameter because it would collide with a name codegen
// generates for itself.
//
//   - "weave_main" is the internal bridge name for the compiled `main`
//     (weave_spec.md §12; kept in sync manually with codegen.go's
//     weaveMainFunc — see CLAUDE.md's "確定した設計判断").
//   - Any name starting with "__" is reserved wholesale for codegen's
//     own compiler-generated temporaries (e.g. the `__exitcode` exit
//     code conversion, and the `__t0`, `__t1`, ... operator/condition/
//     closure evaluation temps — see codegen.go's funcGen.newTemp). A
//     prefix rule scales better than enumerating every generated name
//     individually, since the temp count grows with expression
//     complexity and isn't a fixed set.
func reservedName(name string) (string, bool) {
	if name == "weave_main" {
		return "reserved for the compiled entry point (weave_spec.md §12)", true
	}
	if name == "main" {
		return "reserved for the entry point declaration; it may only appear as a top-level `main = fn(args) { ... }` (weave_spec.md §12)", true
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
//
// A function literal's body (Step 5) is just another scope in this same
// chain, parented at wherever the literal appears lexically — which is
// exactly what lets it read (and, since assignment reuses the nearest
// visible binding, mutate its own copy of) the enclosing function's
// variables: weave_spec.md §10's closure capture.
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
//
// loopDepth is reset to 0 while checking a function literal's body
// (checkFuncLit) and restored afterward: a `while` enclosing a `fn(...)`
// lexically does NOT make `break`/`continue` valid inside that literal
// — a function body is its own boundary for loop-control statements,
// same as in most languages with nested functions. Nothing in
// weave_spec.md states this explicitly, but the alternative (a
// closure's `break` reaching through to a loop in some *other*
// function's stack frame entirely, once the closure escapes and is
// called later) doesn't correspond to anything coherent at runtime, so
// it's treated as settled rather than left as an open design question.
// goTypes/goFuncs are the compile-time symbol tables Step 3's Go asset
// declarations populate (see goasset.go) — a completely separate
// namespace from scope's ordinary dynamic-value bindings, since these
// names never hold a runtime value at all.
type checker struct {
	loopDepth int
	goTypes   map[string]*GoTypeInfo
	goFuncs   map[string]*GoFuncInfo
	goVars    map[string]*GoVarInfo // govar(...) declarations (goasset.go)

	// goStaticVars/goListProto track static Go-asset typing —
	// goStaticVars maps a variable directly holding a single Go
	// struct/pointer value to its gotype(...) name; goListProto maps a
	// variable holding a proto-bound gofunc/gomethod call's list result
	// to the gotype name position 0 of that list carries (see
	// goasset.go's trackGoAssetResult/checkGoMethodCall for the full
	// reasoning).
	goStaticVars map[string]string
	goListProto  map[string]string
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
// concern here (see weavert.ExitCode, weavert.CheckBool, weavert.Call)
// — but name/scope rules like these are exactly what sema is still
// responsible for.
func Check(file *ast.File) error {
	if file.Main == nil {
		return fmt.Errorf("missing entry point: expected `main = fn(args) { ... }`")
	}
	if why, ok := reservedName(file.Main.Param); ok {
		return fmt.Errorf("line %d: %q is a reserved name (%s)", file.Main.Line, file.Main.Param, why)
	}

	c := &checker{}
	root := newScope(nil)
	// main's own parameter (weave_spec.md §12's command-line-arguments
	// list) is visible throughout — including TopLevel, since codegen
	// binds it before TopLevel runs too (see codegen.go's Generate) —
	// exactly mirroring checkFuncLit's own param-declared-in-child-scope
	// setup, just declared directly on root since main's body shares
	// root with TopLevel (ast.File's own doc comment).
	root.declared[file.Main.Param] = true
	for _, stmt := range file.TopLevel {
		if err := c.checkStmt(stmt, root); err != nil {
			return err
		}
	}
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
		if call, ok := s.Value.(*ast.CallExpr); ok {
			if handled, err := c.checkGoDecl(s.Name, call, s.Line); handled {
				return err
			}
		}
		if s.Name == "_" {
			return c.checkBlankAssign(s, sc)
		}
		if why, ok := reservedName(s.Name); ok {
			return fmt.Errorf("line %d: %q is a reserved name (%s)", s.Line, s.Name, why)
		}
		if _, ok := s.Value.(*ast.FuncLit); ok {
			// Self-recursion: a function literal assigned directly to a
			// name (`fact = fn(n) { ... fact(n-1) ... }`) may refer to
			// that same name inside its own body — declare the binding
			// before checking the literal (checkFuncLit's own scope is a
			// child of sc, so it inherits this) rather than after, the
			// order every other assignment uses. This only ever matters
			// when the closure genuinely captures its own name (an
			// ordinary non-recursive `f = fn(x) {...}` behaves
			// identically either way — declareIfNew is a no-op the
			// second time). codegen.go's genAssignStmt mirrors this
			// exact carve-out so the two stay consistent.
			sc.declareIfNew(s.Name)
		}
		if err := c.checkExpr(s.Value, sc); err != nil {
			return err
		}
		c.trackGoAssetResult(s.Name, s.Value)
		sc.declareIfNew(s.Name)
		return nil
	case *ast.ExprStmt:
		return c.checkExpr(s.X, sc)
	case *ast.ReturnStmt:
		if s.Value == nil {
			return nil
		}
		return c.checkExpr(s.Value, sc)
	case *ast.IfStmt:
		for _, clause := range s.Clauses {
			if err := c.checkExpr(clause.Cond, sc); err != nil {
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
		if err := c.checkExpr(s.Cond, sc); err != nil {
			return err
		}
		c.loopDepth++
		err := c.checkBlock(s.Body, sc)
		c.loopDepth--
		return err
	case *ast.ForStmt:
		return c.checkForStmt(s, sc)
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
	case *ast.PropAssignStmt:
		if err := c.checkExpr(s.Obj, sc); err != nil {
			return err
		}
		return c.checkExpr(s.Value, sc)
	case *ast.IndexAssignStmt:
		if err := c.checkExpr(s.Obj, sc); err != nil {
			return err
		}
		if err := c.checkExpr(s.Index, sc); err != nil {
			return err
		}
		return c.checkExpr(s.Value, sc)
	default:
		return fmt.Errorf("sema: unsupported statement %T", stmt)
	}
}

// checkBlankAssign handles `_ = value` (weave_spec.md §10): `_` is a
// write-only placeholder name, always available for discarding a value
// once, but deliberately kept from behaving like an ordinary reusable
// variable in two ways — checkExpr's *ast.Ident case rejects any read of
// it unconditionally (regardless of visibility), and this function
// rejects a *second* binding of it within the same scope as an illegal
// reassignment, rather than silently reusing the existing one the way
// scope's own "assignment reuses the nearest visible binding" rule
// (see scope's doc comment) would for any other name.
//
// The check is deliberately scoped to sc's own `declared` map, not
// sc.has (which walks outward through every enclosing scope) — `_`
// intentionally does not participate in ordinary lexical
// inheritance/shadowing at all: an outer `_ = ...` never blocks an
// inner, unrelated `_` binding (e.g. a nested function's own `fn(_)
// {...}` parameter, or a sibling block's own `_ = ...`), since neither
// one can ever be read regardless of visibility, so there is nothing an
// outer binding could leak into an inner one. What IS rejected is a
// second bind attempt reachable from the exact same scope as an earlier
// one — most notably, a function whose parameter is `_` (weave_spec.md
// §5's `fn(_) {...}` — checkFuncLit declares it directly into the
// function body's own fresh scope) can never rebind `_` again anywhere
// in its own body.
func (c *checker) checkBlankAssign(s *ast.AssignStmt, sc *scope) error {
	if sc.declared["_"] {
		return fmt.Errorf("line %d: \"_\" cannot be reassigned (weave_spec.md §10) — it may be bound at most once per scope", s.Line)
	}
	if err := c.checkExpr(s.Value, sc); err != nil {
		return err
	}
	sc.declared["_"] = true
	return nil
}

// checkExpr validates identifier references. A builtin CallExpr's own
// Callee is deliberately not checked as a variable reference (see
// builtinNames) — codegen separately rejects any non-FuncLit-value
// callee it doesn't recognize as either a builtin or a general call.
func (c *checker) checkExpr(expr ast.Expr, sc *scope) error {
	switch e := expr.(type) {
	case *ast.Ident:
		if e.Name == "_" {
			return fmt.Errorf("line %d: \"_\" cannot be read (weave_spec.md §10) — it is write-only", e.Line)
		}
		if c.goVars[e.Name] != nil {
			// govar(...)-declared names (weave_spec.md §15.4) never go
			// through an ordinary assignment/scope binding at all — see
			// checkGoVarDecl's own doc comment, and goFuncs' identical
			// treatment a few lines below in the CallExpr case.
			return nil
		}
		if !sc.has(e.Name) {
			return fmt.Errorf("line %d: undefined name %q", e.Line, e.Name)
		}
		return nil
	case *ast.NumberLit, *ast.StringLit, *ast.BoolLit, *ast.NilLit:
		return nil
	case *ast.CallExpr:
		switch callee := e.Callee.(type) {
		case *ast.Ident:
			if why, ok := goAssetReservedName(callee.Name); ok {
				return fmt.Errorf("line %d: %q is a reserved name (%s); it may only appear as `name = %s(...)`", callee.Line, callee.Name, why, callee.Name)
			}
			if !builtinNames[callee.Name] && c.goFuncs[callee.Name] == nil {
				// Neither a builtin nor a gofunc(...)-declared reference
				// (goasset.go) — it must be an ordinary variable.
				if err := c.checkExpr(e.Callee, sc); err != nil {
					return err
				}
			}
		case *ast.PropExpr:
			if err := c.checkGoMethodCall(callee, sc); err != nil {
				return err
			}
		default:
			if err := c.checkExpr(e.Callee, sc); err != nil {
				return err
			}
		}
		for _, arg := range e.Args {
			if err := c.checkExpr(arg, sc); err != nil {
				return err
			}
		}
		return nil
	case *ast.BinaryExpr:
		if err := c.checkExpr(e.X, sc); err != nil {
			return err
		}
		return c.checkExpr(e.Y, sc)
	case *ast.UnaryExpr:
		return c.checkExpr(e.X, sc)
	case *ast.FuncLit:
		return c.checkFuncLit(e, sc)
	case *ast.ObjectLit:
		for _, field := range e.Fields {
			if err := c.checkExpr(field.Value, sc); err != nil {
				return err
			}
		}
		return nil
	case *ast.PropExpr:
		return c.checkExpr(e.Obj, sc)
	case *ast.IndexExpr:
		if err := c.checkExpr(e.X, sc); err != nil {
			return err
		}
		return c.checkExpr(e.Index, sc)
	default:
		return fmt.Errorf("sema: unsupported expression %T", expr)
	}
}

// checkFuncLit checks a function literal's body in a fresh scope
// (parented at sc, enabling closure capture — see scope's doc comment)
// with its parameter declared and loop-control state reset (see
// checker.loopDepth's doc comment).
func (c *checker) checkFuncLit(lit *ast.FuncLit, sc *scope) error {
	if why, ok := reservedName(lit.Param); ok {
		return fmt.Errorf("line %d: %q is a reserved name (%s)", lit.Line, lit.Param, why)
	}
	inner := newScope(sc)
	inner.declared[lit.Param] = true

	savedLoopDepth := c.loopDepth
	c.loopDepth = 0
	for _, stmt := range lit.Body {
		if err := c.checkStmt(stmt, inner); err != nil {
			c.loopDepth = savedLoopDepth
			return err
		}
	}
	c.loopDepth = savedLoopDepth
	return nil
}

// checkForStmt checks `for k, v in obj {...}` (weave_spec.md §7): obj is
// checked in sc (the enclosing scope), while Key/Value are declared
// fresh in a child scope covering the body only — mirroring
// checkFuncLit's param scoping, but with two names instead of one and
// without resetting loopDepth (break/continue are valid inside a `for`,
// same as a `while`, §7).
func (c *checker) checkForStmt(s *ast.ForStmt, sc *scope) error {
	if err := c.checkExpr(s.Obj, sc); err != nil {
		return err
	}
	if why, ok := reservedName(s.Key); ok {
		return fmt.Errorf("line %d: %q is a reserved name (%s)", s.Line, s.Key, why)
	}
	if why, ok := reservedName(s.Value); ok {
		return fmt.Errorf("line %d: %q is a reserved name (%s)", s.Line, s.Value, why)
	}
	inner := newScope(sc)
	inner.declared[s.Key] = true
	inner.declared[s.Value] = true

	c.loopDepth++
	var err error
	for _, stmt := range s.Body {
		if err = c.checkStmt(stmt, inner); err != nil {
			break
		}
	}
	c.loopDepth--
	return err
}
