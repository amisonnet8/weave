package sema

import (
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

func TestCheck_IndexExprChecksXAndIndex(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{&ast.ReturnStmt{Value: &ast.IndexExpr{
			X: &ast.Ident{Name: "undefined"}, Index: &ast.NumberLit{Value: 0},
		}}},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: index access on an undefined object")
	}
}

func TestCheck_IndexExprChecksIndexExpr(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "nums", Value: &ast.CallExpr{Callee: &ast.Ident{Name: "list"}, Args: []ast.Expr{&ast.NumberLit{Value: 1}}}},
			&ast.ReturnStmt{Value: &ast.IndexExpr{X: &ast.Ident{Name: "nums"}, Index: &ast.Ident{Name: "undefined"}}},
		},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: index expression itself is undefined")
	}
}

func TestCheck_IndexAssignChecksObjIndexAndValue(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "nums", Value: &ast.CallExpr{Callee: &ast.Ident{Name: "list"}, Args: []ast.Expr{&ast.NumberLit{Value: 1}}}},
			&ast.IndexAssignStmt{Obj: &ast.Ident{Name: "nums"}, Index: &ast.NumberLit{Value: 0}, Value: &ast.Ident{Name: "undefined"}},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: index assignment value is undefined")
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

// gomethodExpr builds `gomethod("GoName")`, or `gomethod("GoName",
// protoName)` when proto is given — the latter binds position 0 of the
// method's returned list to an earlier gotype(...) declaration
// (weave_spec.md §15.1).
func gomethodExpr(goMethodName string, proto ...string) ast.Expr {
	args := []ast.Expr{&ast.StringLit{Value: goMethodName}}
	if len(proto) == 1 {
		args = append(args, &ast.Ident{Name: proto[0]})
	}
	return &ast.CallExpr{Callee: &ast.Ident{Name: "gomethod"}, Args: args}
}

// goFuncDeclExpr builds `gofunc("?pkg.Func")`, or `gofunc("?pkg.Func",
// protoName)` when proto is given — the latter binds position 0 of the
// call's returned list to an earlier gotype(...) declaration
// (weave_spec.md §15.2).
func goFuncDeclExpr(goName string, proto ...string) *ast.CallExpr {
	args := []ast.Expr{&ast.StringLit{Value: goName}}
	if len(proto) == 1 {
		args = append(args, &ast.Ident{Name: proto[0]})
	}
	return &ast.CallExpr{Callee: &ast.Ident{Name: "gofunc"}, Args: args}
}

func TestCheck_GoTypeAndGoFuncDeclAreValid(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "GoFile", Value: goTypeDeclExpr("?os.File", []ast.ObjectField{
				{Name: "Close", Value: gomethodExpr("Close")},
			})},
			&ast.AssignStmt{Name: "goOpen", Value: goFuncDeclExpr("?os.Open", "GoFile")},
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
			&ast.AssignStmt{Name: "f", Value: goFuncDeclExpr("?os.Open", "NeverDeclared")},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: NeverDeclared is not a gotype(...)")
	}
}

func TestCheck_GoFuncTooManyArgsIsAnError(t *testing.T) {
	// gofunc(...) takes a Go function name, and optionally a gotype(...)
	// proto name — never more than that (weave_spec.md §15.2's own
	// `gofunc(name, protoOrNil)`-style shape, after §15.4's goReturns/
	// goParams type hints were removed).
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "f", Value: &ast.CallExpr{
				Callee: &ast.Ident{Name: "gofunc"},
				Args:   []ast.Expr{&ast.StringLit{Value: "?strings.ToUpper"}, &ast.Ident{Name: "GoFile"}, &ast.StringLit{Value: "extra"}},
			}},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: gofunc(...) takes at most two arguments")
	}
}

// govarDeclExpr builds `govar("?pkg.Var")` (weave_spec.md §15.4).
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

