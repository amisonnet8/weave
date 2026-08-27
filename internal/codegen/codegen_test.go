package codegen

import (
	"strings"
	"testing"

	"github.com/amisonnet8/weave/internal/ast"
)

func TestGenerate_HelloWorld(t *testing.T) {
	file := &ast.File{
		Main: &ast.FuncDecl{
			Name:  "main",
			Param: "args",
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

	want := "FUNC\t!weave_main\t^any\t:\t^int\n" +
		"\tVAR\t%args\t^any\n" +
		"\tVAR\t%__exitcode\t^int\n" +
		"\tSET\t%args\t$1\n" +
		"\tCALL\t:\t?weavert.Print\t\"Hello, Weave!\"\n" +
		"\tCALL\t%__exitcode\t:\t?weavert.ExitCode\t0.0\n" +
		"\tRET\t%__exitcode\n" +
		"ENDFUNC\n" +
		"FUNC\t!main\t:\n" +
		"\tVAR\t%exitcode\t^int\n" +
		"\tVAR\t%args\t^any\n" +
		"\tCALL\t%args\t:\t?weavert.Args\n" +
		"\tCALL\t%exitcode\t:\t!weave_main\t%args\n" +
		"\tCALL\t:\t?os.Exit\t%exitcode\n" +
		"\tRET\n" +
		"ENDFUNC\n"
	if ir != want {
		t.Errorf("Generate() =\n%s\nwant:\n%s", ir, want)
	}
}

func TestGenerate_VariablesAreHoistedAndSetInOrder(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
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
			got, err := genExpr(newFuncGen(&codegenCtx{}), tt.expr)
			if err != nil {
				t.Fatalf("genExpr: %v", err)
			}
			if got != tt.want {
				t.Errorf("genExpr() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGenerate_BinaryExprEvaluatesOperandsThenCallsWeavert(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{&ast.ReturnStmt{Value: &ast.BinaryExpr{
			Op: "+",
			X:  &ast.NumberLit{Value: 1},
			Y:  &ast.NumberLit{Value: 2},
		}}},
	}}
	ir, err := Generate(file)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.Contains(ir, "VAR\t%__t0\t^any\n") {
		t.Errorf("expected a hoisted __t0 temp, got:\n%s", ir)
	}
	if !strings.Contains(ir, "CALL\t%__t0\t:\t?weavert.Add\t1.0\t2.0\n") {
		t.Errorf("expected the + to lower to weavert.Add, got:\n%s", ir)
	}
	if !strings.Contains(ir, "CALL\t%__exitcode\t:\t?weavert.ExitCode\t%__t0\n") {
		t.Errorf("expected return to consume the __t0 temp, got:\n%s", ir)
	}
}

func TestGenerate_NestedBinaryExprUsesDistinctTemps(t *testing.T) {
	// (1 + 2) * 3
	expr := &ast.BinaryExpr{
		Op: "*",
		X:  &ast.BinaryExpr{Op: "+", X: &ast.NumberLit{Value: 1}, Y: &ast.NumberLit{Value: 2}},
		Y:  &ast.NumberLit{Value: 3},
	}
	fg := newFuncGen(&codegenCtx{})
	got, err := genExpr(fg, expr)
	if err != nil {
		t.Fatalf("genExpr: %v", err)
	}
	if got != "%__t1" {
		t.Errorf("outer result token = %q, want %%__t1", got)
	}
	body := fg.body.String()
	if !strings.Contains(body, "CALL\t%__t0\t:\t?weavert.Add\t1.0\t2.0\n") {
		t.Errorf("expected inner + to use __t0, got:\n%s", body)
	}
	if !strings.Contains(body, "CALL\t%__t1\t:\t?weavert.Mul\t%__t0\t3.0\n") {
		t.Errorf("expected outer * to consume __t0, got:\n%s", body)
	}
}

func TestGenerate_UnaryMinusReusesSub(t *testing.T) {
	fg := newFuncGen(&codegenCtx{})
	got, err := genExpr(fg, &ast.UnaryExpr{Op: "-", X: &ast.Ident{Name: "x"}})
	if err != nil {
		t.Fatalf("genExpr: %v", err)
	}
	if got != "%__t0" {
		t.Errorf("result token = %q, want %%__t0", got)
	}
	if !strings.Contains(fg.body.String(), "CALL\t%__t0\t:\t?weavert.Sub\t0.0\t%x\n") {
		t.Errorf("expected unary - to reuse weavert.Sub, got:\n%s", fg.body.String())
	}
}

func TestGenerate_UnknownBinaryOperatorIsAnError(t *testing.T) {
	fg := newFuncGen(&codegenCtx{})
	_, err := genExpr(fg, &ast.BinaryExpr{Op: "^^", X: &ast.NumberLit{Value: 1}, Y: &ast.NumberLit{Value: 2}})
	if err == nil {
		t.Fatal("expected an error for an unknown binary operator")
	}
}

func TestGenerate_ReturnRoutesThroughExitCode(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
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
		Name: "main", Param: "args",
		Body: []ast.Stmt{&ast.ReturnStmt{}},
	}}
	if _, err := Generate(file); err == nil {
		t.Fatal("expected an error for a bare `return` in main")
	}
}

// blocksBalance is a light structural check for control-flow codegen:
// every IF/ENDIF and LOOP/ENDLOOP must nest correctly (a stack-based
// check — ELSE/ENDIF must close the innermost still-open IF, ENDLOOP
// must close the innermost still-open LOOP), and every BREAK/CONTINUE
// must sit inside at least one open LOOP — amivm's block-form control
// flow (amivm_spec.md §4.10-4.11) replaced the old GOTO/LABEL-based
// scheme this helper used to check.
func blocksBalance(t *testing.T, ir string) {
	t.Helper()
	var stack []string // "IF" or "LOOP", innermost last
	for _, line := range strings.Split(ir, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "IF":
			stack = append(stack, "IF")
		case "LOOP":
			stack = append(stack, "LOOP")
		case "ELSE":
			if len(stack) == 0 || stack[len(stack)-1] != "IF" {
				t.Fatalf("ELSE outside an open IF:\n%s", ir)
			}
		case "ENDIF":
			if len(stack) == 0 || stack[len(stack)-1] != "IF" {
				t.Fatalf("ENDIF without a matching open IF:\n%s", ir)
			}
			stack = stack[:len(stack)-1]
		case "ENDLOOP":
			if len(stack) == 0 || stack[len(stack)-1] != "LOOP" {
				t.Fatalf("ENDLOOP without a matching open LOOP:\n%s", ir)
			}
			stack = stack[:len(stack)-1]
		case "BREAK", "CONTINUE":
			inLoop := false
			for _, s := range stack {
				if s == "LOOP" {
					inLoop = true
				}
			}
			if !inLoop {
				t.Fatalf("%s outside any open LOOP:\n%s", fields[0], ir)
			}
		}
	}
	if len(stack) != 0 {
		t.Fatalf("unclosed block(s) %v:\n%s", stack, ir)
	}
}

