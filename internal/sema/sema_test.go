package sema

import (
	"strings"
	"testing"

	"github.com/amisonnet8/weave/internal/ast"
)

func TestCheck_ValidMain(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{Name: "main", Param: "args"}}
	if err := Check(file); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

func TestCheck_MissingMain(t *testing.T) {
	if err := Check(&ast.File{}); err == nil {
		t.Fatal("expected an error for a missing main")
	}
}

func TestCheck_MainParamReservedNameIsAnError(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{Name: "main", Param: "weave_main"}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: weave_main is a reserved name")
	}
}

func TestCheck_MainBodyCanReferenceItsOwnParam(t *testing.T) {
	// main = fn(args) { return len(args) } — args (whatever name main's
	// own parameter happens to use) must be visible throughout the body,
	// exactly like an ordinary function literal's own parameter.
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{&ast.ReturnStmt{Value: &ast.CallExpr{
			Callee: &ast.Ident{Name: "len"},
			Args:   []ast.Expr{&ast.Ident{Name: "args"}},
		}}},
	}}
	if err := Check(file); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

func TestCheck_MainNameIsReservedElsewhere(t *testing.T) {
	// `main` used as an ordinary variable (not the top-level entry-point
	// declaration the parser recognizes) must be rejected — mirrors
	// weave_main's own reservedName treatment.
	file := &ast.File{Main: &ast.FuncDecl{Name: "main", Param: "args"}, TopLevel: []ast.Stmt{
		&ast.AssignStmt{Name: "main", Value: &ast.NumberLit{Value: 1}},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: main is a reserved name")
	}
}

func TestCheck_AssignThenUseIsValid(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "x", Value: &ast.NumberLit{Value: 1}},
			&ast.ExprStmt{X: &ast.CallExpr{
				Callee: &ast.Ident{Name: "print"},
				Args:   []ast.Expr{&ast.Ident{Name: "x"}},
			}},
			&ast.ReturnStmt{Value: &ast.Ident{Name: "x"}},
		},
	}}
	if err := Check(file); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

func TestCheck_UseBeforeAssignIsAnError(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{&ast.ReturnStmt{Value: &ast.Ident{Name: "x"}}},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: x is used before assignment")
	}
}

func TestCheck_AssignToReservedNameIsAnError(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "weave_main", Value: &ast.NumberLit{Value: 1}},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error assigning to the reserved name weave_main")
	}
}

func TestCheck_UnderscoreParamNeverReadIsValid(t *testing.T) {
	// weave_spec.md §5/§10: fn(_) {...} — a parameter genuinely never
	// referenced in the body — must compile cleanly (this is exactly
	// what fn() {...} desugars to at the parser level).
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "f", Value: &ast.FuncLit{
				Param: "_",
				Body:  []ast.Stmt{&ast.ReturnStmt{Value: &ast.NumberLit{Value: 1}}},
			}},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

func TestCheck_ReadingUnderscoreIsAnError(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "f", Value: &ast.FuncLit{
				Param: "_",
				Body:  []ast.Stmt{&ast.ReturnStmt{Value: &ast.Ident{Name: "_"}}},
			}},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: \"_\" cannot be read")
	}
}

func TestCheck_ReassigningUnderscoreInSameScopeIsAnError(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "_", Value: &ast.NumberLit{Value: 1}},
			&ast.AssignStmt{Name: "_", Value: &ast.NumberLit{Value: 2}},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: \"_\" cannot be reassigned")
	}
}

func TestCheck_UnderscoreParamCannotBeReassignedInsideItsOwnBody(t *testing.T) {
	// fn(_) { _ = 5 } — the parameter binding already occupies "_" in
	// the function's own (fresh) scope, so a later assignment to "_"
	// inside that same body is a reassignment, not a fresh first bind.
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "f", Value: &ast.FuncLit{
				Param: "_",
				Body: []ast.Stmt{
					&ast.AssignStmt{Name: "_", Value: &ast.NumberLit{Value: 5}},
					&ast.ReturnStmt{Value: &ast.NumberLit{Value: 1}},
				},
			}},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: \"_\" cannot be reassigned inside fn(_) {...}'s own body")
	}
}

func TestCheck_UnderscoreInSiblingScopesIsIndependent(t *testing.T) {
	// Two unrelated sibling `if` blocks, each binding "_" once, are each
	// their own fresh scope — neither sees the other's "_" at all, so
	// neither counts as a reassignment (weave_spec.md §10's own
	// "checkBlankAssign only checks THIS scope" rule).
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.IfStmt{Clauses: []ast.IfClause{{
				Cond: &ast.BoolLit{Value: true},
				Body: []ast.Stmt{&ast.AssignStmt{Name: "_", Value: &ast.NumberLit{Value: 1}}},
			}}},
			&ast.IfStmt{Clauses: []ast.IfClause{{
				Cond: &ast.BoolLit{Value: true},
				Body: []ast.Stmt{&ast.AssignStmt{Name: "_", Value: &ast.NumberLit{Value: 2}}},
			}}},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

func TestCheck_UnderscoreInNestedClosureIsIndependentOfOuter(t *testing.T) {
	// An outer `_ = ...` must not block an unrelated nested closure's
	// own `fn(_) {...}` parameter — "_" deliberately doesn't participate
	// in ordinary lexical inheritance.
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "_", Value: &ast.NumberLit{Value: 1}},
			&ast.AssignStmt{Name: "f", Value: &ast.FuncLit{
				Param: "_",
				Body:  []ast.Stmt{&ast.ReturnStmt{Value: &ast.NumberLit{Value: 2}}},
			}},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

func TestCheck_AssignToDoubleUnderscorePrefixIsAnError(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "__t0", Value: &ast.NumberLit{Value: 1}},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error assigning to a __-prefixed reserved name")
	}
}

