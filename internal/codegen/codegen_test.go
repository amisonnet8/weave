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
		"\tCALL\t:\t?fmt.Println\t\"Hello, Weave!\"\n" +
		"\tRET\t0\n" +
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

func TestGenerate_StringLiteralIsGoQuoted(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", ReturnType: "int",
		Body: []ast.Stmt{
			&ast.ExprStmt{X: &ast.CallExpr{
				Callee: &ast.Ident{Name: "print"},
				Args:   []ast.Expr{&ast.StringLit{Value: `say "hi"` + "\t\\end"}},
			}},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	ir, err := Generate(file)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.Contains(ir, `"say \"hi\"\t\\end"`) {
		t.Errorf("expected a properly Go-escaped string literal, got:\n%s", ir)
	}
}

func TestGenerate_NonIntegerExitCodeIsAnError(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", ReturnType: "int",
		Body: []ast.Stmt{&ast.ReturnStmt{Value: &ast.NumberLit{Value: 1.5}}},
	}}
	if _, err := Generate(file); err == nil {
		t.Fatal("expected an error for a non-integer exit code")
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