func TestGenerate_IfElifElseBlocksBalance(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.IfStmt{
				Clauses: []ast.IfClause{
					{Cond: &ast.BinaryExpr{Op: "==", X: &ast.Ident{Name: "x"}, Y: &ast.NumberLit{Value: 100}},
						Body: []ast.Stmt{&ast.AssignStmt{Name: "y", Value: &ast.NumberLit{Value: 100}}}},
					{Cond: &ast.BinaryExpr{Op: "==", X: &ast.Ident{Name: "x"}, Y: &ast.NumberLit{Value: 200}},
						Body: []ast.Stmt{&ast.AssignStmt{Name: "z", Value: &ast.NumberLit{Value: 200}}}},
				},
				Else: []ast.Stmt{&ast.AssignStmt{Name: "x", Value: &ast.NumberLit{Value: 1}}},
			},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	// x is read (in the conditions) before any assignment in this
	// snippet, but sema (not codegen) is what enforces that — Generate
	// itself doesn't validate, so this is fine as a pure codegen shape test.
	ir, err := Generate(file)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	blocksBalance(t, ir)
	if strings.Count(ir, "IF\t") != 2 {
		t.Errorf("expected 2 IFs (one per if/elif condition), got:\n%s", ir)
	}
}

func TestGenerate_NestedWhileBreakContinueBlocksBalance(t *testing.T) {
	// while true { while true { break; continue } }
	// Native BREAK/CONTINUE always target the innermost enclosing LOOP
	// by construction (Go's own for-loop semantics) — there's no label
	// bookkeeping left to assert on directly; blocksBalance's own
	// "BREAK/CONTINUE must be inside an open LOOP" check plus the
	// exact-nesting-count checks below are what's left to verify.
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.WhileStmt{
				Cond: &ast.BoolLit{Value: true},
				Body: []ast.Stmt{&ast.WhileStmt{
					Cond: &ast.BoolLit{Value: true},
					Body: []ast.Stmt{&ast.BreakStmt{}, &ast.ContinueStmt{}},
				}},
			},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	ir, err := Generate(file)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	blocksBalance(t, ir)
	if strings.Count(ir, "\tLOOP\n") != 2 || strings.Count(ir, "\tENDLOOP\n") != 2 {
		t.Errorf("expected 2 nested LOOP/ENDLOOP pairs, got:\n%s", ir)
	}
	// 3 BREAKs total: one per while's own genBreakUnless (loop-exit
	// check) plus the user's explicit `break` statement.
	if strings.Count(ir, "\tBREAK\n") != 3 {
		t.Errorf("expected exactly 3 BREAKs (2 loop-exit checks + 1 user break), got:\n%s", ir)
	}
	if strings.Count(ir, "\tCONTINUE\n") != 1 {
		t.Errorf("expected exactly one CONTINUE, got:\n%s", ir)
	}
}

func TestGenerate_ShortCircuitAndSkipsRHSOnFalse(t *testing.T) {
	fg := newFuncGen(&codegenCtx{})
	got, err := genExpr(fg, &ast.BinaryExpr{Op: "&&", X: &ast.Ident{Name: "a"}, Y: &ast.Ident{Name: "b"}})
	if err != nil {
		t.Fatalf("genExpr: %v", err)
	}
	body := fg.body.String()
	if !strings.Contains(body, "CALL\t"+got+"\t:\t?weavert.CheckBool\t%a\n") {
		t.Errorf("expected the left operand to be checked first, got:\n%s", body)
	}
	if !strings.Contains(body, "CALL\t"+got+"\t:\t?weavert.CheckBool\t%b\n") {
		t.Errorf("expected the right operand to be checked (in the true branch), got:\n%s", body)
	}
	blocksBalance(t, "FUNC\t!f\t:\n"+body+"ENDFUNC\n")
}

func TestGenerate_ShortCircuitOrSkipsRHSOnTrue(t *testing.T) {
	fg := newFuncGen(&codegenCtx{})
	_, err := genExpr(fg, &ast.BinaryExpr{Op: "||", X: &ast.Ident{Name: "a"}, Y: &ast.Ident{Name: "b"}})
	if err != nil {
		t.Fatalf("genExpr: %v", err)
	}
	blocksBalance(t, "FUNC\t!f\t:\n"+fg.body.String()+"ENDFUNC\n")
}

func TestGenerate_ConditionRoutesThroughCheckBool(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.WhileStmt{Cond: &ast.Ident{Name: "cond"}, Body: nil},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	ir, err := Generate(file)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.Contains(ir, "?weavert.CheckBool\t%cond\n") {
		t.Errorf("expected the while condition to route through weavert.CheckBool, got:\n%s", ir)
	}
	if !strings.Contains(ir, "VAR\t%__t0\t^bool\n") {
		t.Errorf("expected the condition temp to be declared ^bool, got:\n%s", ir)
	}
}

// genFuncLit now compiles every function literal to a native, inline
// CLOS block (never an independent top-level FUNC) — free variables need
// no explicit capture step at all, since the generated Go func literal
// is lexically nested and captures enclosing %-variables through Go's
// own closure semantics (see closure.go's doc comment). The freeVars
// analysis this used to require (env-slice construction) no longer
// exists; these tests replace the old TestFreeVars_*/
// TestGenerate_FuncLitEmitsClosureFuncAndSLTYPE/
// TestGenerate_NoClosuresMeansNoSLTYPE suite.