func TestCheck_BinaryExprChecksBothOperands(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{&ast.ReturnStmt{Value: &ast.BinaryExpr{
			Op: "+",
			X:  &ast.NumberLit{Value: 1},
			Y:  &ast.Ident{Name: "undefined"},
		}}},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: the right-hand operand is undefined")
	}
}

func TestCheck_UnaryExprChecksOperand(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{&ast.ReturnStmt{Value: &ast.UnaryExpr{
			Op: "-",
			X:  &ast.Ident{Name: "undefined"},
		}}},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: the unary operand is undefined")
	}
}

func TestCheck_VariableAssignedOnlyInsideIfIsInvisibleAfter(t *testing.T) {
	// if true { y = 1 }
	// return y   <- y was never assigned in an enclosing scope
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.IfStmt{Clauses: []ast.IfClause{{
				Cond: &ast.BoolLit{Value: true},
				Body: []ast.Stmt{&ast.AssignStmt{Name: "y", Value: &ast.NumberLit{Value: 1}}},
			}}},
			&ast.ReturnStmt{Value: &ast.Ident{Name: "y"}},
		},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: y is only visible inside the if-block")
	}
}

func TestCheck_ReassigningAnOuterVariableInsideIfIsVisibleAfter(t *testing.T) {
	// x = 1
	// if true { x = 2 }   <- reassigns the outer x, not a new shadowed one
	// return x
	//
	// This is the reading forced by weave_spec.md §7's own
	// `while i < 10 { i = i + 1 }` example — see sema.go's scope doc
	// comment and CLAUDE.md's Step 4 "確定した設計判断".
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "x", Value: &ast.NumberLit{Value: 1}},
			&ast.IfStmt{Clauses: []ast.IfClause{{
				Cond: &ast.BoolLit{Value: true},
				Body: []ast.Stmt{&ast.AssignStmt{Name: "x", Value: &ast.NumberLit{Value: 2}}},
			}}},
			&ast.ReturnStmt{Value: &ast.Ident{Name: "x"}},
		},
	}}
	if err := Check(file); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

func TestCheck_ElifBranchesDoNotSeeEachOthersLocals(t *testing.T) {
	// if false { y = 1 } elif true { print(y) }  <- y from the `if` branch
	// isn't visible in the `elif` branch (siblings, not nested).
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.IfStmt{Clauses: []ast.IfClause{
				{
					Cond: &ast.BoolLit{Value: false},
					Body: []ast.Stmt{&ast.AssignStmt{Name: "y", Value: &ast.NumberLit{Value: 1}}},
				},
				{
					Cond: &ast.BoolLit{Value: true},
					Body: []ast.Stmt{&ast.ExprStmt{X: &ast.CallExpr{
						Callee: &ast.Ident{Name: "print"},
						Args:   []ast.Expr{&ast.Ident{Name: "y"}},
					}}},
				},
			}},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: the elif branch cannot see the if branch's local y")
	}
}

func TestCheck_WhileLoopCounterExampleIsValid(t *testing.T) {
	// i = 0
	// while i < 10 { i = i + 1 }   <- weave_spec.md §7's own example
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "i", Value: &ast.NumberLit{Value: 0}},
			&ast.WhileStmt{
				Cond: &ast.BinaryExpr{Op: "<", X: &ast.Ident{Name: "i"}, Y: &ast.NumberLit{Value: 10}},
				Body: []ast.Stmt{&ast.AssignStmt{
					Name:  "i",
					Value: &ast.BinaryExpr{Op: "+", X: &ast.Ident{Name: "i"}, Y: &ast.NumberLit{Value: 1}},
				}},
			},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

func TestCheck_BreakOutsideLoopIsAnError(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{&ast.BreakStmt{}, &ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}}},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: break outside of a loop")
	}
}

func TestCheck_ContinueOutsideLoopIsAnError(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{&ast.ContinueStmt{}, &ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}}},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: continue outside of a loop")
	}
}

func TestCheck_BreakInsideWhileIsValid(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.WhileStmt{
				Cond: &ast.BoolLit{Value: true},
				Body: []ast.Stmt{&ast.BreakStmt{}},
			},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

func TestCheck_BreakAfterWhileIsAnErrorAgain(t *testing.T) {
	// loopDepth must be decremented after the while ends.
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.WhileStmt{Cond: &ast.BoolLit{Value: false}, Body: nil},
			&ast.BreakStmt{},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: break after the while has ended is outside any loop")
	}
}

