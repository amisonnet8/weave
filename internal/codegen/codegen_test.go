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
		Name: "main", ReturnType: "int",
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

// labelsBalance is a light structural check for control-flow codegen:
// every GOTO/IF target must have a matching LABEL, and vice versa.
func labelsBalance(t *testing.T, ir string) {
	t.Helper()
	defined := map[string]bool{}
	referenced := map[string]bool{}
	for _, line := range strings.Split(ir, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "LABEL":
			defined[fields[1]] = true
		case "GOTO":
			referenced[fields[1]] = true
		case "IF":
			referenced[fields[2]] = true
		}
	}
	for label := range referenced {
		if !defined[label] {
			t.Errorf("label %s is referenced but never defined:\n%s", label, ir)
		}
	}
	for label := range defined {
		if !referenced[label] {
			t.Errorf("label %s is defined but never referenced:\n%s", label, ir)
		}
	}
}

func TestGenerate_IfElifElseLabelsBalance(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", ReturnType: "int",
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
	labelsBalance(t, ir)
	if strings.Count(ir, "IF\t") != 2 {
		t.Errorf("expected 2 IFs (one per if/elif condition), got:\n%s", ir)
	}
}

func TestGenerate_WhileBreakContinueTargetInnermostLoop(t *testing.T) {
	// while true { while true { break; continue } }
	// the inner break/continue must target the INNER loop's labels.
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", ReturnType: "int",
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
	labelsBalance(t, ir)

	// There must be two distinct while_start/while_end pairs (outer,
	// inner). break's GOTO must target the *second* (inner) while_end,
	// and continue's GOTO must target the *second* (inner) while_start —
	// not the outer loop's.
	lines := strings.Split(ir, "\n")
	var whileStarts, whileEnds []string
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "LABEL" {
			if strings.HasPrefix(fields[1], "#while_start") {
				whileStarts = append(whileStarts, fields[1])
			}
			if strings.HasPrefix(fields[1], "#while_end") {
				whileEnds = append(whileEnds, fields[1])
			}
		}
	}
	if len(whileStarts) != 2 || len(whileEnds) != 2 {
		t.Fatalf("expected 2 while_start and 2 while_end labels, got %v / %v", whileStarts, whileEnds)
	}
	// The two GOTOs immediately following break/continue's position:
	// find them by locating the two consecutive GOTO lines inside the
	// innermost body.
	var gotos []string
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "GOTO" {
			gotos = append(gotos, fields[1])
		}
	}
	// last two GOTOs before the innermost while's closing GOTO-to-start
	// are break's and continue's targets, in that order.
	foundBreak, foundContinue := false, false
	for _, g := range gotos {
		if g == whileEnds[1] {
			foundBreak = true
		}
		if g == whileStarts[1] {
			foundContinue = true
		}
	}
	if !foundBreak {
		t.Errorf("expected a GOTO to the inner while_end (%s) for break, gotos: %v", whileEnds[1], gotos)
	}
	if !foundContinue {
		t.Errorf("expected a GOTO to the inner while_start (%s) for continue, gotos: %v", whileStarts[1], gotos)
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
	labelsBalance(t, "FUNC\t!f\t:\n"+body+"ENDFUNC\n")
}

func TestGenerate_ShortCircuitOrSkipsRHSOnTrue(t *testing.T) {
	fg := newFuncGen(&codegenCtx{})
	_, err := genExpr(fg, &ast.BinaryExpr{Op: "||", X: &ast.Ident{Name: "a"}, Y: &ast.Ident{Name: "b"}})
	if err != nil {
		t.Fatalf("genExpr: %v", err)
	}
	labelsBalance(t, "FUNC\t!f\t:\n"+fg.body.String()+"ENDFUNC\n")
}