func TestGenerate_FuncLitEmitsNestedCLOS(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "base", Value: &ast.NumberLit{Value: 100}},
			&ast.AssignStmt{Name: "f", Value: &ast.FuncLit{
				Param: "x",
				Body: []ast.Stmt{&ast.ReturnStmt{Value: &ast.BinaryExpr{
					Op: "+", X: &ast.Ident{Name: "x"}, Y: &ast.Ident{Name: "base"},
				}}},
			}},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	ir, err := Generate(file)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.Contains(ir, "CLOS\t%__t") {
		t.Errorf("expected an inline CLOS assigning into a temp, got:\n%s", ir)
	}
	if !strings.Contains(ir, "ENDCLOS\n") {
		t.Errorf("expected a matching ENDCLOS, got:\n%s", ir)
	}
	if !strings.Contains(ir, "SET\t%c1_x\t&1\n") {
		t.Errorf("expected the closure's own param bound from &1 into a prefixed local, got:\n%s", ir)
	}
	// base is captured by plain reference — no AGET/env machinery, just
	// the same %base token weave_main itself declared.
	if !strings.Contains(ir, "?weavert.Add\t%c1_x\t%base\n") {
		t.Errorf("expected the closure body to reference the enclosing %%base directly, got:\n%s", ir)
	}
	if strings.Contains(ir, "AGET") || strings.Contains(ir, "SLMAKE") || strings.Contains(ir, "NewClosure") {
		t.Errorf("native CLOS needs no env-slice machinery at all, got:\n%s", ir)
	}
	// A closure's own `return` must NOT go through weavert.ExitCode —
	// that's exclusively main's bridge (see genReturnStmt's doc
	// comment on the Step 5 bug this guards against).
	closStart := strings.Index(ir, "CLOS\t%__t")
	closEnd := strings.Index(ir[closStart:], "ENDCLOS") + closStart
	closBody := ir[closStart:closEnd]
	if strings.Contains(closBody, "weavert.ExitCode") {
		t.Errorf("closure body must not call weavert.ExitCode, got:\n%s", closBody)
	}
	if !strings.Contains(closBody, "RET\t%c1___t") {
		t.Errorf("expected the closure to RET its own prefixed ^any temp, got:\n%s", closBody)
	}
}

func TestGenerate_NestedCurryProducesTwoLevelsOfCLOS(t *testing.T) {
	// fn(a) fn(b) { return a + b } — the parser already flattens this
	// into nested single-Param FuncLits; codegen must emit CLOS-in-CLOS.
	inner := &ast.FuncLit{Param: "b", Body: []ast.Stmt{
		&ast.ReturnStmt{Value: &ast.BinaryExpr{Op: "+", X: &ast.Ident{Name: "a"}, Y: &ast.Ident{Name: "b"}}},
	}}
	outer := &ast.FuncLit{Param: "a", Body: []ast.Stmt{&ast.ReturnStmt{Value: inner}}}
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "add", Value: outer},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	ir, err := Generate(file)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if strings.Count(ir, "CLOS\t") != 2 || strings.Count(ir, "ENDCLOS\n") != 2 {
		t.Fatalf("expected exactly 2 nested CLOS/ENDCLOS pairs, got:\n%s", ir)
	}
	// The inner closure's body must reference the outer's own bound
	// parameter (a) by its outer-level prefixed token, proving capture
	// crossed the CLOS boundary via funcGen.resolve rather than
	// resolving to some local of its own.
	if !strings.Contains(ir, "?weavert.Add\t%c1_a\t%c2_b\n") {
		t.Errorf("expected the inner body to add the outer's %%c1_a to its own %%c2_b, got:\n%s", ir)
	}
}

func TestGenerate_AssignInsideClosureReassignsOuterBinding(t *testing.T) {
	// n = 1; f = fn(x) { n = n + x }; — the closure body's `n = ...`
	// must reassign weave_main's own %n (weave_spec.md §10's "参照で
	// 捕捉する"/"代入は既存を再代入優先"), not declare a closure-local
	// shadow copy — the entire point of switching to native CLOS capture
	// (see funcGen's and genAssignStmt's doc comments).
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "n", Value: &ast.NumberLit{Value: 1}},
			&ast.AssignStmt{Name: "f", Value: &ast.FuncLit{
				Param: "x",
				Body: []ast.Stmt{&ast.AssignStmt{Name: "n", Value: &ast.BinaryExpr{
					Op: "+", X: &ast.Ident{Name: "n"}, Y: &ast.Ident{Name: "x"},
				}}},
			}},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	ir, err := Generate(file)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if strings.Contains(ir, "VAR\t%c1_n\t") {
		t.Errorf("closure must not declare its own local shadow of n, got:\n%s", ir)
	}
	if strings.Count(ir, "VAR\t%n\t^any\n") != 1 {
		t.Errorf("expected exactly one VAR %%n (weave_main's own), got:\n%s", ir)
	}
	if !strings.Contains(ir, "?weavert.Add\t%n\t%c1_x\n") {
		t.Errorf("expected the closure body to read weave_main's own %%n, got:\n%s", ir)
	}
	if !strings.Contains(ir, "SET\t%n\t%c1___t0\n") {
		t.Errorf("expected the closure body to write back into weave_main's own %%n, got:\n%s", ir)
	}
}

func TestGenerate_MultiArgCallCurriesThroughWeavertCall(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{&ast.ReturnStmt{Value: &ast.CallExpr{
			Callee: &ast.Ident{Name: "f"},
			Args:   []ast.Expr{&ast.NumberLit{Value: 1}, &ast.NumberLit{Value: 2}},
		}}},
	}}
	ir, err := Generate(file)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if strings.Count(ir, "?weavert.Call\t") != 2 {
		t.Errorf("expected two weavert.Call invocations (one per curried argument), got:\n%s", ir)
	}
	if !strings.Contains(ir, "?weavert.Call\t%f\t1.0\n") {
		t.Errorf("expected the first application to apply f to 1.0, got:\n%s", ir)
	}
}

func TestGenerate_ZeroArgGeneralCallDesugarsToNil(t *testing.T) {
	// weave_spec.md §5: f() desugars to f(nil) — an ordinary Weave
	// function value called with no explicit argument still gets a
	// value applied underneath (every Weave function takes exactly one
	// argument).
	fg := newFuncGen(&codegenCtx{})
	fg.declare("f", "^any")
	result, err := genGeneralCall(fg, &ast.CallExpr{Callee: &ast.Ident{Name: "f"}})
	if err != nil {
		t.Fatalf("genGeneralCall: %v", err)
	}
	if !strings.Contains(fg.body.String(), "?weavert.Call\t%f\tnil\n") {
		t.Errorf("expected a CALL applying %%f to a literal nil, got:\n%s", fg.body.String())
	}
	if result == "" {
		t.Error("expected a non-empty result token")
	}
}

