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

func TestParse_IfElifElse(t *testing.T) {
	src := "func main(): int {\n" +
		"\tif x == 100 {\n\t\ty = 100\n" +
		"\t} elif x == 200 {\n\t\tz = 200\n" +
		"\t} else {\n\t\tx = x + 1\n\t}\n" +
		"\treturn 0\n}\n"
	file, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	ifStmt, ok := file.Main.Body[0].(*ast.IfStmt)
	if !ok {
		t.Fatalf("statement 0: got %T, want *ast.IfStmt", file.Main.Body[0])
	}
	if len(ifStmt.Clauses) != 2 {
		t.Fatalf("got %d clauses (if+elif), want 2", len(ifStmt.Clauses))
	}
	if len(ifStmt.Clauses[0].Body) != 1 || len(ifStmt.Clauses[1].Body) != 1 {
		t.Errorf("expected one statement per clause body, got %#v", ifStmt.Clauses)
	}
	if ifStmt.Else == nil || len(ifStmt.Else) != 1 {
		t.Errorf("expected a one-statement else body, got %#v", ifStmt.Else)
	}
}

func TestParse_IfWithoutElifOrElse(t *testing.T) {
	file, err := Parse("func main(): int {\n\tif true {\n\t\tx = 1\n\t}\n\treturn 0\n}\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	ifStmt, ok := file.Main.Body[0].(*ast.IfStmt)
	if !ok {
		t.Fatalf("statement 0: got %T, want *ast.IfStmt", file.Main.Body[0])
	}
	if len(ifStmt.Clauses) != 1 {
		t.Errorf("got %d clauses, want 1 (just the if)", len(ifStmt.Clauses))
	}
	if ifStmt.Else != nil {
		t.Errorf("expected no else body, got %#v", ifStmt.Else)
	}
}

func TestParse_While(t *testing.T) {
	file, err := Parse("func main(): int {\n\twhile i < 10 {\n\t\ti = i + 1\n\t}\n\treturn 0\n}\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	w, ok := file.Main.Body[0].(*ast.WhileStmt)
	if !ok {
		t.Fatalf("statement 0: got %T, want *ast.WhileStmt", file.Main.Body[0])
	}
	if exprString(w.Cond) != "(i < 10)" {
		t.Errorf("cond = %s, want (i < 10)", exprString(w.Cond))
	}
	if len(w.Body) != 1 {
		t.Errorf("got %d body statements, want 1", len(w.Body))
	}
}

func TestParse_BreakAndContinue(t *testing.T) {
	src := "func main(): int {\n\twhile true {\n\t\tbreak\n\t\tcontinue\n\t}\n\treturn 0\n}\n"
	file, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	w := file.Main.Body[0].(*ast.WhileStmt)
	if _, ok := w.Body[0].(*ast.BreakStmt); !ok {
		t.Errorf("body[0] = %T, want *ast.BreakStmt", w.Body[0])
	}
	if _, ok := w.Body[1].(*ast.ContinueStmt); !ok {
		t.Errorf("body[1] = %T, want *ast.ContinueStmt", w.Body[1])
	}
}

func TestParse_FuncLitSingleParam(t *testing.T) {
	lit := parseExprForTest(t, "fn(a) { return a }")
	f, ok := lit.(*ast.FuncLit)
	if !ok {
		t.Fatalf("got %T, want *ast.FuncLit", lit)
	}
	if f.Param != "a" {
		t.Errorf("Param = %q, want a", f.Param)
	}
	if len(f.Body) != 1 {
		t.Errorf("got %d body statements, want 1", len(f.Body))
	}
}

// TestParse_MultiParamSugarMatchesChainedSugar verifies weave_spec.md
// §5's own claim: `fn(a, b) {...}` and `fn(a) fn(b) {...}` are the same
// thing. The parser folds both into an identical nested single-Param
// AST (ast.FuncLit's doc comment), so this compares their exprString
// forms directly.
func TestParse_MultiParamSugarMatchesChainedSugar(t *testing.T) {
	multi := parseExprForTest(t, "fn(a, b) { return a }")
	chained := parseExprForTest(t, "fn(a) fn(b) { return a }")

	multiLit, ok := multi.(*ast.FuncLit)
	if !ok {
		t.Fatalf("multi: got %T, want *ast.FuncLit", multi)
	}
	chainedLit, ok := chained.(*ast.FuncLit)
	if !ok {
		t.Fatalf("chained: got %T, want *ast.FuncLit", chained)
	}
	if multiLit.Param != "a" || chainedLit.Param != "a" {
		t.Fatalf("outer Param: multi=%q chained=%q, want a/a", multiLit.Param, chainedLit.Param)
	}
	innerMulti, ok := multiLit.Body[0].(*ast.ReturnStmt).Value.(*ast.FuncLit)
	if !ok {
		t.Fatalf("multi body[0] value: got %T, want *ast.FuncLit", multiLit.Body[0])
	}
	innerChained, ok := chainedLit.Body[0].(*ast.ReturnStmt).Value.(*ast.FuncLit)
	if !ok {
		t.Fatalf("chained body[0] value: got %T, want *ast.FuncLit", chainedLit.Body[0])
	}
	if innerMulti.Param != "b" || innerChained.Param != "b" {
		t.Fatalf("inner Param: multi=%q chained=%q, want b/b", innerMulti.Param, innerChained.Param)
	}
}

func TestParse_FuncLitNoParamsIsAnError(t *testing.T) {
	if _, err := Parse("func main(): int {\n\tf = fn() { return 1 }\n\treturn 0\n}\n"); err == nil {
		t.Fatal("expected an error: fn(...) requires at least one parameter")
	}
}

func TestParse_ImmediatelyInvokedFuncLit(t *testing.T) {
	call, ok := parseExprForTest(t, "fn(a) { return a }(5)").(*ast.CallExpr)
	if !ok {
		t.Fatalf("got %T, want *ast.CallExpr", call)
	}
	if _, ok := call.Callee.(*ast.FuncLit); !ok {
		t.Errorf("callee: got %T, want *ast.FuncLit", call.Callee)
	}
	if len(call.Args) != 1 {
		t.Errorf("got %d args, want 1", len(call.Args))
	}
}

func TestParse_CurryCallSugar(t *testing.T) {
	// f(a, b, c) parses as ONE CallExpr with 3 args (codegen expands the
	// curry application; see internal/codegen's genGeneralCall).
	call, ok := parseExprForTest(t, "f(1, 2, 3)").(*ast.CallExpr)
	if !ok {
		t.Fatalf("got %T, want *ast.CallExpr", call)
	}
	if len(call.Args) != 3 {
		t.Errorf("got %d args, want 3", len(call.Args))
	}

	// f(1)(2)(3) parses as nested single-arg CallExprs instead.
	chained, ok := parseExprForTest(t, "f(1)(2)(3)").(*ast.CallExpr)
	if !ok {
		t.Fatalf("got %T, want *ast.CallExpr", chained)
	}
	if len(chained.Args) != 1 {
		t.Errorf("outer call: got %d args, want 1", len(chained.Args))
	}
	if _, ok := chained.Callee.(*ast.CallExpr); !ok {
		t.Errorf("outer callee: got %T, want *ast.CallExpr", chained.Callee)
	}
}

func TestParse_ObjectLit(t *testing.T) {
	lit, ok := parseExprForTest(t, "{ x: 1, y: 2 }").(*ast.ObjectLit)
	if !ok {
		t.Fatalf("got %T, want *ast.ObjectLit", lit)
	}
	if len(lit.Fields) != 2 || lit.Fields[0].Name != "x" || lit.Fields[1].Name != "y" {
		t.Errorf("Fields = %+v, want [x y]", lit.Fields)
	}
}

func TestParse_EmptyObjectLit(t *testing.T) {
	lit, ok := parseExprForTest(t, "{}").(*ast.ObjectLit)
	if !ok {
		t.Fatalf("got %T, want *ast.ObjectLit", lit)
	}
	if len(lit.Fields) != 0 {
		t.Errorf("got %d fields, want 0", len(lit.Fields))
	}
}

func TestParse_ObjectLitTrailingComma(t *testing.T) {
	lit, ok := parseExprForTest(t, "{ x: 1, y: 2, }").(*ast.ObjectLit)
	if !ok {
		t.Fatalf("got %T, want *ast.ObjectLit", lit)
	}
	if len(lit.Fields) != 2 {
		t.Errorf("got %d fields, want 2", len(lit.Fields))
	}
}

func TestParse_PropExpr(t *testing.T) {
	prop, ok := parseExprForTest(t, "point.x").(*ast.PropExpr)
	if !ok {
		t.Fatalf("got %T, want *ast.PropExpr", prop)
	}
	if prop.Prop != "x" {
		t.Errorf("Prop = %q, want x", prop.Prop)
	}
	obj, ok := prop.Obj.(*ast.Ident)
	if !ok || obj.Name != "point" {
		t.Errorf("Obj = %#v, want Ident(point)", prop.Obj)
	}
}

func TestParse_ChainedPropExpr(t *testing.T) {
	prop, ok := parseExprForTest(t, "a.b.c").(*ast.PropExpr)
	if !ok {
		t.Fatalf("got %T, want *ast.PropExpr", prop)
	}
	if prop.Prop != "c" {
		t.Errorf("outer Prop = %q, want c", prop.Prop)
	}
	inner, ok := prop.Obj.(*ast.PropExpr)
	if !ok || inner.Prop != "b" {
		t.Errorf("inner = %#v, want PropExpr(b)", prop.Obj)
	}
}

func TestParse_PropAssignStmt(t *testing.T) {
	file, err := Parse("func main(): int {\n\tpoint.x = 10\n\treturn 0\n}\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	assign, ok := file.Main.Body[0].(*ast.PropAssignStmt)
	if !ok {
		t.Fatalf("statement 0: got %T, want *ast.PropAssignStmt", file.Main.Body[0])
	}
	if assign.Prop != "x" {
		t.Errorf("Prop = %q, want x", assign.Prop)
	}
	obj, ok := assign.Obj.(*ast.Ident)
	if !ok || obj.Name != "point" {
		t.Errorf("Obj = %#v, want Ident(point)", assign.Obj)
	}
}

func TestParse_MethodCallOnPropExpr(t *testing.T) {
	// obj.greet("x") — a call whose callee is a property access, not a
	// plain identifier (general-call/builtin dispatch is codegen's job,
	// not the parser's — see codegen.genCallExpr).
	call, ok := parseExprForTest(t, `obj.greet("x")`).(*ast.CallExpr)
	if !ok {
		t.Fatalf("got %T, want *ast.CallExpr", call)
	}
	if _, ok := call.Callee.(*ast.PropExpr); !ok {
		t.Errorf("callee: got %T, want *ast.PropExpr", call.Callee)
	}
}

func TestParse_ForIn(t *testing.T) {
	file, err := Parse("func main(): int {\n\tfor k, v in obj {\n\t\tprint(k)\n\t}\n\treturn 0\n}\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	f, ok := file.Main.Body[0].(*ast.ForStmt)
	if !ok {
		t.Fatalf("statement 0: got %T, want *ast.ForStmt", file.Main.Body[0])
	}
	if f.Key != "k" || f.Value != "v" {
		t.Errorf("Key=%q Value=%q, want k/v", f.Key, f.Value)
	}
	obj, ok := f.Obj.(*ast.Ident)
	if !ok || obj.Name != "obj" {
		t.Errorf("Obj = %#v, want Ident(obj)", f.Obj)
	}
	if len(f.Body) != 1 {
		t.Errorf("got %d body statements, want 1", len(f.Body))
	}
}

func TestParse_ForInMissingCommaIsAnError(t *testing.T) {
	if _, err := Parse("func main(): int {\n\tfor k v in obj {\n\t}\n\treturn 0\n}\n"); err == nil {
		t.Fatal("expected an error for a missing ',' between k and v")
	}
}

func TestParse_TopLevelStatementsBeforeMain(t *testing.T) {
	file, err := Parse("proto = { x: 1 }\nfunc main(): int {\n\treturn 0\n}\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(file.TopLevel) != 1 {
		t.Fatalf("got %d top-level statements, want 1", len(file.TopLevel))
	}
	a, ok := file.TopLevel[0].(*ast.AssignStmt)
	if !ok || a.Name != "proto" {
		t.Fatalf("TopLevel[0] = %#v, want AssignStmt(proto)", file.TopLevel[0])
	}
	if file.Main == nil || len(file.Main.Body) != 1 {
		t.Fatalf("Main = %#v, want a 1-statement body", file.Main)
	}
}

func TestParse_TopLevelStatementsAfterMain(t *testing.T) {
	file, err := Parse("func main(): int {\n\treturn 0\n}\nunused = 2\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(file.TopLevel) != 1 {
		t.Fatalf("got %d top-level statements, want 1", len(file.TopLevel))
	}
}

func TestParse_TwoFuncMainIsAnError(t *testing.T) {
	if _, err := Parse("func main(): int {\n\treturn 0\n}\nfunc main(): int {\n\treturn 1\n}\n"); err == nil {
		t.Fatal("expected an error for a second `func main`")
	}
}

func TestParse_MissingMainIsNotAParseError(t *testing.T) {
	// A single file legitimately has no `func main` — it may be a
	// package member file (weave_spec.md §17.1). Requiring at least one
	// `func main` across a whole package is modloader.Load's job, not
	// Parse's — see TestLoad_MissingMainIsAnError in internal/modloader.
	file, err := Parse("x = 1\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if file.Main != nil {
		t.Errorf("Main = %#v, want nil", file.Main)
	}
}

func TestParse_ImportStmt(t *testing.T) {
	file, err := Parse("import mathutil \"./mathutil\"\nfunc main(): int {\n\treturn 0\n}\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(file.Imports) != 1 {
		t.Fatalf("got %d imports, want 1", len(file.Imports))
	}
	imp := file.Imports[0]
	if imp.Qualifier != "mathutil" || imp.Path != "./mathutil" {
		t.Errorf("Import = %#v, want Qualifier=mathutil Path=./mathutil", imp)
	}
}

func TestParse_MultipleImports(t *testing.T) {
	file, err := Parse("import a \"./a\"\nimport b \"./b\"\nfunc main(): int {\n\treturn 0\n}\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(file.Imports) != 2 {
		t.Fatalf("got %d imports, want 2", len(file.Imports))
	}
}

func TestParse_ImportAfterTopLevelStmtIsAnError(t *testing.T) {
	if _, err := Parse("x = 1\nimport a \"./a\"\nfunc main(): int {\n\treturn 0\n}\n"); err == nil {
		t.Fatal("expected an error for `import` after another top-level statement")
	}
}

func TestParse_ImportPathMustBeStringLiteral(t *testing.T) {
	if _, err := Parse("import a x\nfunc main(): int {\n\treturn 0\n}\n"); err == nil {
		t.Fatal("expected an error for a non-string-literal import path")
	}
}

func TestParse_PubTopLevelAssign(t *testing.T) {
	file, err := Parse("pub x = 1\nfunc main(): int {\n\treturn 0\n}\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(file.TopLevel) != 1 {
		t.Fatalf("got %d top-level statements, want 1", len(file.TopLevel))
	}
	a, ok := file.TopLevel[0].(*ast.AssignStmt)
	if !ok || !a.Pub || a.Name != "x" {
		t.Errorf("TopLevel[0] = %#v, want Pub AssignStmt(x)", file.TopLevel[0])
	}
}

func TestParse_PubWithoutAssignIsAnError(t *testing.T) {
	if _, err := Parse("pub print(1)\nfunc main(): int {\n\treturn 0\n}\n"); err == nil {
		t.Fatal("expected an error for `pub` prefixing a non-assignment statement")
	}
}

func TestParse_PubInsideMainIsAnError(t *testing.T) {
	if _, err := Parse("func main(): int {\n\tpub x = 1\n\treturn 0\n}\n"); err == nil {
		t.Fatal("expected an error for `pub` used inside func main")
	}
}

func TestParse_NonPubTopLevelAssignHasPubFalse(t *testing.T) {
	file, err := Parse("x = 1\nfunc main(): int {\n\treturn 0\n}\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	a, ok := file.TopLevel[0].(*ast.AssignStmt)
	if !ok || a.Pub {
		t.Errorf("TopLevel[0] = %#v, want non-Pub AssignStmt", file.TopLevel[0])
	}
}
