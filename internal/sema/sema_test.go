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