func TestCheck_FuncLitCanReadOuterVariable(t *testing.T) {
	// base = 100
	// addBase = fn(x) { return x + base }
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "base", Value: &ast.NumberLit{Value: 100}},
			&ast.AssignStmt{Name: "addBase", Value: &ast.FuncLit{
				Param: "x",
				Body: []ast.Stmt{&ast.ReturnStmt{Value: &ast.BinaryExpr{
					Op: "+", X: &ast.Ident{Name: "x"}, Y: &ast.Ident{Name: "base"},
				}}},
			}},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

func TestCheck_FuncLitParamNotVisibleOutside(t *testing.T) {
	// f = fn(x) { return x }
	// return x   <- x is the closure's own param, not visible in main
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "f", Value: &ast.FuncLit{
				Param: "x",
				Body:  []ast.Stmt{&ast.ReturnStmt{Value: &ast.Ident{Name: "x"}}},
			}},
			&ast.ReturnStmt{Value: &ast.Ident{Name: "x"}},
		},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: x is the closure's own parameter, not visible in main")
	}
}

func TestCheck_FuncLitReservedParamNameIsAnError(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "f", Value: &ast.FuncLit{
				Param: "__t0",
				Body:  []ast.Stmt{&ast.ReturnStmt{Value: &ast.NumberLit{Value: 1}}},
			}},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: __t0 is a reserved parameter name")
	}
}

func TestCheck_BreakInsideFuncLitInsideWhileIsAnError(t *testing.T) {
	// A function literal is its own boundary for break/continue, even
	// when lexically nested inside a while loop (weave_spec.md doesn't
	// say this explicitly — see sema.go checker.loopDepth's doc comment
	// for why this is treated as settled rather than left open).
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.WhileStmt{
				Cond: &ast.BoolLit{Value: true},
				Body: []ast.Stmt{
					&ast.AssignStmt{Name: "f", Value: &ast.FuncLit{
						Param: "x",
						Body:  []ast.Stmt{&ast.BreakStmt{}},
					}},
					&ast.BreakStmt{},
				},
			},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: break inside the closure cannot reach the enclosing while")
	}
}

func TestCheck_GeneralCallCalleeMustBeDefined(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{&ast.ReturnStmt{Value: &ast.CallExpr{
			Callee: &ast.Ident{Name: "undefinedFunc"},
			Args:   []ast.Expr{&ast.NumberLit{Value: 1}},
		}}},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: undefinedFunc is not a builtin or a declared variable")
	}
}

func TestCheck_BuiltinCalleeIsNotTreatedAsAVariable(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.ExprStmt{X: &ast.CallExpr{
				Callee: &ast.Ident{Name: "print"},
				Args:   []ast.Expr{&ast.StringLit{Value: "hi"}},
			}},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

func TestCheck_ObjectLitChecksFieldValues(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{&ast.ReturnStmt{Value: &ast.ObjectLit{
			Fields: []ast.ObjectField{{Name: "x", Value: &ast.Ident{Name: "undefined"}}},
		}}},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: object field value is undefined")
	}
}

func TestCheck_PropExprChecksObj(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{&ast.ReturnStmt{Value: &ast.PropExpr{
			Obj: &ast.Ident{Name: "undefined"}, Prop: "x",
		}}},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: property access on an undefined object")
	}
}

func TestCheck_PropAssignChecksObjAndValue(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "o", Value: &ast.ObjectLit{}},
			&ast.PropAssignStmt{Obj: &ast.Ident{Name: "o"}, Prop: "x", Value: &ast.Ident{Name: "undefined"}},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: property assignment value is undefined")
	}
}

func TestCheck_HasAndRemoveAreBuiltins(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "o", Value: &ast.ObjectLit{}},
			&ast.ExprStmt{X: &ast.CallExpr{
				Callee: &ast.Ident{Name: "has"},
				Args:   []ast.Expr{&ast.Ident{Name: "o"}, &ast.StringLit{Value: "x"}},
			}},
			&ast.ExprStmt{X: &ast.CallExpr{
				Callee: &ast.Ident{Name: "remove"},
				Args:   []ast.Expr{&ast.Ident{Name: "o"}, &ast.StringLit{Value: "x"}},
			}},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

func TestCheck_ForInDeclaresKeyAndValue(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "o", Value: &ast.ObjectLit{}},
			&ast.ForStmt{
				Key: "k", Value: "v", Obj: &ast.Ident{Name: "o"},
				Body: []ast.Stmt{
					&ast.ExprStmt{X: &ast.CallExpr{Callee: &ast.Ident{Name: "print"}, Args: []ast.Expr{&ast.Ident{Name: "k"}}}},
					&ast.ExprStmt{X: &ast.CallExpr{Callee: &ast.Ident{Name: "print"}, Args: []ast.Expr{&ast.Ident{Name: "v"}}}},
				},
			},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

