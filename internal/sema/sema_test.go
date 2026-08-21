package sema

import (
	"testing"

	"github.com/amisonnet8/weave/internal/ast"
)

func TestCheck_ValidMain(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{Name: "main", ReturnType: "int"}}
	if err := Check(file); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

func TestCheck_MissingMain(t *testing.T) {
	if err := Check(&ast.File{}); err == nil {
		t.Fatal("expected an error for a missing main")
	}
}

func TestCheck_WrongFuncName(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{Name: "notMain", ReturnType: "int"}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: `func` may only declare main")
	}
}

func TestCheck_WrongReturnType(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{Name: "main", ReturnType: "string"}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: main must return int")
	}
}

func TestCheck_AssignThenUseIsValid(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", ReturnType: "int",
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
		Name: "main", ReturnType: "int",
		Body: []ast.Stmt{&ast.ReturnStmt{Value: &ast.Ident{Name: "x"}}},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: x is used before assignment")
	}
}

func TestCheck_AssignToReservedNameIsAnError(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", ReturnType: "int",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "weave_main", Value: &ast.NumberLit{Value: 1}},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error assigning to the reserved name weave_main")
	}
}

func TestCheck_AssignToDoubleUnderscorePrefixIsAnError(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", ReturnType: "int",
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
		Name: "main", ReturnType: "int",
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
		Name: "main", ReturnType: "int",
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
		Name: "main", ReturnType: "int",
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
		Name: "main", ReturnType: "int",
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
		Name: "main", ReturnType: "int",
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
		Name: "main", ReturnType: "int",
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
		Name: "main", ReturnType: "int",
		Body: []ast.Stmt{&ast.BreakStmt{}, &ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}}},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: break outside of a loop")
	}
}

func TestCheck_ContinueOutsideLoopIsAnError(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", ReturnType: "int",
		Body: []ast.Stmt{&ast.ContinueStmt{}, &ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}}},
	}}
	if err := Check(file); err == nil {
		t.Fatal("expected an error: continue outside of a loop")
	}
}

func TestCheck_BreakInsideWhileIsValid(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", ReturnType: "int",
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
		Name: "main", ReturnType: "int",
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
		Name: "main", ReturnType: "int",
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
		Name: "main", ReturnType: "int",
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
		Name: "main", ReturnType: "int",
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
		Name: "main", ReturnType: "int",
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
		Name: "main", ReturnType: "int",
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
		Name: "main", ReturnType: "int",
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
		Name: "main", ReturnType: "int",
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
		Name: "main", ReturnType: "int",
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
		Name: "main", ReturnType: "int",
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
		Name: "main", ReturnType: "int",
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