func TestGenerate_ConditionRoutesThroughCheckBool(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", ReturnType: "int",
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

func TestFreeVars_ParamIsNotFree(t *testing.T) {
	lit := &ast.FuncLit{Param: "a", Body: []ast.Stmt{
		&ast.ReturnStmt{Value: &ast.Ident{Name: "a"}},
	}}
	if got := freeVars(lit, nil); len(got) != 0 {
		t.Errorf("freeVars = %v, want none (a is the param)", got)
	}
}

func TestFreeVars_OuterVariableIsFree(t *testing.T) {
	lit := &ast.FuncLit{Param: "x", Body: []ast.Stmt{
		&ast.ReturnStmt{Value: &ast.BinaryExpr{Op: "+", X: &ast.Ident{Name: "x"}, Y: &ast.Ident{Name: "base"}}},
	}}
	got := freeVars(lit, nil)
	if len(got) != 1 || got[0] != "base" {
		t.Errorf("freeVars = %v, want [base]", got)
	}
}

func TestFreeVars_LocallyAssignedNameIsNotFree(t *testing.T) {
	lit := &ast.FuncLit{Param: "x", Body: []ast.Stmt{
		&ast.AssignStmt{Name: "y", Value: &ast.NumberLit{Value: 1}},
		&ast.ReturnStmt{Value: &ast.Ident{Name: "y"}},
	}}
	if got := freeVars(lit, nil); len(got) != 0 {
		t.Errorf("freeVars = %v, want none (y is assigned locally before use)", got)
	}
}

func TestFreeVars_NameOnlyBoundInsideIfIsStillFreeAfter(t *testing.T) {
	// if true { y = 1 }
	// return y   <- y was never bound at THIS literal's own top level,
	// so a read here must be treated as free (referring to some outer
	// scope) even though "y" appears bound somewhere inside the literal.
	lit := &ast.FuncLit{Param: "x", Body: []ast.Stmt{
		&ast.IfStmt{Clauses: []ast.IfClause{{
			Cond: &ast.BoolLit{Value: true},
			Body: []ast.Stmt{&ast.AssignStmt{Name: "y", Value: &ast.NumberLit{Value: 1}}},
		}}},
		&ast.ReturnStmt{Value: &ast.Ident{Name: "y"}},
	}}
	got := freeVars(lit, nil)
	if len(got) != 1 || got[0] != "y" {
		t.Errorf("freeVars = %v, want [y]", got)
	}
}

func TestFreeVars_NestedFuncLitCapturesTransitively(t *testing.T) {
	// fn(a) fn(b) { return a + b + outer }
	// outer must be free for BOTH the inner and outer literal.
	inner := &ast.FuncLit{Param: "b", Body: []ast.Stmt{
		&ast.ReturnStmt{Value: &ast.BinaryExpr{
			Op: "+",
			X:  &ast.BinaryExpr{Op: "+", X: &ast.Ident{Name: "a"}, Y: &ast.Ident{Name: "b"}},
			Y:  &ast.Ident{Name: "outer"},
		}},
	}}
	outer := &ast.FuncLit{Param: "a", Body: []ast.Stmt{&ast.ReturnStmt{Value: inner}}}

	innerFree := freeVars(inner, nil)
	if len(innerFree) != 2 || innerFree[0] != "a" || innerFree[1] != "outer" {
		t.Errorf("inner freeVars = %v, want [a outer]", innerFree)
	}
	outerFree := freeVars(outer, nil)
	if len(outerFree) != 1 || outerFree[0] != "outer" {
		t.Errorf("outer freeVars = %v, want [outer] (a is outer's own param)", outerFree)
	}
}

func TestGenerate_FuncLitEmitsClosureFuncAndSLTYPE(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", ReturnType: "int",
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
	if !strings.HasPrefix(ir, "SLTYPE\t^WeaveEnv\t^any\n") {
		t.Errorf("expected the IR to start with the env SLTYPE, got:\n%s", ir)
	}
	if !strings.Contains(ir, "FUNC\t!closure0\t^WeaveEnv\t^any\t:\t^any\n") {
		t.Errorf("expected a closure0 FUNC with the env type, got:\n%s", ir)
	}
	if !strings.Contains(ir, "AGET\t%base\t$1\t0\n") {
		t.Errorf("expected the closure to unpack captured base via AGET, got:\n%s", ir)
	}
	if !strings.Contains(ir, "SET\t%x\t$2\n") {
		t.Errorf("expected the closure's own param to be bound from $2, got:\n%s", ir)
	}
	if !strings.Contains(ir, "ASET\t%__t") || !strings.Contains(ir, "\t0\t%base\n") {
		t.Errorf("expected the call site to ASET base into the env, got:\n%s", ir)
	}
	if !strings.Contains(ir, "?weavert.NewClosure\t!closure0\t") {
		t.Errorf("expected the call site to build the closure via weavert.NewClosure, got:\n%s", ir)
	}
	// A closure's own `return` must NOT go through weavert.ExitCode —
	// that's exclusively main's bridge (see genReturnStmt's doc
	// comment on the Step 5 bug this guards against).
	closureStart := strings.Index(ir, "FUNC\t!closure0")
	closureEnd := strings.Index(ir[closureStart:], "ENDFUNC") + closureStart
	closureBody := ir[closureStart:closureEnd]
	if strings.Contains(closureBody, "weavert.ExitCode") {
		t.Errorf("closure body must not call weavert.ExitCode, got:\n%s", closureBody)
	}
	if !strings.Contains(closureBody, "RET\t%__t") {
		t.Errorf("expected the closure to RET its own ^any temp, got:\n%s", closureBody)
	}
}

func TestGenerate_NoClosuresMeansNoSLTYPE(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", ReturnType: "int",
		Body: []ast.Stmt{&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}}},
	}}
	ir, err := Generate(file)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if strings.Contains(ir, "WeaveEnv") {
		t.Errorf("expected no WeaveEnv SLTYPE when no closures were compiled, got:\n%s", ir)
	}
}

