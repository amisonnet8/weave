package codegen

import (
	"fmt"
	"strings"

	"github.com/amisonnet8/weave/internal/ast"
)

// GoTypeInfo/GoFuncInfo mirror sema's own (internal/sema/goasset.go) —
// a separate copy per the established pattern of codegen re-deriving
// its own view of the AST rather than consuming sema's internal state
// (see CLAUDE.md's "確定した設計判断" on manually-synced tables, e.g.
// builtinNames/reservedName).
type GoTypeInfo struct {
	GoName  string
	Methods map[string]string
}

type GoFuncInfo struct {
	GoName string
	Proto  string
}

// genGoDecl mirrors sema's own recognition of `name = gotype(...)` /
// `name = gofunc(...)` (sema/goasset.go's checkGoDecl) — codegen has
// already been told by sema.Check that the file is valid, so this just
// re-derives the same symbol tables to know how to lower later
// references. Neither declaration form emits any IR itself (weave_spec.md
// §16: these are compile-time-only, never a runtime value) — genAssignStmt
// calls this before its ordinary dynamic-value handling and, if it
// reports true, skips VAR/SET entirely for this statement.
func genGoDecl(fg *funcGen, name string, call *ast.CallExpr) (bool, error) {
	callee, ok := call.Callee.(*ast.Ident)
	if !ok {
		return false, nil
	}
	switch callee.Name {
	case "gotype":
		genGoTypeDecl(fg, name, call)
		return true, nil
	case "gofunc":
		genGoFuncDecl(fg, name, call)
		return true, nil
	default:
		return false, nil
	}
}

func genGoTypeDecl(fg *funcGen, name string, call *ast.CallExpr) {
	goName := call.Args[0].(*ast.StringLit).Value
	members := call.Args[1].(*ast.ObjectLit)
	methods := map[string]string{}
	for _, field := range members.Fields {
		mcall := field.Value.(*ast.CallExpr)
		methods[field.Name] = mcall.Args[0].(*ast.StringLit).Value
	}
	if fg.ctx.goTypes == nil {
		fg.ctx.goTypes = map[string]*GoTypeInfo{}
	}
	fg.ctx.goTypes[name] = &GoTypeInfo{GoName: goName, Methods: methods}
}

func genGoFuncDecl(fg *funcGen, name string, call *ast.CallExpr) {
	goName := call.Args[0].(*ast.StringLit).Value
	proto := ""
	if id, ok := call.Args[1].(*ast.Ident); ok {
		proto = id.Name
	}
	if fg.ctx.goFuncs == nil {
		fg.ctx.goFuncs = map[string]*GoFuncInfo{}
	}
	fg.ctx.goFuncs[name] = &GoFuncInfo{GoName: goName, Proto: proto}
}

// genGoFuncCall lowers a call to a gofunc(...)-declared name directly to
// its real Go function, bypassing weavert.Call's dynamic dispatch
// entirely (weave_spec.md §16: "動的なプロトタイプ検索を一切経由せず
// ...ネイティブ命令の速さ・単純さ" for gofunc/gomethod). Arguments are
// passed positionally, matching the wrapped Go function's own native
// arity — not curried the way an ordinary Weave call is, since the
// whole point of gofunc is to mirror the real Go signature rather than
// force it through Weave's 1-argument convention.
//
// Scope limitation: this assumes the wrapped Go function returns
// exactly one value. weave_spec.md never addresses multi-value Go
// returns (§17's own os.Open example actually returns (*File, error) in
// real Go, which this can't express) — deferred to when Step 5's
// integration sample actually needs it; see CLAUDE.md's 後半 Step 3
// "確定した設計判断".
func genGoFuncCall(fg *funcGen, info *GoFuncInfo, call *ast.CallExpr) (string, error) {
	var argVals []string
	for _, arg := range call.Args {
		v, err := genExpr(fg, arg)
		if err != nil {
			return "", err
		}
		argVals = append(argVals, v)
	}
	tmp := fg.newTemp("^any")
	fmt.Fprintf(&fg.body, "\tCALL\t%s\t:\t%s%s\n", tmp, info.GoName, argSuffix(argVals))
	return tmp, nil
}

func argSuffix(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return "\t" + strings.Join(args, "\t")
}