func TestCheck_GomethodTooManyArgsIsAnError(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "GoReader", Value: goTypeDeclExpr("?*strings.Reader", []ast.ObjectField{
				{Name: "len", Value: &ast.CallExpr{
					Callee: &ast.Ident{Name: "gomethod"},
					Args:   []ast.Expr{&ast.StringLit{Value: "Len"}, &ast.Ident{Name: "GoReader"}, &ast.StringLit{Value: "extra"}},
				}},
			})},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: gomethod(...) takes at most two arguments")
	}
}

func TestCheck_GomethodUnknownProtoIsAnError(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "GoReader", Value: goTypeDeclExpr("?*strings.Reader", []ast.ObjectField{
				{Name: "len", Value: gomethodExpr("Len", "NeverDeclared")},
			})},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: NeverDeclared is not a gotype(...)")
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

// atExpr builds `at(listExpr, index)` (weave_spec.md §11) — helper for
// tests exercising the proto-bound `at(...)` static-typing propagation
// (trackAtResult).
func atExpr(list ast.Expr, index float64) *ast.CallExpr {
	return &ast.CallExpr{Callee: &ast.Ident{Name: "at"}, Args: []ast.Expr{list, &ast.NumberLit{Value: index}}}
}

// goReaderStmts declares a proto-bound GoReader gotype (a proto-less
// `len` method — it just returns a plain number) and a proto-bound
// newReader gofunc whose returned list's position 0 is bound to that
// proto, then extracts the call result into `r` via `at(result, 0)` —
// the sequence that makes `r` statically Go-typed (goStaticVars) and
// lets `r.len()` resolve at compile time. Shared by several tests below.
func goReaderStmts() []ast.Stmt {
	return []ast.Stmt{
		&ast.AssignStmt{Name: "GoReader", Value: goTypeDeclExpr("?*strings.Reader", []ast.ObjectField{
			{Name: "len", Value: gomethodExpr("Len")},
		})},
		&ast.AssignStmt{Name: "newReader", Value: goFuncDeclExpr("?strings.NewReader", "GoReader")},
		&ast.AssignStmt{Name: "result", Value: &ast.CallExpr{
			Callee: &ast.Ident{Name: "newReader"},
			Args:   []ast.Expr{&ast.StringLit{Value: "hi"}},
		}},
		&ast.AssignStmt{Name: "r", Value: atExpr(&ast.Ident{Name: "result"}, 0)},
	}
}

func TestCheck_StaticGoMethodCallIsValid(t *testing.T) {
	body := append(goReaderStmts(),
		&ast.ExprStmt{X: &ast.CallExpr{Callee: &ast.PropExpr{Obj: &ast.Ident{Name: "r"}, Prop: "len"}}},
		&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
	)
	file := &ast.File{Main: &ast.FuncDecl{Name: "main", Param: "args", Body: body}}
	if err := Check(file); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

func TestCheck_StaticGoMethodCallRejectsUnknownMember(t *testing.T) {
	body := append(goReaderStmts(),
		&ast.ExprStmt{X: &ast.CallExpr{Callee: &ast.PropExpr{Obj: &ast.Ident{Name: "r"}, Prop: "notDeclared"}}},
		&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
	)
	file := &ast.File{Main: &ast.FuncDecl{Name: "main", Param: "args", Body: body}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: notDeclared was never gomethod(...)-declared on GoReader")
	}
}

func TestCheck_GoMethodProtoBindingPropagatesThroughChainedCall(t *testing.T) {
	// A gomethod(...) itself can be proto-bound too (not just gofunc) —
	// obj.first() returns a list whose position 0 is bound to a second
	// gotype, and at(listVar, 0) on *that* result (bound to its own
	// variable first — trackAtResult only recognizes a plain Ident as
	// at(...)'s first argument, matching weave_spec.md's own
	// `r = at(result, 0)` idiom) must resolve further `.Method()` calls
	// natively, exactly like a gofunc-returned value.
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: append(goReaderStmts(),
			&ast.AssignStmt{Name: "GoOuter", Value: goTypeDeclExpr("?pkg.Outer", []ast.ObjectField{
				{Name: "inner", Value: gomethodExpr("Inner", "GoReader")},
			})},
			&ast.AssignStmt{Name: "outer", Value: goFuncDeclExpr("?pkg.NewOuter", "GoOuter")},
			&ast.AssignStmt{Name: "o", Value: atExpr(&ast.CallExpr{Callee: &ast.Ident{Name: "outer"}}, 0)},
			&ast.AssignStmt{Name: "innerResult", Value: &ast.CallExpr{Callee: &ast.PropExpr{Obj: &ast.Ident{Name: "o"}, Prop: "inner"}}},
			&ast.AssignStmt{Name: "inner", Value: atExpr(&ast.Ident{Name: "innerResult"}, 0)},
			&ast.ExprStmt{X: &ast.CallExpr{Callee: &ast.PropExpr{Obj: &ast.Ident{Name: "inner"}, Prop: "len"}}},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		),
	}}
	if err := Check(file); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