func TestCheck_ForInKeyNotVisibleOutside(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "o", Value: &ast.ObjectLit{}},
			&ast.ForStmt{Key: "k", Value: "v", Obj: &ast.Ident{Name: "o"}},
			&ast.ReturnStmt{Value: &ast.Ident{Name: "k"}},
		},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: k is the for-loop's own key variable, not visible after the loop")
	}
}

func TestCheck_BreakInsideForIsValid(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "o", Value: &ast.ObjectLit{}},
			&ast.ForStmt{
				Key: "k", Value: "v", Obj: &ast.Ident{Name: "o"},
				Body: []ast.Stmt{&ast.BreakStmt{}},
			},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

func TestCheck_ListLenStringAreBuiltins(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.ExprStmt{X: &ast.CallExpr{Callee: &ast.Ident{Name: "list"}, Args: []ast.Expr{&ast.NumberLit{Value: 1}}}},
			&ast.ExprStmt{X: &ast.CallExpr{Callee: &ast.Ident{Name: "len"}, Args: []ast.Expr{&ast.StringLit{Value: "hi"}}}},
			&ast.ExprStmt{X: &ast.CallExpr{Callee: &ast.Ident{Name: "string"}, Args: []ast.Expr{&ast.NumberLit{Value: 1}}}},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

func TestCheck_AtAndRaiseIfErrorAreBuiltins(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "l", Value: &ast.CallExpr{Callee: &ast.Ident{Name: "list"}, Args: []ast.Expr{&ast.NumberLit{Value: 1}}}},
			&ast.ExprStmt{X: &ast.CallExpr{Callee: &ast.Ident{Name: "at"}, Args: []ast.Expr{&ast.Ident{Name: "l"}, &ast.NumberLit{Value: 0}}}},
			&ast.ExprStmt{X: &ast.CallExpr{Callee: &ast.Ident{Name: "raiseIfError"}, Args: []ast.Expr{&ast.NilLit{}}}},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

func TestCheck_ActorBuiltinsDoNotRequireCalleeVariableCheck(t *testing.T) {
	// send(a)("increment", 5) — the OUTER call's callee is itself a
	// CallExpr rooted at the builtin `send`, not a plain Ident, so it
	// must not be mistaken for a general call needing `send` itself to
	// resolve as a declared variable.
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "a", Value: &ast.CallExpr{
				Callee: &ast.Ident{Name: "spawn"},
				Args:   []ast.Expr{&ast.ObjectLit{}},
			}},
			&ast.ExprStmt{X: &ast.CallExpr{
				Callee: &ast.CallExpr{Callee: &ast.Ident{Name: "send"}, Args: []ast.Expr{&ast.Ident{Name: "a"}}},
				Args:   []ast.Expr{&ast.StringLit{Value: "increment"}, &ast.NumberLit{Value: 5}},
			}},
			&ast.ExprStmt{X: &ast.CallExpr{
				Callee: &ast.CallExpr{Callee: &ast.Ident{Name: "ask"}, Args: []ast.Expr{&ast.Ident{Name: "a"}}},
				Args:   []ast.Expr{&ast.StringLit{Value: "get"}},
			}},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

func goTypeDeclExpr(goName string, members []ast.ObjectField) *ast.CallExpr {
	return &ast.CallExpr{
		Callee: &ast.Ident{Name: "gotype"},
		Args:   []ast.Expr{&ast.StringLit{Value: goName}, &ast.ObjectLit{Fields: members}},
	}
}

// gomethodExpr builds the untyped `gomethod("GoName")` form
// (weave_spec.md §15.1) — reflect-dispatched, no goReturns/goParams.
func gomethodExpr(goMethodName string) ast.Expr {
	return &ast.CallExpr{Callee: &ast.Ident{Name: "gomethod"}, Args: []ast.Expr{&ast.StringLit{Value: goMethodName}}}
}

// goReturnsExpr builds `goReturns(item1, item2, ...)` (weave_spec.md
// §15.4) — each item is either a *ast.StringLit ("?pkg.Type") or an
// *ast.Ident naming an earlier gotype(...) declaration (a proto-bound
// position).
func goReturnsExpr(items ...ast.Expr) ast.Expr {
	return &ast.CallExpr{Callee: &ast.Ident{Name: "goReturns"}, Args: items}
}

// goParamsExpr builds `goParams("?T1", "?T2", ...)` (weave_spec.md §15.4).
func goParamsExpr(types ...string) ast.Expr {
	args := make([]ast.Expr, len(types))
	for i, t := range types {
		args[i] = &ast.StringLit{Value: t}
	}
	return &ast.CallExpr{Callee: &ast.Ident{Name: "goParams"}, Args: args}
}

// typedGomethodExpr builds the typed `gomethod(goMethodName,
// goReturns(...), goParams(...))` form (weave_spec.md §15.1/§15.4's
// all-or-nothing shape — see GoMethodInfo's doc comment).
func typedGomethodExpr(goMethodName string, returns ast.Expr, paramTypes ...string) ast.Expr {
	return &ast.CallExpr{Callee: &ast.Ident{Name: "gomethod"}, Args: []ast.Expr{
		&ast.StringLit{Value: goMethodName}, returns, goParamsExpr(paramTypes...),
	}}
}

// goFuncDeclExpr builds the untyped `gofunc("?pkg.Func")` form
// (weave_spec.md §15.2) — a single argument, no goReturns/goParams.
func goFuncDeclExpr(goName string) *ast.CallExpr {
	return &ast.CallExpr{Callee: &ast.Ident{Name: "gofunc"}, Args: []ast.Expr{&ast.StringLit{Value: goName}}}
}

// typedGoFuncDeclExpr builds the typed `gofunc("?pkg.Func",
// goReturns(...), goParams(...))` form (weave_spec.md §15.2/§15.4's
// all-or-nothing shape — see GoFuncInfo's doc comment).
func typedGoFuncDeclExpr(goName string, returns ast.Expr, paramTypes ...string) *ast.CallExpr {
	return &ast.CallExpr{Callee: &ast.Ident{Name: "gofunc"}, Args: []ast.Expr{
		&ast.StringLit{Value: goName}, returns, goParamsExpr(paramTypes...),
	}}
}

func TestCheck_GoTypeAndGoFuncDeclAreValid(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "GoFile", Value: goTypeDeclExpr("?os.File", []ast.ObjectField{
				{Name: "Close", Value: gomethodExpr("Close")},
			})},
			&ast.AssignStmt{Name: "goOpen", Value: typedGoFuncDeclExpr("?os.Open", goReturnsExpr(&ast.Ident{Name: "GoFile"}), "?string")},
			&ast.AssignStmt{Name: "toUpper", Value: goFuncDeclExpr("?strings.ToUpper")},
			&ast.ExprStmt{X: &ast.CallExpr{Callee: &ast.Ident{Name: "toUpper"}, Args: []ast.Expr{&ast.StringLit{Value: "hi"}}}},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

func TestCheck_GoFuncNameMustHaveQuestionMarkPrefix(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "f", Value: goFuncDeclExpr("strings.ToUpper")},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: Go function name must start with '?'")
	}
}

