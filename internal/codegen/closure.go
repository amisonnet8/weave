package codegen

import (
	"fmt"

	"github.com/amisonnet8/weave/internal/ast"
)

// genFuncLit compiles a function literal (weave_spec.md §5) into a
// native AMIVM CLOS block, nested inline exactly where the literal
// appears in the enclosing funcGen's own body — never an independent
// top-level FUNC. AMIVM's CLOS instruction can now nest inside another
// CLOS's body (amivm_spec.md §2.1, extended specifically for this): a
// curried literal (`fn(a) fn(b) {...}`, whose body is, after parser.go's
// flattening, itself a `return`ed FuncLit) compiles to a CLOS containing
// another CLOS, arbitrarily deep, one level per curry stage.
//
// Free variables need no explicit capture step: the generated Go func
// literal is lexically nested exactly where the Weave closure literal
// appears, so it captures enclosing %-variables through Go's own closure
// semantics — by reference, matching weave_spec.md §10's "定義時点の
// 外側の変数を参照で捕捉する" literally (see funcGen's doc comment and
// CLAUDE.md's design-decision note for how this differs from — and
// replaces — the previous env-slice/reflection scheme, which captured by
// value because AMIVM's CLOS couldn't nest yet).
//
// Each closure's own bound parameter is bound via `&1` — always the
// *current* CLOS's own level, referenced only once, right here, whether
// or not this literal is itself nested inside another closure (AMIVM
// resolves `&1` relative to whichever CLOS the token is textually
// written inside — see amivm_spec.md §2.1/§4). It's immediately copied
// into an ordinary %-declared local (matching how a captured outer
// variable is read: a plain %token) so that every subsequent reference
// to the parameter, from this closure's own body OR a nested closure's,
// is uniform — a %-token resolved through funcGen.resolve, never a raw
// `&N`/`&L-N` operand. This means codegen never needs to track or emit
// explicit `&L-N` addressing itself: AMIVM's own nesting-depth-based
// naming (`amivm_closureL_paramN`) only has to disambiguate that one
// `SET ... &1` line per closure instance from every other instance's —
// which funcGen.namePrefix already guarantees by construction.
func genFuncLit(fg *funcGen, lit *ast.FuncLit) (string, error) {
	fg.ctx.closureCount++
	inner := newFuncGen(fg.ctx)
	inner.parent = fg
	inner.namePrefix = fmt.Sprintf("c%d_", fg.ctx.closureCount)

	paramTok := inner.declare(lit.Param, "^any")
	fmt.Fprintf(&inner.body, "\tSET\t%%%s\t&1\n", paramTok)

	if err := genBlock(inner, lit.Body); err != nil {
		return "", err
	}
	// A Weave function body needn't end with an explicit `return`
	// (weave_spec.md §6.2's own `increment: fn(self, n) { self.count =
	// self.count + n }` has none) — but the generated CLOS still
	// declares an `^any` result, and Go requires every path through such
	// a function literal to return something. Appending an unconditional
	// trailing `RET nil` covers the implicit case; for a body that
	// already returns on every path, this is simply unreachable code,
	// which Go accepts without complaint (confirmed by direct probe —
	// see CLAUDE.md's Step 7 "確定した設計判断").
	inner.body.WriteString("\tRET\tnil\n")

	target := fg.newTemp("^any")
	fmt.Fprintf(&fg.body, "\tCLOS\t%s\t^any\t:\t^any\n", target)
	for _, d := range inner.decls {
		fg.body.WriteString(d)
	}
	fg.body.WriteString(inner.body.String())
	fg.body.WriteString("\tENDCLOS\n")

	return target, nil
}
