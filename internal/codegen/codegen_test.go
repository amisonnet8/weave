package codegen

import (
	"strings"
	"testing"

	"github.com/amisonnet8/weave/internal/ast"
)

func TestGenerate_HelloWorld(t *testing.T) {
	file := &ast.File{
		Main: &ast.FuncDecl{
			Name:       "main",
			ReturnType: "int",
			Body: []ast.Stmt{
				&ast.ExprStmt{X: &ast.CallExpr{
					Callee: &ast.Ident{Name: "print"},
					Args:   []ast.Expr{&ast.StringLit{Value: "Hello, Weave!"}},
				}},
				&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
			},
		},
	}

	ir, err := Generate(file)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	want := "FUNC\t!weave_main\t:\t^int\n" +
		"\tVAR\t%__exitcode\t^int\n" +
		"\tCALL\t:\t?weavert.Print\t\"Hello, Weave!\"\n" +
		"\tCALL\t%__exitcode\t:\t?weavert.ExitCode\t0.0\n" +
		"\tRET\t%__exitcode\n" +
		"ENDFUNC\n" +
		"FUNC\t!main\t:\n" +
		"\tVAR\t%exitcode\t^int\n" +
		"\tCALL\t%exitcode\t:\t!weave_main\n" +
		"\tCALL\t:\t?os.Exit\t%exitcode\n" +
		"\tRET\n" +
		"ENDFUNC\n"
	if ir != want {
		t.Errorf("Generate() =\n%s\nwant:\n%s", ir, want)
	}
}

func TestGenerate_VariablesAreHoistedAndSetInOrder(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", ReturnType: "int",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "x", Value: &ast.NumberLit{Value: 1}},
			&ast.AssignStmt{Name: "x", Value: &ast.NumberLit{Value: 2}}, // reassignment: no second VAR
			&ast.ExprStmt{X: &ast.CallExpr{
				Callee: &ast.Ident{Name: "print"},
				Args:   []ast.Expr{&ast.Ident{Name: "x"}},
			}},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	ir, err := Generate(file)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if strings.Count(ir, "VAR\t%x\t^any") != 1 {
		t.Errorf("expected exactly one VAR %%x, got:\n%s", ir)
	}
	if strings.Count(ir, "SET\t%x\t") != 2 {
		t.Errorf("expected two SETs of %%x, got:\n%s", ir)
	}
	if !strings.Contains(ir, "CALL\t:\t?weavert.Print\t%x\n") {
		t.Errorf("expected print(x) to pass the %%x variable token, got:\n%s", ir)
	}
}

func TestGenerate_LiteralKinds(t *testing.T) {
	tests := []struct {
		name string
		expr ast.Expr
		want string
	}{
		{"whole number gets a forced decimal point", &ast.NumberLit{Value: 1234}, "1234.0"},
		{"fractional number is untouched", &ast.NumberLit{Value: 1.5}, "1.5"},
		{"true", &ast.BoolLit{Value: true}, "true"},
		{"false", &ast.BoolLit{Value: false}, "false"},
		{"nil", &ast.NilLit{}, "nil"},
		{"string is Go-quoted", &ast.StringLit{Value: `a"b`}, `"a\"b"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := genValue(tt.expr)
			if err != nil {
				t.Fatalf("genValue: %v", err)
			}
			if got != tt.want {
				t.Errorf("genValue() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGenerate_ReturnRoutesThroughExitCode(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", ReturnType: "int",
		Body: []ast.Stmt{&ast.ReturnStmt{Value: &ast.Ident{Name: "x"}}},
	}}
	ir, err := Generate(file)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.Contains(ir, "CALL\t%__exitcode\t:\t?weavert.ExitCode\t%x\n") {
		t.Errorf("expected return to convert via weavert.ExitCode, got:\n%s", ir)
	}
}

func TestGenerate_MissingReturnIsAnError(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", ReturnType: "int",
		Body: []ast.Stmt{&ast.ReturnStmt{}},
	}}
	if _, err := Generate(file); err == nil {
		t.Fatal("expected an error for a bare `return` in main")
	}
}