func TestGenerate_ZeroArgMethodCallStaysSelfOnly(t *testing.T) {
	// obj.method() must NOT gain a synthetic nil on top of self — the
	// desugar in genGeneralCall only ever reaches the "ordinary function
	// value" branch, which method-sugar calls (handled earlier, via
	// genMethodCall) never fall through to.
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "obj", Value: &ast.ObjectLit{Fields: []ast.ObjectField{
				{Name: "greet", Value: &ast.FuncLit{Param: "self", Body: []ast.Stmt{&ast.ReturnStmt{Value: &ast.NumberLit{Value: 1}}}}},
			}}},
			&ast.ExprStmt{X: &ast.CallExpr{Callee: &ast.PropExpr{Obj: &ast.Ident{Name: "obj"}, Prop: "greet"}}},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	ir, err := Generate(file)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.Contains(ir, "?weavert.ObjGet\t%obj\t\"greet\"\n") {
		t.Errorf("expected the usual ObjGet-based method dispatch, got:\n%s", ir)
	}
	// Exactly one weavert.Call applying the found method to self — a
	// second one chaining a synthetic nil on top would mean the general-
	// call desugar leaked into method-sugar dispatch.
	if n := strings.Count(ir, "?weavert.Call\t"); n != 1 {
		t.Errorf("expected exactly 1 weavert.Call (self only), got %d:\n%s", n, ir)
	}
}

func TestGenerate_ObjectLitBuildsViaNewObjectAndObjSet(t *testing.T) {
	fg := newFuncGen(&codegenCtx{})
	got, err := genExpr(fg, &ast.ObjectLit{Fields: []ast.ObjectField{
		{Name: "x", Value: &ast.NumberLit{Value: 1}},
		{Name: "y", Value: &ast.NumberLit{Value: 2}},
	}})
	if err != nil {
		t.Fatalf("genExpr: %v", err)
	}
	body := fg.body.String()
	if !strings.Contains(body, "CALL\t"+got+"\t:\t?weavert.NewObject\n") {
		t.Errorf("expected NewObject into %s, got:\n%s", got, body)
	}
	if !strings.Contains(body, "CALL\t:\t?weavert.ObjSet\t"+got+"\t\"x\"\t1.0\n") {
		t.Errorf("expected ObjSet for field x, got:\n%s", body)
	}
	if !strings.Contains(body, "CALL\t:\t?weavert.ObjSet\t"+got+"\t\"y\"\t2.0\n") {
		t.Errorf("expected ObjSet for field y, got:\n%s", body)
	}
}

func TestGenerate_PropExprUsesObjGet(t *testing.T) {
	fg := newFuncGen(&codegenCtx{})
	got, err := genExpr(fg, &ast.PropExpr{Obj: &ast.Ident{Name: "obj"}, Prop: "x"})
	if err != nil {
		t.Fatalf("genExpr: %v", err)
	}
	if !strings.Contains(fg.body.String(), "CALL\t"+got+"\t:\t?weavert.ObjGet\t%obj\t\"x\"\n") {
		t.Errorf("expected ObjGet, got:\n%s", fg.body.String())
	}
}

func TestGenerate_PropAssignUsesObjSet(t *testing.T) {
	fg := newFuncGen(&codegenCtx{})
	err := genPropAssignStmt(fg, &ast.PropAssignStmt{
		Obj: &ast.Ident{Name: "obj"}, Prop: "x", Value: &ast.NumberLit{Value: 5},
	})
	if err != nil {
		t.Fatalf("genPropAssignStmt: %v", err)
	}
	if !strings.Contains(fg.body.String(), "CALL\t:\t?weavert.ObjSet\t%obj\t\"x\"\t5.0\n") {
		t.Errorf("expected ObjSet, got:\n%s", fg.body.String())
	}
}

func TestGenerate_IndexExprUsesObjAt(t *testing.T) {
	// list[i] must emit the exact same IR as at(list, i) — see
	// ast.IndexExpr's own doc comment.
	fg := newFuncGen(&codegenCtx{})
	got, err := genExpr(fg, &ast.IndexExpr{X: &ast.Ident{Name: "nums"}, Index: &ast.NumberLit{Value: 1}})
	if err != nil {
		t.Fatalf("genExpr: %v", err)
	}
	if !strings.Contains(fg.body.String(), "CALL\t"+got+"\t:\t?weavert.ObjAt\t%nums\t1.0\n") {
		t.Errorf("expected ObjAt, got:\n%s", fg.body.String())
	}
}

func TestGenerate_IndexAssignUsesObjSetAt(t *testing.T) {
	fg := newFuncGen(&codegenCtx{})
	err := genIndexAssignStmt(fg, &ast.IndexAssignStmt{
		Obj: &ast.Ident{Name: "nums"}, Index: &ast.NumberLit{Value: 1}, Value: &ast.NumberLit{Value: 99},
	})
	if err != nil {
		t.Fatalf("genIndexAssignStmt: %v", err)
	}
	if !strings.Contains(fg.body.String(), "CALL\t:\t?weavert.ObjSetAt\t%nums\t1.0\t99.0\n") {
		t.Errorf("expected ObjSetAt, got:\n%s", fg.body.String())
	}
}

func TestGenerate_HasAndRemoveBuiltins(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
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
	ir, err := Generate(file)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.Contains(ir, "?weavert.ObjHas\t%o\t\"x\"\n") {
		t.Errorf("expected has(...) to lower to weavert.ObjHas, got:\n%s", ir)
	}
	if !strings.Contains(ir, "?weavert.ObjRemove\t%o\t\"x\"\n") {
		t.Errorf("expected remove(...) to lower to weavert.ObjRemove, got:\n%s", ir)
	}
}

func TestGenerate_MethodCallInjectsSelfFirst(t *testing.T) {
	// alice.greet(1) -> ObjGet(alice, "greet") looked up once, then
	// Call'd with alice first, then 1 (weave_spec.md §9).
	fg := newFuncGen(&codegenCtx{})
	_, err := genGeneralCall(fg, &ast.CallExpr{
		Callee: &ast.PropExpr{Obj: &ast.Ident{Name: "alice"}, Prop: "greet"},
		Args:   []ast.Expr{&ast.NumberLit{Value: 1}},
	})
	if err != nil {
		t.Fatalf("genGeneralCall: %v", err)
	}
	body := fg.body.String()
	if !strings.Contains(body, "?weavert.ObjGet\t%alice\t\"greet\"\n") {
		t.Errorf("expected a single ObjGet for the method lookup, got:\n%s", body)
	}
	if strings.Count(body, "?weavert.ObjGet") != 1 {
		t.Errorf("expected obj to be evaluated exactly once, got:\n%s", body)
	}
	if strings.Count(body, "?weavert.Call") != 2 {
		t.Errorf("expected two Call applications (self, then the explicit arg), got:\n%s", body)
	}
	if !strings.Contains(body, "%alice\n") {
		t.Errorf("expected self (%%alice) to be applied, got:\n%s", body)
	}
	if !strings.Contains(body, "1.0\n") {
		t.Errorf("expected the explicit argument 1.0 to be applied, got:\n%s", body)
	}
}