func TestCheck_GoFuncUnknownProtoIsAnError(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "f", Value: typedGoFuncDeclExpr("?os.Open", goReturnsExpr(&ast.Ident{Name: "NeverDeclared"}), "?string")},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: NeverDeclared is not a gotype(...)")
	}
}

func TestCheck_GoFuncAllOrNothingMismatchIsAnError(t *testing.T) {
	// Exactly 1 argument (untyped) or exactly 3 (typed) are the only
	// valid gofunc(...) shapes now — no partial typing (weave_spec.md
	// §15.4's all-or-nothing rule).
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "f", Value: &ast.CallExpr{
				Callee: &ast.Ident{Name: "gofunc"},
				Args:   []ast.Expr{&ast.StringLit{Value: "?strings.ToUpper"}, goReturnsExpr(&ast.StringLit{Value: "?string"})},
			}},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: gofunc(...) with goReturns(...) but no goParams(...) is neither 1 nor 3 arguments")
	}
}

// govarDeclExpr builds `govar("?pkg.Var")` (weave_spec.md §15.5).
func govarDeclExpr(goName string) *ast.CallExpr {
	return &ast.CallExpr{Callee: &ast.Ident{Name: "govar"}, Args: []ast.Expr{&ast.StringLit{Value: goName}}}
}

func TestCheck_GovarDeclAndReadAreValid(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "ErrRange", Value: govarDeclExpr("?strconv.ErrRange")},
			&ast.ExprStmt{X: &ast.CallExpr{Callee: &ast.Ident{Name: "print"}, Args: []ast.Expr{&ast.Ident{Name: "ErrRange"}}}},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

func TestCheck_GovarNameMustHaveQuestionMarkPrefix(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "x", Value: govarDeclExpr("strconv.ErrRange")},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: Go variable name must start with '?'")
	}
}

func TestCheck_GovarWrongArgCountIsAnError(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "x", Value: &ast.CallExpr{Callee: &ast.Ident{Name: "govar"}}},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: govar(...) takes exactly one argument")
	}
}

func TestCheck_BareGovarCallOutsideDeclarationIsAnError(t *testing.T) {
	// govar is reserved (goAssetReservedName) and may only appear as
	// `name = govar(...)` — an ordinary call site (even one that would
	// otherwise typecheck, like passing its result to print) must be
	// rejected with a clear message, not the generic "undefined name".
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.ExprStmt{X: &ast.CallExpr{Callee: &ast.Ident{Name: "print"}, Args: []ast.Expr{govarDeclExpr("?strconv.ErrRange")}}},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: govar(...) may only appear as name = govar(...)")
	}
}

func TestCheck_ByteSliceReturnAndParamHintsAreValid(t *testing.T) {
	// weave_spec.md §15.4: "?[]byte" is the one supported slice type
	// hint, valid in both goReturns(...) (os.ReadFile-shaped) and
	// goParams(...) (bytes.NewReader-shaped) positions.
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "readFile", Value: typedGoFuncDeclExpr("?os.ReadFile", goReturnsExpr(&ast.StringLit{Value: "?[]byte"}, &ast.StringLit{Value: "?error"}), "?string")},
			&ast.AssignStmt{Name: "newReader", Value: typedGoFuncDeclExpr("?bytes.NewReader", goReturnsExpr(&ast.StringLit{Value: "?any"}), "?[]byte")},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

