package parser

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/amisonnet8/weave/internal/ast"
)

// exprString renders e as a fully-parenthesized expression, for
// precedence tests: it makes grouping visible in a single assertion
// instead of walking the AST shape by hand.
func exprString(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.NumberLit:
		return strconv.FormatFloat(e.Value, 'g', -1, 64)
	case *ast.Ident:
		return e.Name
	case *ast.BoolLit:
		if e.Value {
			return "true"
		}
		return "false"
	case *ast.BinaryExpr:
		return "(" + exprString(e.X) + " " + e.Op + " " + exprString(e.Y) + ")"
	case *ast.UnaryExpr:
		return "(" + e.Op + exprString(e.X) + ")"
	default:
		return fmt.Sprintf("%#v", e)
	}
}

func parseExprForTest(t *testing.T, src string) ast.Expr {
	t.Helper()
	file, err := Parse("func main(): int {\n\treturn " + src + "\n}\n")
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	ret, ok := file.Main.Body[0].(*ast.ReturnStmt)
	if !ok {
		t.Fatalf("Parse(%q): body[0] = %T, want *ast.ReturnStmt", src, file.Main.Body[0])
	}
	return ret.Value
}

func TestParse_OperatorPrecedence(t *testing.T) {
	tests := []struct{ src, want string }{
		{"1 + 2 * 3", "(1 + (2 * 3))"},
		{"1 * 2 + 3", "((1 * 2) + 3)"},
		{"1 < 2 == true", "((1 < 2) == true)"},
		{"true || false && true", "(true || (false && true))"},
		{"!true && false", "((!true) && false)"},
		{"-1 + 2", "((-1) + 2)"},
		{"1 + 2 == 3 && 4 < 5", "(((1 + 2) == 3) && (4 < 5))"},
		{"(1 + 2) * 3", "((1 + 2) * 3)"},
		{"1 - 2 - 3", "((1 - 2) - 3)"}, // left-associative
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			got := exprString(parseExprForTest(t, tt.src))
			if got != tt.want {
				t.Errorf("parse(%q) = %s, want %s", tt.src, got, tt.want)
			}
		})
	}
}

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

func TestParse_AssignStmt(t *testing.T) {
	src := "func main(): int {\n\tx = 1\n\ty = \"hi\"\n\tz = true\n\tw = false\n\tn = nil\n\treturn 0\n}\n"
	file, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(file.Main.Body) != 6 {
		t.Fatalf("got %d statements, want 6: %+v", len(file.Main.Body), file.Main.Body)
	}

	assign, ok := file.Main.Body[0].(*ast.AssignStmt)
	if !ok || assign.Name != "x" {
		t.Fatalf("statement 0: got %#v, want AssignStmt(x)", file.Main.Body[0])
	}
	if _, ok := assign.Value.(*ast.NumberLit); !ok {
		t.Errorf("x's value: got %#v, want *ast.NumberLit", assign.Value)
	}

	z, ok := file.Main.Body[2].(*ast.AssignStmt)
	if !ok {
		t.Fatalf("statement 2: got %T, want *ast.AssignStmt", file.Main.Body[2])
	}
	zVal, ok := z.Value.(*ast.BoolLit)
	if !ok || zVal.Value != true {
		t.Errorf("z's value: got %#v, want BoolLit(true)", z.Value)
	}

	n, ok := file.Main.Body[4].(*ast.AssignStmt)
	if !ok {
		t.Fatalf("statement 4: got %T, want *ast.AssignStmt", file.Main.Body[4])
	}
	if _, ok := n.Value.(*ast.NilLit); !ok {
		t.Errorf("n's value: got %#v, want *ast.NilLit", n.Value)
	}
}

func TestParse_AssignToNonIdentIsAnError(t *testing.T) {
	if _, err := Parse("func main(): int {\n\tprint(x) = 1\n\treturn 0\n}\n"); err == nil {
		t.Fatal("expected an error assigning to a non-identifier")
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