func TestGenerate_MethodCallWithZeroArgsStillAppliesSelf(t *testing.T) {
	// alice.greet() -> greet(alice): self alone is a valid, complete
	// application even with no explicit args (weave_spec.md §9's own
	// `alice.greet()` example).
	fg := newFuncGen(&codegenCtx{})
	_, err := genGeneralCall(fg, &ast.CallExpr{
		Callee: &ast.PropExpr{Obj: &ast.Ident{Name: "alice"}, Prop: "greet"},
	})
	if err != nil {
		t.Fatalf("genGeneralCall: %v", err)
	}
	if strings.Count(fg.body.String(), "?weavert.Call\t") != 1 {
		t.Errorf("expected exactly one Call (self only), got:\n%s", fg.body.String())
	}
}

func TestGenerate_FuncLitAlwaysEndsWithRetNil(t *testing.T) {
	// weave_spec.md §6.2's own increment: fn(self, n) { self.count =
	// self.count + n } has no explicit return at all.
	fg := newFuncGen(&codegenCtx{})
	_, err := genFuncLit(fg, &ast.FuncLit{
		Param: "x",
		Body:  []ast.Stmt{&ast.AssignStmt{Name: "y", Value: &ast.Ident{Name: "x"}}},
	})
	if err != nil {
		t.Fatalf("genFuncLit: %v", err)
	}
	body := fg.body.String()
	if !strings.HasSuffix(strings.TrimSuffix(body, "\tENDCLOS\n"), "RET\tnil\n") {
		t.Errorf("expected the closure to end with an implicit RET nil, got:\n%s", body)
	}
}

func TestGenerate_ListCallBuildsNumericKeyedObject(t *testing.T) {
	fg := newFuncGen(&codegenCtx{})
	got, err := genListCall(fg, &ast.CallExpr{
		Args: []ast.Expr{&ast.NumberLit{Value: 10}, &ast.NumberLit{Value: 20}},
	})
	if err != nil {
		t.Fatalf("genListCall: %v", err)
	}
	body := fg.body.String()
	if !strings.Contains(body, "CALL\t"+got+"\t:\t?weavert.NewObject\n") {
		t.Errorf("expected NewObject, got:\n%s", body)
	}
	if !strings.Contains(body, "?weavert.ObjSet\t"+got+"\t\"0\"\t10.0\n") {
		t.Errorf("expected key \"0\" -> 10.0, got:\n%s", body)
	}
	if !strings.Contains(body, "?weavert.ObjSet\t"+got+"\t\"1\"\t20.0\n") {
		t.Errorf("expected key \"1\" -> 20.0, got:\n%s", body)
	}
}

func TestGenerate_LenAndStringBuiltins(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.ExprStmt{X: &ast.CallExpr{Callee: &ast.Ident{Name: "len"}, Args: []ast.Expr{&ast.StringLit{Value: "hi"}}}},
			&ast.ExprStmt{X: &ast.CallExpr{Callee: &ast.Ident{Name: "string"}, Args: []ast.Expr{&ast.NumberLit{Value: 1}}}},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	ir, err := Generate(file)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.Contains(ir, "?weavert.Len\t\"hi\"\n") {
		t.Errorf("expected len(...) to lower to weavert.Len, got:\n%s", ir)
	}
	if !strings.Contains(ir, "?weavert.ToString\t1.0\n") {
		t.Errorf("expected string(...) to lower to weavert.ToString, got:\n%s", ir)
	}
}

func TestGenerate_ForInBlocksBalanceAndUseKeyValueVars(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.ForStmt{
				Key: "k", Value: "v", Obj: &ast.Ident{Name: "o"},
				Body: []ast.Stmt{&ast.ExprStmt{X: &ast.CallExpr{
					Callee: &ast.Ident{Name: "print"}, Args: []ast.Expr{&ast.Ident{Name: "v"}},
				}}},
			},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	ir, err := Generate(file)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	blocksBalance(t, ir)
	if !strings.Contains(ir, "?weavert.ObjKeys\t%o\n") {
		t.Errorf("expected ObjKeys(o), got:\n%s", ir)
	}
	if !strings.Contains(ir, "?weavert.KeyAt\t") {
		t.Errorf("expected KeyAt for the key variable, got:\n%s", ir)
	}
	if !strings.Contains(ir, "VAR\t%k\t^any\n") || !strings.Contains(ir, "VAR\t%v\t^any\n") {
		t.Errorf("expected k and v to be declared, got:\n%s", ir)
	}
}

// A for-in body with no `continue` at all used to need special care
// under the old GOTO/LABEL scheme (a continue-label reachable only by
// fallthrough is "declared and not used" to go/types — CLAUDE.md's old
// Step 8 "確定した設計判断"). Under the current LOOP-based design
// (forin.go's genForStmt doc comment) there is no such label to begin
// with — the index advance sits at the top of the loop body and is
// always reached by ordinary fallthrough or a native CONTINUE alike —
// so this bug class no longer exists; blocksBalance already covers the
// remaining structural correctness via TestGenerate_ForInBlocksBalanceAndUseKeyValueVars.

func TestGenerate_ForInInsideClosureUsesPrefixedKeyValueTokens(t *testing.T) {
	// A regression test for a real bug: genForStmt used to declare k/v
	// via fg.declare but then reference the bare (unprefixed) s.Key/
	// s.Value names directly in the KeyAt/ObjGet CALLs, instead of the
	// namePrefix-qualified token declare actually returned — harmless at
	// the top level (namePrefix == ""), but inside a closure it produced
	// a reference to a %name that was never declared anywhere (caught by
	// go/types as "undefined": see CLAUDE.md's design-decision note).
	fg := newFuncGen(&codegenCtx{})
	_, err := genFuncLit(fg, &ast.FuncLit{
		Param: "o",
		Body: []ast.Stmt{
			&ast.ForStmt{
				Key: "k", Value: "v", Obj: &ast.Ident{Name: "o"},
				Body: []ast.Stmt{&ast.ExprStmt{X: &ast.CallExpr{
					Callee: &ast.Ident{Name: "print"}, Args: []ast.Expr{&ast.Ident{Name: "v"}},
				}}},
			},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	})
	if err != nil {
		t.Fatalf("genFuncLit: %v", err)
	}
	body := fg.body.String()
	if strings.Contains(body, "\t%k\t") || strings.Contains(body, "\t%v\t") || strings.Contains(body, "\t%v\n") {
		t.Errorf("expected no unprefixed %%k/%%v reference inside the closure, got:\n%s", body)
	}
	if !strings.Contains(body, "VAR\t%c1_k\t^any\n") || !strings.Contains(body, "VAR\t%c1_v\t^any\n") {
		t.Errorf("expected k/v declared with the closure's own prefix, got:\n%s", body)
	}
	if !strings.Contains(body, "?weavert.KeyAt\t") || !strings.Contains(body, "%c1_k\t") {
		t.Errorf("expected KeyAt to write into the prefixed key token, got:\n%s", body)
	}
	if !strings.Contains(body, "?weavert.ObjGet\t") || !strings.Contains(body, "%c1_v\t") {
		t.Errorf("expected ObjGet to write into the prefixed value token, got:\n%s", body)
	}
}

func TestGenerate_GoFuncDeclEmitsNoIR(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "toUpper", Value: &ast.CallExpr{
				Callee: &ast.Ident{Name: "gofunc"},
				Args:   []ast.Expr{&ast.StringLit{Value: "?strings.ToUpper"}},
			}},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	ir, err := Generate(file)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if strings.Contains(ir, "toUpper") {
		t.Errorf("gofunc(...) declaration must emit no IR referencing the Weave name itself, got:\n%s", ir)
	}
	if strings.Contains(ir, "VAR\t%toUpper") {
		t.Errorf("expected no VAR for a gofunc(...)-declared name, got:\n%s", ir)
	}
}