func TestCheck_UnsupportedSliceReturnHintIsAnError(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "f", Value: typedGoFuncDeclExpr("?os.ReadFile", goReturnsExpr(&ast.StringLit{Value: "?[]int"}), "?string")},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: only \"?[]byte\" is a supported slice type hint")
	}
}

func TestCheck_UnsupportedSliceParamHintIsAnError(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "f", Value: typedGoFuncDeclExpr("?bytes.NewReader", goReturnsExpr(&ast.StringLit{Value: "?any"}), "?[]string")},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: only \"?[]byte\" is a supported slice type hint")
	}
}

func TestCheck_GomethodAllOrNothingMismatchIsAnError(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "GoReader", Value: goTypeDeclExpr("?*strings.Reader", []ast.ObjectField{
				{Name: "len", Value: &ast.CallExpr{
					Callee: &ast.Ident{Name: "gomethod"},
					Args:   []ast.Expr{&ast.StringLit{Value: "Len"}, goReturnsExpr(&ast.StringLit{Value: "?int"})},
				}},
			})},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: gomethod(...) with goReturns(...) but no goParams(...) is neither 1 nor 3 arguments")
	}
}

func TestCheck_BareGoReturnsCallIsAnError(t *testing.T) {
	// goReturns(...) is only recognized inside a gomethod(...)/gofunc(...)
	// signature position — used bare, it must be rejected with a clear
	// "reserved name" error, not a confusing "undefined name"
	// (goAssetReservedName).
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.ExprStmt{X: &ast.CallExpr{Callee: &ast.Ident{Name: "print"}, Args: []ast.Expr{goReturnsExpr(&ast.StringLit{Value: "?int"})}}},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	err := Check(file)
	if err == nil {
		t.Fatal("expected an error: goReturns(...) used outside a signature position")
	}
	if !strings.Contains(err.Error(), "goReturns") {
		t.Fatalf("expected error to mention goReturns, got: %v", err)
	}
}

func TestCheck_BareGoParamsCallIsAnError(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.ExprStmt{X: &ast.CallExpr{Callee: &ast.Ident{Name: "print"}, Args: []ast.Expr{goParamsExpr("?int")}}},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	err := Check(file)
	if err == nil {
		t.Fatal("expected an error: goParams(...) used outside a signature position")
	}
	if !strings.Contains(err.Error(), "goParams") {
		t.Fatalf("expected error to mention goParams, got: %v", err)
	}
}

func TestCheck_GotypeOutsideDeclarationShapeIsAnError(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.ExprStmt{X: &ast.CallExpr{
				Callee: &ast.Ident{Name: "print"},
				Args:   []ast.Expr{goTypeDeclExpr("?os.File", nil)},
			}},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: gotype(...) used outside `name = gotype(...)`")
	}
}

func TestCheck_GoTypeMemberMustBeGomethod(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "GoFile", Value: goTypeDeclExpr("?os.File", []ast.ObjectField{
				{Name: "Close", Value: &ast.NumberLit{Value: 1}},
			})},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: member value must be gomethod(...)")
	}
}

// atExpr builds `at(listExpr, index)` (weave_spec.md §11/§15.4) —
// helper for tests exercising the proto-bound `at(...)` static-typing
// propagation (trackAtResult).
func atExpr(list ast.Expr, index float64) *ast.CallExpr {
	return &ast.CallExpr{Callee: &ast.Ident{Name: "at"}, Args: []ast.Expr{list, &ast.NumberLit{Value: index}}}
}

// typedGoReaderStmts declares a typed GoReader gotype (a proto-bound
// `len` method) and a typed newReader gofunc whose sole return position
// is bound to that proto, then extracts a call result into `r` via
// `at(...)` — the sequence that makes `r` statically Go-typed
// (goStaticVars) and lets `r.len()` resolve natively. Shared by several
// tests below.
func typedGoReaderStmts() []ast.Stmt {
	return []ast.Stmt{
		&ast.AssignStmt{Name: "GoReader", Value: goTypeDeclExpr("?*strings.Reader", []ast.ObjectField{
			{Name: "len", Value: typedGomethodExpr("Len", goReturnsExpr(&ast.StringLit{Value: "?int"}))},
		})},
		&ast.AssignStmt{Name: "newReader", Value: typedGoFuncDeclExpr("?strings.NewReader", goReturnsExpr(&ast.Ident{Name: "GoReader"}), "?string")},
		&ast.AssignStmt{Name: "result", Value: &ast.CallExpr{
			Callee: &ast.Ident{Name: "newReader"},
			Args:   []ast.Expr{&ast.StringLit{Value: "hi"}},
		}},
		&ast.AssignStmt{Name: "r", Value: atExpr(&ast.Ident{Name: "result"}, 0)},
	}
}

