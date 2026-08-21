package weavert

import (
	"fmt"
	"reflect"
)

// Weave functions are first-class values that capture outer variables by
// reference (weave_spec.md §10) — including returning a closure from
// another closure, which is how currying works (§5's `fn(a) fn(b)
// {...}`). Every Weave function literal compiles directly to AMIVM's own
// CLOS instruction, nested exactly as deeply as the Weave source itself
// nests function literals. This relies on AMIVM's `&L-N` closure-argument
// addressing (added specifically to unblock this — CLOS can now nest
// inside another CLOS's body, and each nesting level's own bound
// parameter gets a level-qualified Go name so an inner CLOS's parameter
// never shadows an outer one — see internal/codegen/closure.go's doc
// comment and CLAUDE.md's design-decision note for the full history,
// including the earlier hand-rolled env-slice/reflection scheme this
// replaced).
//
// Because each Weave closure is now a real, lexically nested Go func
// literal, capturing outer variables needs no explicit bookkeeping at
// all — Go's own closure semantics do it, by reference, matching §10's
// "定義時点の外側の変数を参照で捕捉する" literally (the previous
// env-slice scheme captured by value instead, a documented deviation
// from this wording that native CLOS nesting resolves as a side effect).
//
// Call still goes through reflection rather than a native Go call
// expression: a Weave closure value is always held in a `^any`-typed
// variable (weave_spec.md §2's unified value representation), and Go
// requires a concretely function-typed expression at a call site — which
// an `any`-typed variable never is, even when its dynamic value is a
// function (the same wall genCond hit with `IF`/`^bool` — see CLAUDE.md's
// Step 4 "確定した設計判断"). Every Weave call expression lowers to this
// (internal/codegen/codegen.go's genGeneralCall), whether the callee is
// freshly created, a captured parameter, or read back out of a variable.
func Call(f any, arg any) any {
	fnVal := reflect.ValueOf(f)
	if fnVal.Kind() != reflect.Func {
		panic(fmt.Sprintf("weave: value is not callable, got %T", f))
	}
	fnType := fnVal.Type()

	// arg == nil carries no Go type information (reflect.ValueOf(nil) is
	// an invalid Value, unusable in Call), so it needs the target
	// parameter's own zero value instead — which, since that parameter is
	// always `any` (every Weave function's argument type), is just an
	// untyped nil `any`, identical in effect to passing arg directly when
	// arg is non-nil.
	argVal := reflect.ValueOf(arg)
	if !argVal.IsValid() {
		argVal = reflect.Zero(fnType.In(0))
	}

	out := fnVal.Call([]reflect.Value{argVal})
	return out[0].Interface()
}