func TestGenerate_GoFuncCallRoutesThroughCallGoFuncList(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "toUpper", Value: &ast.CallExpr{
				Callee: &ast.Ident{Name: "gofunc"},
				Args:   []ast.Expr{&ast.StringLit{Value: "?strings.ToUpper"}},
			}},
			&ast.ExprStmt{X: &ast.CallExpr{
				Callee: &ast.Ident{Name: "print"},
				Args: []ast.Expr{&ast.CallExpr{
					Callee: &ast.Ident{Name: "toUpper"},
					Args:   []ast.Expr{&ast.StringLit{Value: "hi"}},
				}},
			}},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	ir, err := Generate(file)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	// weavert.CallGoFuncList, not a literal native CALL, is what makes an
	// `any`-typed argument (not just a literal) legal — see genGoFuncCall's
	// doc comment. It always returns a Weave list now (weave_spec.md
	// §15.2), so `print` here prints a one-element list, not the bare
	// string — this test only cares about the CALL shape, not the runtime
	// value.
	if !strings.Contains(ir, "?weavert.CallGoFuncList\t?strings.ToUpper\t\"hi\"\n") {
		t.Errorf("expected a CALL to ?weavert.CallGoFuncList passing ?strings.ToUpper as a value, got:\n%s", ir)
	}
	if strings.Contains(ir, "weavert.Call\t") {
		t.Errorf("a gofunc(...) call must not go through weavert.Call (ordinary closure dispatch), got:\n%s", ir)
	}
}

func TestGenerate_GovarDeclEmitsNoIR(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "ErrRange", Value: &ast.CallExpr{
				Callee: &ast.Ident{Name: "govar"},
				Args:   []ast.Expr{&ast.StringLit{Value: "?strconv.ErrRange"}},
			}},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	ir, err := Generate(file)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if strings.Contains(ir, "VAR\t%ErrRange") {
		t.Errorf("expected no VAR for a govar(...)-declared name, got:\n%s", ir)
	}
}

func TestGenerate_GovarReadEmitsLiveNativeCallPerReference(t *testing.T) {
	// weave_spec.md §15.5: each read of a govar(...)-declared name is its
	// own fresh CALL — not a cached snapshot — so referencing it twice
	// must emit the CALL twice (see genGoVarRead's doc comment on why
	// this is what "live" means here).
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "ErrRange", Value: &ast.CallExpr{
				Callee: &ast.Ident{Name: "govar"},
				Args:   []ast.Expr{&ast.StringLit{Value: "?strconv.ErrRange"}},
			}},
			&ast.ExprStmt{X: &ast.CallExpr{Callee: &ast.Ident{Name: "print"}, Args: []ast.Expr{&ast.Ident{Name: "ErrRange"}}}},
			&ast.ExprStmt{X: &ast.CallExpr{Callee: &ast.Ident{Name: "print"}, Args: []ast.Expr{&ast.Ident{Name: "ErrRange"}}}},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	ir, err := Generate(file)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if n := strings.Count(ir, "?weavert.NormalizeGoValue\t?strconv.ErrRange\n"); n != 2 {
		t.Errorf("expected 2 separate live reads of ErrRange, got %d:\n%s", n, ir)
	}
}

func TestGenerate_GoTypeDeclEmitsNoIR(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "GoFile", Value: &ast.CallExpr{
				Callee: &ast.Ident{Name: "gotype"},
				Args: []ast.Expr{
					&ast.StringLit{Value: "?os.File"},
					&ast.ObjectLit{Fields: []ast.ObjectField{
						{Name: "Close", Value: &ast.CallExpr{
							Callee: &ast.Ident{Name: "gomethod"},
							Args:   []ast.Expr{&ast.StringLit{Value: "Close"}},
						}},
					}},
				},
			}},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	ir, err := Generate(file)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if strings.Contains(ir, "GoFile") || strings.Contains(ir, "gotype") {
		t.Errorf("gotype(...) declaration must emit no IR at all, got:\n%s", ir)
	}
}

// atExpr builds `at(list, index)`.
func atExpr(list ast.Expr, index float64) *ast.CallExpr {
	return &ast.CallExpr{Callee: &ast.Ident{Name: "at"}, Args: []ast.Expr{list, &ast.NumberLit{Value: index}}}
}

// goReaderDeclStmts declares an entirely unbound GoReader/newReader pair
// (gofunc with no second/proto argument) — every call through these
// always returns a Weave list via reflection (weave_spec.md §15.2), and
// `r` itself is never statically Go-typed (proto-binding requires the
// gofunc(...)/gomethod(...) declaration's own optional second argument —
// weave_spec.md §15.1/§15.2 — contrast protoGoReaderDeclStmts below).
func goReaderDeclStmts() []ast.Stmt {
	return []ast.Stmt{
		&ast.AssignStmt{Name: "GoReader", Value: &ast.CallExpr{
			Callee: &ast.Ident{Name: "gotype"},
			Args: []ast.Expr{
				&ast.StringLit{Value: "?strings.Reader"},
				&ast.ObjectLit{Fields: []ast.ObjectField{
					{Name: "len", Value: &ast.CallExpr{
						Callee: &ast.Ident{Name: "gomethod"},
						Args:   []ast.Expr{&ast.StringLit{Value: "Len"}},
					}},
				}},
			},
		}},
		&ast.AssignStmt{Name: "newReader", Value: &ast.CallExpr{
			Callee: &ast.Ident{Name: "gofunc"},
			Args:   []ast.Expr{&ast.StringLit{Value: "?strings.NewReader"}},
		}},
		&ast.AssignStmt{Name: "r", Value: &ast.CallExpr{
			Callee: &ast.Ident{Name: "newReader"},
			Args:   []ast.Expr{&ast.StringLit{Value: "hi"}},
		}},
	}
}