func TestCheck_StaticGoMethodCallIsValid(t *testing.T) {
	body := append(typedGoReaderStmts(),
		&ast.ExprStmt{X: &ast.CallExpr{Callee: &ast.PropExpr{Obj: &ast.Ident{Name: "r"}, Prop: "len"}}},
		&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
	)
	file := &ast.File{Main: &ast.FuncDecl{Name: "main", Param: "args", Body: body}}
	if err := Check(file); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

func TestCheck_StaticGoMethodCallRejectsUnknownMember(t *testing.T) {
	body := append(typedGoReaderStmts(),
		&ast.ExprStmt{X: &ast.CallExpr{Callee: &ast.PropExpr{Obj: &ast.Ident{Name: "r"}, Prop: "notDeclared"}}},
		&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
	)
	file := &ast.File{Main: &ast.FuncDecl{Name: "main", Param: "args", Body: body}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: notDeclared was never gomethod(...)-declared on GoReader")
	}
}

func TestCheck_TypedGomethodAndGofuncDeclAreValid(t *testing.T) {
	body := append(typedGoReaderStmts(),
		&ast.ExprStmt{X: &ast.CallExpr{Callee: &ast.PropExpr{Obj: &ast.Ident{Name: "r"}, Prop: "len"}}},
		&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
	)
	file := &ast.File{Main: &ast.FuncDecl{Name: "main", Param: "args", Body: body}}
	if err := Check(file); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

func TestCheck_TypedGomethodWrongArgCountIsAnError(t *testing.T) {
	body := append(typedGoReaderStmts(),
		&ast.ExprStmt{X: &ast.CallExpr{
			Callee: &ast.PropExpr{Obj: &ast.Ident{Name: "r"}, Prop: "len"},
			Args:   []ast.Expr{&ast.NumberLit{Value: 1}}, // len takes none
		}},
		&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
	)
	file := &ast.File{Main: &ast.FuncDecl{Name: "main", Param: "args", Body: body}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: len(...) declared 0 params but was called with 1")
	}
}

func TestCheck_GomethodTypeArgMissingQuestionMarkIsAnError(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "GoReader", Value: goTypeDeclExpr("?*strings.Reader", []ast.ObjectField{
				{Name: "len", Value: typedGomethodExpr("Len", goReturnsExpr(&ast.StringLit{Value: "int"}))}, // missing '?'
			})},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: return type must start with '?'")
	}
}

func TestCheck_GoReturnsWithProtoAndAtExtractionIsValid(t *testing.T) {
	// goReturns(GoReader, "?error") — a two-position typed return list
	// where position 0 is proto-bound; at(result, 0) must propagate that
	// binding to `r` so `r.len()` resolves natively, exactly like the
	// single-position case above.
	body := []ast.Stmt{
		&ast.AssignStmt{Name: "GoReader", Value: goTypeDeclExpr("?*strings.Reader", []ast.ObjectField{
			{Name: "len", Value: typedGomethodExpr("Len", goReturnsExpr(&ast.StringLit{Value: "?int"}))},
		})},
		&ast.AssignStmt{Name: "newReaderChecked", Value: typedGoFuncDeclExpr("?strings.NewReader",
			goReturnsExpr(&ast.Ident{Name: "GoReader"}, &ast.StringLit{Value: "?error"}), "?string")},
		&ast.AssignStmt{Name: "result", Value: &ast.CallExpr{
			Callee: &ast.Ident{Name: "newReaderChecked"},
			Args:   []ast.Expr{&ast.StringLit{Value: "hi"}},
		}},
		&ast.AssignStmt{Name: "r", Value: atExpr(&ast.Ident{Name: "result"}, 0)},
		&ast.ExprStmt{X: &ast.CallExpr{Callee: &ast.PropExpr{Obj: &ast.Ident{Name: "r"}, Prop: "len"}}},
		&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
	}
	file := &ast.File{Main: &ast.FuncDecl{Name: "main", Param: "args", Body: body}}
	if err := Check(file); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

func TestCheck_ReassignedVarLosesStaticGoType(t *testing.T) {
	// r starts Go-typed (via at(...) extraction), then gets reassigned to
	// an ordinary object — r.someProp() afterward must be treated as an
	// ordinary dynamic call, not a (now-stale) static Go method
	// resolution.
	body := append(typedGoReaderStmts(),
		&ast.AssignStmt{Name: "r", Value: &ast.ObjectLit{Fields: []ast.ObjectField{
			{Name: "len", Value: &ast.FuncLit{Param: "self", Body: []ast.Stmt{&ast.ReturnStmt{Value: &ast.NumberLit{Value: 1}}}}},
		}}},
		&ast.ExprStmt{X: &ast.CallExpr{
			Callee: &ast.PropExpr{Obj: &ast.Ident{Name: "r"}, Prop: "len"},
		}},
		&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
	)
	file := &ast.File{Main: &ast.FuncDecl{Name: "main", Param: "args", Body: body}}
	if err := Check(file); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

func TestCheck_TopLevelStatementsShareScopeWithMain(t *testing.T) {
	// A name bound at top level (outside func main) must be visible
	// inside main's body — TopLevel and Main.Body are checked against
	// the same root scope (see ast.File's doc comment).
	file := &ast.File{
		TopLevel: []ast.Stmt{
			&ast.AssignStmt{Name: "proto", Value: &ast.ObjectLit{Fields: []ast.ObjectField{
				{Name: "x", Value: &ast.NumberLit{Value: 1}},
			}}},
		},
		Main: &ast.FuncDecl{
			Name: "main", Param: "args",
			Body: []ast.Stmt{
				&ast.ExprStmt{X: &ast.PropExpr{Obj: &ast.Ident{Name: "proto"}, Prop: "x"}},
				&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
			},
		},
	}
	if err := Check(file); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

func TestCheck_TopLevelUndefinedNameIsAnError(t *testing.T) {
	file := &ast.File{
		TopLevel: []ast.Stmt{
			&ast.ExprStmt{X: &ast.Ident{Name: "undefined"}},
		},
		Main: &ast.FuncDecl{
			Name: "main", Param: "args",
			Body: []ast.Stmt{&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}}},
		},
	}
	if err := Check(file); err == nil {
		t.Fatal("expected an error referencing an undefined top-level name")
	}
}

func TestCheck_SelfRecursiveFuncLitIsValid(t *testing.T) {
	// fact = fn(n) { ... fact(n - 1) ... } — a function literal assigned
	// directly to a name may refer to itself inside its own body.
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "fact", Value: &ast.FuncLit{
				Param: "n",
				Body: []ast.Stmt{&ast.ReturnStmt{Value: &ast.CallExpr{
					Callee: &ast.Ident{Name: "fact"},
					Args:   []ast.Expr{&ast.Ident{Name: "n"}},
				}}},
			}},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

func TestCheck_MutualRecursionIsStillAnError(t *testing.T) {
	// The self-recursion carve-out only pre-declares a FuncLit's own
	// name for itself — a second closure referring to a name not yet
	// assigned (mutual recursion) must still fail, exactly as before.
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "isEven", Value: &ast.FuncLit{
				Param: "n",
				Body: []ast.Stmt{&ast.ReturnStmt{Value: &ast.CallExpr{
					Callee: &ast.Ident{Name: "isOdd"},
					Args:   []ast.Expr{&ast.Ident{Name: "n"}},
				}}},
			}},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error referencing isOdd before it's assigned")
	}
}

