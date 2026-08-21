package parser

import (
	"testing"

	"github.com/amisonnet8/weave/internal/ast"
)

func TestParse_HelloWorld(t *testing.T) {
	src := "func main(): int {\n\tprint(\"Hello, Weave!\")\n\treturn 0\n}\n"
	file, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if file.Main == nil {
		t.Fatal("expected file.Main to be set")
	}
	if file.Main.Name != "main" || file.Main.ReturnType != "int" {
		t.Errorf("got Name=%q ReturnType=%q", file.Main.Name, file.Main.ReturnType)
	}
	if len(file.Main.Body) != 2 {
		t.Fatalf("got %d statements, want 2: %+v", len(file.Main.Body), file.Main.Body)
	}

	printCall, ok := file.Main.Body[0].(*ast.ExprStmt)
	if !ok {
		t.Fatalf("statement 0: got %T, want *ast.ExprStmt", file.Main.Body[0])
	}
	call, ok := printCall.X.(*ast.CallExpr)
	if !ok {
		t.Fatalf("statement 0 expr: got %T, want *ast.CallExpr", printCall.X)
	}
	callee, ok := call.Callee.(*ast.Ident)
	if !ok || callee.Name != "print" {
		t.Fatalf("call callee: got %#v, want Ident(print)", call.Callee)
	}
	if len(call.Args) != 1 {
		t.Fatalf("got %d args, want 1", len(call.Args))
	}
	arg, ok := call.Args[0].(*ast.StringLit)
	if !ok || arg.Value != "Hello, Weave!" {
		t.Fatalf("call arg: got %#v", call.Args[0])
	}

	ret, ok := file.Main.Body[1].(*ast.ReturnStmt)
	if !ok {
		t.Fatalf("statement 1: got %T, want *ast.ReturnStmt", file.Main.Body[1])
	}
	num, ok := ret.Value.(*ast.NumberLit)
	if !ok || num.Value != 0 {
		t.Fatalf("return value: got %#v", ret.Value)
	}
}

func TestParse_MissingReturnTypeIsAnError(t *testing.T) {
	if _, err := Parse("func main() {\n}\n"); err == nil {
		t.Fatal("expected an error for a missing `: <type>`")
	}
}

func TestParse_UnterminatedBlockIsAnError(t *testing.T) {
	if _, err := Parse("func main(): int {\n\treturn 0\n"); err == nil {
		t.Fatal("expected an error for an unterminated block")
	}
}