func TestCheck_ReassignedVarLosesStaticGoType(t *testing.T) {
	// r starts Go-typed (via at(...) extraction), then gets reassigned to
	// an ordinary object — r.someProp() afterward must be treated as an
	// ordinary dynamic call, not a (now-stale) static Go method
	// resolution.
	body := append(goReaderStmts(),
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

// recoverCall builds a bare `recover(fn(_) {...})` call, standing in for
// whatever handler the test doesn't care about the contents of.
func recoverCall() *ast.CallExpr {
	return &ast.CallExpr{
		Callee: &ast.Ident{Name: "recover"},
		Args:   []ast.Expr{&ast.FuncLit{Param: "err", Body: nil}},
	}
}

func TestCheck_RecoverInsideObjectLiteralFieldIsAnError(t *testing.T) {
	// weave_spec.md §6.5/§20: a closure that's an object literal's own
	// field value could be dispatched as an actor message handler if the
	// object is later spawn(...)ed — recover(...) deliberately doesn't
	// support that, and this is caught at compile time (checker.
	// inHandlerLiteral's own doc comment).
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "proto", Value: &ast.ObjectLit{Fields: []ast.ObjectField{
				{Name: "boom", Value: &ast.FuncLit{
					Param: "self",
					Body:  []ast.Stmt{&ast.ExprStmt{X: recoverCall()}},
				}},
			}}},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: recover(...) inside an object literal field's own closure")
	}
}

func TestCheck_RecoverInsideNestedClosureInsideHandlerIsAnError(t *testing.T) {
	// The restriction propagates into closures lexically nested (however
	// deeply) inside a handler-shaped one, not just its own immediate
	// body — a nested closure called synchronously from within the
	// handler would shield the actor from a crash just as effectively
	// (checker.inHandlerLiteral's own doc comment).
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "proto", Value: &ast.ObjectLit{Fields: []ast.ObjectField{
				{Name: "boom", Value: &ast.FuncLit{
					Param: "self",
					Body: []ast.Stmt{
						&ast.AssignStmt{Name: "inner", Value: &ast.FuncLit{
							Param: "y",
							Body:  []ast.Stmt{&ast.ExprStmt{X: recoverCall()}},
						}},
					},
				}},
			}}},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: recover(...) inside a closure nested inside a handler-shaped one")
	}
}

func TestCheck_RecoverInsideOrdinaryClosureIsValid(t *testing.T) {
	// A closure that's never placed as an object literal field (an
	// ordinary named helper, here) is unaffected by the handler-shaped
	// restriction — it can never be reached by actor message dispatch
	// (weave_spec.md §6.2's ObjGet-based lookup only ever finds object
	// properties), so recover(...) is allowed freely inside it, and
	// sibling code after it (main's own trailing return) is unaffected
	// too — checker.inHandlerLiteral must not leak past where it was set.
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "helper", Value: &ast.FuncLit{
				Param: "x",
				Body:  []ast.Stmt{&ast.ExprStmt{X: recoverCall()}},
			}},
			&ast.ExprStmt{X: recoverCall()},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err != nil {
		t.Fatalf("Check: %v (recover(...) should be allowed outside any handler-shaped closure)", err)
	}
}