// shapeDeclExpr builds `shape({ field: "hint", ... })` (weave_spec.md
// §4.3).
func shapeDeclExpr(fields map[string]string) *ast.CallExpr {
	var objFields []ast.ObjectField
	for name, hint := range fields {
		objFields = append(objFields, ast.ObjectField{Name: name, Value: &ast.StringLit{Value: hint}})
	}
	return &ast.CallExpr{Callee: &ast.Ident{Name: "shape"}, Args: []ast.Expr{&ast.ObjectLit{Fields: objFields}}}
}

func TestCheck_ShapeDeclAndCheckShapeCallAreValid(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "PointShape", Value: shapeDeclExpr(map[string]string{"x": "number", "y": "number"})},
			&ast.AssignStmt{Name: "p", Value: &ast.ObjectLit{Fields: []ast.ObjectField{
				{Name: "x", Value: &ast.NumberLit{Value: 1}},
				{Name: "y", Value: &ast.NumberLit{Value: 2}},
			}}},
			&ast.ExprStmt{X: &ast.CallExpr{
				Callee: &ast.Ident{Name: "checkShape"},
				Args:   []ast.Expr{&ast.Ident{Name: "PointShape"}, &ast.Ident{Name: "p"}},
			}},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

func TestCheck_ShapeUnknownHintIsAnError(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "S", Value: shapeDeclExpr(map[string]string{"x": "integer"})}, // not a recognized hint
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: \"integer\" is not a recognized shape hint")
	}
}

func TestCheck_CheckShapeFirstArgMustBeShapeDeclared(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "notAShape", Value: &ast.NumberLit{Value: 1}},
			&ast.AssignStmt{Name: "p", Value: &ast.ObjectLit{}},
			&ast.ExprStmt{X: &ast.CallExpr{
				Callee: &ast.Ident{Name: "checkShape"},
				Args:   []ast.Expr{&ast.Ident{Name: "notAShape"}, &ast.Ident{Name: "p"}},
			}},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: checkShape's first argument must be a shape(...) declared name")
	}
}

func TestCheck_ShapeOutsideDeclarationShapeIsAnError(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.ExprStmt{X: &ast.CallExpr{
				Callee: &ast.Ident{Name: "print"},
				Args:   []ast.Expr{shapeDeclExpr(map[string]string{"x": "number"})},
			}},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: shape(...) used outside `name = shape(...)`")
	}
}

func TestCheck_NonRecursiveFuncLitStillReservedNameChecked(t *testing.T) {
	// The pre-declare carve-out for FuncLit RHS must not skip the
	// reserved-name check.
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "weave_main", Value: &ast.FuncLit{
				Param: "x",
				Body:  []ast.Stmt{&ast.ReturnStmt{Value: &ast.Ident{Name: "x"}}},
			}},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error assigning a FuncLit to a reserved name")
	}
}