func TestGenerate_GoFuncCallAlwaysBuildsListViaReflect(t *testing.T) {
	// weavert.CallGoFuncList builds the returned list internally via
	// reflection (see its own doc comment) — there is no separate
	// visible CALL to NormalizeGoValue in the emitted IR; this just
	// confirms the gofunc call itself is still routed through it.
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: append(goReaderDeclStmts(), &ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}}),
	}}
	ir, err := Generate(file)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.Contains(ir, "?weavert.CallGoFuncList\t?strings.NewReader\t\"hi\"\n") {
		t.Errorf("expected a CALL to ?weavert.CallGoFuncList passing ?strings.NewReader as a value, got:\n%s", ir)
	}
}

// protoGoReaderDeclStmts declares a gotype with an untyped `len` method
// (gomethod("Len"), no proto of its own) but a proto-bound newReader
// gofunc (gofunc(name, GoReader) — weave_spec.md §15.2's optional second
// argument), then extracts the returned list's position 0 into `r` via
// at(...). The method name is resolved statically (genGoMethodCall), but
// the Go call itself still always dispatches via reflection
// (weavert.CallGoMethodList) — there is no separate "fully native" tier
// any more (weave_spec.md §15.4's type hints were removed).
func protoGoReaderDeclStmts() []ast.Stmt {
	return []ast.Stmt{
		&ast.AssignStmt{Name: "GoReader", Value: &ast.CallExpr{
			Callee: &ast.Ident{Name: "gotype"},
			Args: []ast.Expr{
				&ast.StringLit{Value: "?*strings.Reader"},
				&ast.ObjectLit{Fields: []ast.ObjectField{
					{Name: "len", Value: &ast.CallExpr{
						Callee: &ast.Ident{Name: "gomethod"},
						Args:   []ast.Expr{&ast.StringLit{Value: "Len"}},
					}},
				}},
			},
		}},
		&ast.AssignStmt{Name: "newReader", Value: &ast.CallExpr{
			Callee: &ast.Ident{Name: "gofunc"},
			Args: []ast.Expr{
				&ast.StringLit{Value: "?strings.NewReader"},
				&ast.Ident{Name: "GoReader"},
			},
		}},
		&ast.AssignStmt{Name: "result", Value: &ast.CallExpr{
			Callee: &ast.Ident{Name: "newReader"},
			Args:   []ast.Expr{&ast.StringLit{Value: "hi"}},
		}},
		&ast.AssignStmt{Name: "r", Value: atExpr(&ast.Ident{Name: "result"}, 0)},
	}
}

func TestGenerate_StaticGoMethodCallBypassesWeavertObjGet(t *testing.T) {
	body := append(protoGoReaderDeclStmts(),
		&ast.ExprStmt{X: &ast.CallExpr{Callee: &ast.PropExpr{Obj: &ast.Ident{Name: "r"}, Prop: "len"}}},
		&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
	)
	file := &ast.File{Main: &ast.FuncDecl{Name: "main", Param: "args", Body: body}}
	ir, err := Generate(file)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.Contains(ir, "?weavert.CallGoMethodList\t%r\t\"Len\"\n") {
		t.Errorf("expected a direct CallGoMethodList with the resolved Go method name, got:\n%s", ir)
	}
	if strings.Contains(ir, "?weavert.ObjGet") {
		t.Errorf("a static Go method call must not go through weavert.ObjGet, got:\n%s", ir)
	}
}

func TestGenerate_GoMethodProtoBindingRoutesChainedCallThroughCallGoMethodList(t *testing.T) {
	// gomethod(...) itself can be proto-bound too (not just gofunc) —
	// mirrors sema's TestCheck_GoMethodProtoBindingPropagatesThroughChainedCall.
	// obj.first() returns a list whose position 0 is bound to a second
	// gotype; extracting that (at(..., 0), assigned to its own variable
	// first) must let a further `.Method()` call resolve the method name
	// statically too, dispatching through CallGoMethodList rather than
	// ObjGet.
	body := append(protoGoReaderDeclStmts(),
		&ast.AssignStmt{Name: "GoOuter", Value: &ast.CallExpr{
			Callee: &ast.Ident{Name: "gotype"},
			Args: []ast.Expr{
				&ast.StringLit{Value: "?pkg.Outer"},
				&ast.ObjectLit{Fields: []ast.ObjectField{
					{Name: "inner", Value: &ast.CallExpr{
						Callee: &ast.Ident{Name: "gomethod"},
						Args:   []ast.Expr{&ast.StringLit{Value: "Inner"}, &ast.Ident{Name: "GoReader"}},
					}},
				}},
			},
		}},
		&ast.AssignStmt{Name: "outer", Value: &ast.CallExpr{
			Callee: &ast.Ident{Name: "gofunc"},
			Args:   []ast.Expr{&ast.StringLit{Value: "?pkg.NewOuter"}, &ast.Ident{Name: "GoOuter"}},
		}},
		&ast.AssignStmt{Name: "outerResult", Value: &ast.CallExpr{Callee: &ast.Ident{Name: "outer"}}},
		&ast.AssignStmt{Name: "o", Value: atExpr(&ast.Ident{Name: "outerResult"}, 0)},
		&ast.AssignStmt{Name: "innerResult", Value: &ast.CallExpr{Callee: &ast.PropExpr{Obj: &ast.Ident{Name: "o"}, Prop: "inner"}}},
		&ast.AssignStmt{Name: "inner", Value: atExpr(&ast.Ident{Name: "innerResult"}, 0)},
		&ast.ExprStmt{X: &ast.CallExpr{Callee: &ast.PropExpr{Obj: &ast.Ident{Name: "inner"}, Prop: "len"}}},
		&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
	)
	file := &ast.File{Main: &ast.FuncDecl{Name: "main", Param: "args", Body: body}}
	ir, err := Generate(file)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.Contains(ir, "?weavert.CallGoMethodList\t%inner\t\"Len\"\n") {
		t.Errorf("expected the chained call's method name to resolve statically too, got:\n%s", ir)
	}
	if strings.Contains(ir, "?weavert.ObjGet\t%inner") {
		t.Errorf("a static Go method call must not go through weavert.ObjGet, got:\n%s", ir)
	}
}