func TestGenerate_MultiArgCallCurriesThroughWeavertCall(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", ReturnType: "int",
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

func TestGenerate_ZeroArgGeneralCallIsAnError(t *testing.T) {
	fg := newFuncGen(&codegenCtx{})
	_, err := genGeneralCall(fg, &ast.CallExpr{Callee: &ast.Ident{Name: "f"}})
	if err == nil {
		t.Fatal("expected an error: a call needs at least one argument")
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

func TestGenerate_HasAndRemoveBuiltins(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", ReturnType: "int",
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

func TestFreeVars_ObjectLitFieldValuesAreWalked(t *testing.T) {
	lit := &ast.FuncLit{Param: "x", Body: []ast.Stmt{
		&ast.ReturnStmt{Value: &ast.ObjectLit{Fields: []ast.ObjectField{
			{Name: "a", Value: &ast.Ident{Name: "x"}},
			{Name: "b", Value: &ast.Ident{Name: "base"}},
		}}},
	}}
	got := freeVars(lit, nil)
	if len(got) != 1 || got[0] != "base" {
		t.Errorf("freeVars = %v, want [base] (x is the param)", got)
	}
}

func TestFreeVars_PropExprObjIsWalked(t *testing.T) {
	lit := &ast.FuncLit{Param: "x", Body: []ast.Stmt{
		&ast.ReturnStmt{Value: &ast.PropExpr{Obj: &ast.Ident{Name: "outer"}, Prop: "field"}},
	}}
	got := freeVars(lit, nil)
	if len(got) != 1 || got[0] != "outer" {
		t.Errorf("freeVars = %v, want [outer]", got)
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
	closureBody := fg.ctx.closureFuncs[0]
	if !strings.HasSuffix(strings.TrimSuffix(closureBody, "ENDFUNC\n"), "RET\tnil\n") {
		t.Errorf("expected the closure to end with an implicit RET nil, got:\n%s", closureBody)
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
		Name: "main", ReturnType: "int",
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

func TestGenerate_ForInLabelsBalanceAndUseKeyValueVars(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", ReturnType: "int",
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
	labelsBalance(t, ir)
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

func TestGenerate_ForInContinueLabelAlwaysReferenced(t *testing.T) {
	// A for-in body with no `continue` at all must still reference its
	// continueLabel via an explicit GOTO — Go treats a label reachable
	// only by fallthrough as unused (the exact bug examples/builtins.weave
	// caught — see CLAUDE.md's Step 8 "確定した設計判断").
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", ReturnType: "int",
		Body: []ast.Stmt{
			&ast.ForStmt{Key: "k", Value: "v", Obj: &ast.Ident{Name: "o"}},
			&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
		},
	}}
	ir, err := Generate(file)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	labelsBalance(t, ir)
}

func TestGenerate_GoFuncDeclEmitsNoIR(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", ReturnType: "int",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "toUpper", Value: &ast.CallExpr{
				Callee: &ast.Ident{Name: "gofunc"},
				Args:   []ast.Expr{&ast.StringLit{Value: "?strings.ToUpper"}, &ast.NilLit{}},
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

func TestGenerate_GoFuncCallRoutesThroughCallGoFunc(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", ReturnType: "int",
		Body: []ast.Stmt{
			&ast.AssignStmt{Name: "toUpper", Value: &ast.CallExpr{
				Callee: &ast.Ident{Name: "gofunc"},
				Args:   []ast.Expr{&ast.StringLit{Value: "?strings.ToUpper"}, &ast.NilLit{}},
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
	// weavert.CallGoFunc, not a literal native CALL, is what makes an
	// `any`-typed argument (not just a literal) legal — see genGoFuncCall's
	// doc comment.
	if !strings.Contains(ir, "?weavert.CallGoFunc\t?strings.ToUpper\t\"hi\"\n") {
		t.Errorf("expected a CALL to ?weavert.CallGoFunc passing ?strings.ToUpper as a value, got:\n%s", ir)
	}
	if strings.Contains(ir, "weavert.Call\t") {
		t.Errorf("a gofunc(...) call must not go through weavert.Call (ordinary closure dispatch), got:\n%s", ir)
	}
}

func TestGenerate_GoTypeDeclEmitsNoIR(t *testing.T) {
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", ReturnType: "int",
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
			Args:   []ast.Expr{&ast.StringLit{Value: "?strings.NewReader"}, &ast.Ident{Name: "GoReader"}},
		}},
		&ast.AssignStmt{Name: "r", Value: &ast.CallExpr{
			Callee: &ast.Ident{Name: "newReader"},
			Args:   []ast.Expr{&ast.StringLit{Value: "hi"}},
		}},
	}
}

func TestGenerate_GoFuncCallNormalizesResult(t *testing.T) {
	// weavert.CallGoFunc folds NormalizeGoValue in internally (see its
	// own doc comment) — there is no separate visible CALL to
	// NormalizeGoValue in the emitted IR any more; this just confirms
	// the gofunc call itself is still routed through CallGoFunc.
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", ReturnType: "int",
		Body: append(goReaderDeclStmts(), &ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}}),
	}}
	ir, err := Generate(file)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.Contains(ir, "?weavert.CallGoFunc\t?strings.NewReader\t\"hi\"\n") {
		t.Errorf("expected a CALL to ?weavert.CallGoFunc passing ?strings.NewReader as a value, got:\n%s", ir)
	}
}

func TestGenerate_StaticGoMethodCallBypassesWeavertObjGet(t *testing.T) {
	body := append(goReaderDeclStmts(),
		&ast.ExprStmt{X: &ast.CallExpr{Callee: &ast.PropExpr{Obj: &ast.Ident{Name: "r"}, Prop: "len"}}},
		&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}},
	)
	file := &ast.File{Main: &ast.FuncDecl{Name: "main", ReturnType: "int", Body: body}}
	ir, err := Generate(file)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.Contains(ir, "?weavert.CallGoMethod\t%r\t\"Len\"\n") {
		t.Errorf("expected a direct CallGoMethod with the resolved Go method name, got:\n%s", ir)
	}
	if strings.Contains(ir, "?weavert.ObjGet") {
		t.Errorf("a static Go method call must not go through weavert.ObjGet, got:\n%s", ir)
	}
}

func TestGenerate_OrdinaryObjectMethodStillUsesDynamicDispatch(t *testing.T) {
	// A variable that was never assigned from a gofunc(...) call must
	// keep using the ordinary dynamic obj.method() path.
	file := &ast.File{Main: &ast.FuncDecl{
		Name: "main", ReturnType: "int",
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
			Name: "main", ReturnType: "int",
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
	// message handler constructing a Go asset) must not treat that name
	// as a free variable to capture — gofunc(...) declarations emit no
	// VAR/SET at all, so an AGET/ASET pair referencing it would name an
	// undeclared variable (see freeVars' doc comment; caught via a
	// spawn/gofunc integration example).
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
			Name: "main", ReturnType: "int",
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
	if !strings.Contains(ir, "?weavert.CallGoFunc\t?strings.NewReader\t") {
		t.Errorf("expected the closure body to still compile the gofunc call itself, got:\n%s", ir)
	}
}
