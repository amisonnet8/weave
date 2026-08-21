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
	"print":  true,
	"has":    true,
	"remove": true,
	"list":   true,
	"len":    true,
	"string": true,
	"spawn":  true,
	"send":   true,
	"ask":    true,
	"reply":  true,
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

	// goStaticVars tracks the narrow case Step 4 actually resolves
	// statically: a plain variable's most recent assignment came
	// directly from a gofunc(...) call whose declared proto is a known
	// gotype — see goasset.go's trackGoStaticVar/checkGoMethodCall.
	goStaticVars map[string]string
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
		if err := c.checkExpr(s.Value, sc); err != nil {
			return err
		}
		if why, ok := reservedName(s.Name); ok {
			return fmt.Errorf("line %d: %q is a reserved name (%s)", s.Line, s.Name, why)
		}
		c.trackGoStaticVar(s.Name, s.Value)
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
	default:
		return fmt.Errorf("sema: unsupported statement %T", stmt)
	}
}

// checkExpr validates identifier references. A builtin CallExpr's own
// Callee is deliberately not checked as a variable reference (see
// builtinNames) — codegen separately rejects any non-FuncLit-value
// callee it doesn't recognize as either a builtin or a general call.
func (c *checker) checkExpr(expr ast.Expr, sc *scope) error {
	switch e := expr.(type) {
	case *ast.Ident:
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