func TestGenerate_UntypedGoMethodCallStillUsesReflectFallback(t *testing.T) {
	// A bare gomethod("Name") reached through an otherwise-unbound
	// gofunc (no proto argument anywhere) must keep using ordinary
	// dynamic dispatch — see TestGenerate_OrdinaryObjectMethodStillUsesDynamicDispatch
	// for the ObjGet assertion; this just confirms goReaderDeclStmts's
	// `r` really isn't statically typed (contrast protoGoReaderDeclStmts
	// above).
	body := append(goReaderDeclStmts(),
		&ast.ExprStmt{X: &ast.CallExpr{Callee: &ast.PropExpr{Obj: &ast.Ident{Name: "r"}, Prop: "len"}}},
		&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
	)
	file := &ast.File{Main: &ast.FuncDecl{Name: "main", Param: "args", Body: body}}
	ir, err := Generate(file)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.Contains(ir, "?weavert.ObjGet\t%r\t\"len\"\n") {
		t.Errorf("expected ordinary dynamic dispatch via ObjGet (r is never statically Go-typed here), got:\n%s", ir)
	}
	if strings.Contains(ir, "ASSERT\t") || strings.Contains(ir, "?weavert.CallGoMethodList") {
		t.Errorf("an unbound gomethod call must not emit any ASSERT/CallGoMethodList, got:\n%s", ir)
	}
}

func TestGenerate_OrdinaryObjectMethodStillUsesDynamicDispatch(t *testing.T) {
	// A variable that was never assigned from a gofunc(...) call must
	// keep using the ordinary dynamic obj.method() path.
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", Param: "args",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "obj", Value: &ast.ObjectLit{Fields: []ast.ObjectField{
				{Name: "greet", Value: &ast.FuncLit{Param: "self", Body: []ast.Stmt{&ast.ReturnStmt{Value: &ast.NumberLit{Value: 1}}}}},
			}}},
			&ast.ExprStmt{X: &ast.CallExpr{Callee: &ast.PropExpr{Obj: &ast.Ident{Name: "obj"}, Prop: "greet"}}},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	ir, err := Generate(file)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.Contains(ir, "?weavert.ObjGet\t%obj\t\"greet\"\n") {
		t.Errorf("expected ordinary dynamic dispatch via ObjGet, got:\n%s", ir)
	}
}

func TestGenerate_TopLevelStatementsPrecedeMainBody(t *testing.T) {
	// TopLevel statements are prepended into the same weave_main
	// funcGen as Main.Body (see ast.File's doc comment): a name bound
	// at top level must already be SET before main's own statements
	// run, and both must land inside the single FUNC !weave_main block.
	file := &ast.File{
		TopLevel: []ast.Stmt{
			&ast.AssignStmt{Name: "greeting", Value: &ast.StringLit{Value: "hi"}},
		},
		Main: &ast.FuncDecl{
			Name: "main", Param: "args",
			Body: []ast.Stmt{
				&ast.ExprStmt{X: &ast.CallExpr{
					Callee: &ast.Ident{Name: "print"},
					Args:   []ast.Expr{&ast.Ident{Name: "greeting"}},
				}},
				&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
			},
		},
	}
	ir, err := Generate(file)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	setIdx := strings.Index(ir, `SET	%greeting	"hi"`)
	printIdx := strings.Index(ir, "?weavert.Print")
	if setIdx == -1 || printIdx == -1 || setIdx > printIdx {
		t.Errorf("expected top-level SET before main's print CALL, got:\n%s", ir)
	}
	if strings.Count(ir, "FUNC\t!weave_main") != 1 {
		t.Errorf("expected exactly one FUNC !weave_main block, got:\n%s", ir)
	}
}

func TestGenerate_GoFuncCallInsideClosureDoesNotCaptureGoFuncName(t *testing.T) {
	// A closure calling a gofunc(...)-declared name (e.g. an actor
	// message handler constructing a Go asset) must never resolve it as
	// an ordinary %-variable — gofunc(...) declarations emit no VAR/SET
	// at all (genGeneralCall's gofunc check runs before ordinary Ident
	// resolution), so no %newReader token should ever appear.
	file := &ast.File{
		TopLevel: append(goReaderDeclStmts()[:2], // GoReader + newReader decls only
			&ast.AssignStmt{Name: "measure", Value: &ast.FuncLit{
				Param: "self",
				Body: []ast.Stmt{
					&ast.AssignStmt{Name: "r", Value: &ast.CallExpr{
						Callee: &ast.Ident{Name: "newReader"},
						Args:   []ast.Expr{&ast.PropExpr{Obj: &ast.Ident{Name: "self"}, Prop: "text"}},
					}},
					&ast.ReturnStmt{Value: &ast.CallExpr{
						Callee: &ast.PropExpr{Obj: &ast.Ident{Name: "r"}, Prop: "len"},
					}},
				},
			}},
		),
		Main: &ast.FuncDecl{
			Name: "main", Param: "args",
			Body: []ast.Stmt{&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}}},
		},
	}
	ir, err := Generate(file)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if strings.Contains(ir, "%newReader") {
		t.Errorf("gofunc(...)-declared name must never be captured as a closure free variable, got:\n%s", ir)
	}
	if !strings.Contains(ir, "?weavert.CallGoFuncList\t?strings.NewReader\t") {
		t.Errorf("expected the closure body to still compile the gofunc call itself, got:\n%s", ir)
	}
}

func TestGenerate_SelfRecursiveFuncLitReferencesOwnHoistedVar(t *testing.T) {
	// fact = fn(n) { ... fact(n - 1) ... } — mirrors sema's own
	// pre-declare-for-FuncLit-RHS carve-out (sema.go's checkStmt): fact
	// must resolve to weave_main's own %fact, not fall through to some
	// undefined/fresh token.
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
	ir, err := Generate(file)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if strings.Count(ir, "VAR\t%fact\t^any\n") != 1 {
		t.Errorf("expected exactly one VAR %%fact, got:\n%s", ir)
	}
	if !strings.Contains(ir, "?weavert.Call\t%fact\t%c1_n\n") {
		t.Errorf("expected the closure body to call weave_main's own %%fact, got:\n%s", ir)
	}
	if !strings.Contains(ir, "SET\t%fact\t%__t0\n") {
		t.Errorf("expected the closure value to still be assigned to %%fact after compiling it, got:\n%s", ir)
	}
}
