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
