package weavert

import (
	"fmt"
	"reflect"
)

// Weave functions are first-class values that can capture outer
// variables (weave_spec.md §10) — including returning a closure from
// another closure, which is how currying works (§5's `fn(a) fn(b)
// {...}`). AMIVM's CLOS instruction can't express this directly: CLOS
// may not nest inside another CLOS's body (amivm_spec.md §2.1; Cascade
// hit the same wall and simply forbids closures-in-closures — see
// ignored/cascade/internal/codegen/closure.go's doc comment), so a
// closure that needs to return another closure capturing its own
// parameter cannot be a literal Go closure nested inside another one at
// the IR level.
//
// Instead, every Weave function literal compiles to its own independent
// top-level AMIVM FUNC (never a CLOS), and the closure's captured outer
// variables are passed explicitly as a values slice, bundled with a
// reference to that FUNC into a Closure value. This is effectively
// hand-rolled closure conversion, done because AMIVM's own CLOS can't
// do it here. See CLAUDE.md's Step 5 "確定した設計判断" for the full
// design writeup, including the resulting limitation: captures are a
// snapshot taken when the closure is created, not a live reference —
// mutating a captured variable inside the closure body only affects
// that closure's own copy, never the enclosing scope's variable (real
// lexical closures, and Go's own, capture by reference instead).
//
// Call uses reflection rather than a statically-typed function value.
// The natural static design — a named `ClosureFunc func(Env, any) any`
// type here in weavert, referenced from the generated closure FUNC's
// signature as `^weavert.Env` — runs into two AMIVM/Go type-system
// walls at once: AMIVM's SLMAKE only accepts a locally-declared
// `deftype` (a bare `^xxx`, never a package-qualified `^pkg.xxx` —
// confirmed by amivm rejecting `SLMAKE ... ^weavert.Env ...` outright),
// so the env slice's type has to be declared locally in the generated
// program, not reused from here — and Go's function-type identity then
// requires that locally-declared env type to be *exactly* the same
// declared type the generated closure FUNC's signature uses, which a
// second, separately-declared type in this package can never be
// assignment-compatible with. Reflection sidesteps both problems: it
// doesn't care whether the env slice's concrete type was declared here
// or in the generated program, only that it matches what the target
// function expects at the call site — which it always does, since
// codegen consistently uses one locally-declared slice type for every
// closure's env parameter in a given program (internal/codegen/closure.go).

// Closure is a Weave function value: a captured environment plus the
// top-level FUNC to run it through. NewClosure builds one; Call invokes
// one. Both codegen entry points funnel every Weave function value
// through these two functions uniformly, whether or not it actually
// captured anything (an empty env is just as valid).
type Closure struct {
	Fn  any // an AMIVM-generated closure FUNC value: func(EnvType, any) any
	Env any // that FUNC's own EnvType — a locally-declared []any alias
}

// NewClosure bundles fn (an AMIVM top-level FUNC referenced as a bare
// value — amivm_spec.md §5's `value` category includes `!xxx_123`,
// confirmed working for exactly this in
// ignored/amivm/test_ir/15_closure.ir's "AMIVM内定義関数そのものを値と
// して変数に代入し" case) with its captured env into a callable Weave
// value.
func NewClosure(fn any, env any) any {
	return &Closure{Fn: fn, Env: env}
}

// Call applies a Weave function value to a single argument — every
// Weave function takes exactly one (weave_spec.md §5) — and is what
// every Weave call expression lowers to (internal/codegen/closure.go's
// genGeneralCall), whether the callee is freshly created, a captured
// parameter, or read back out of a variable.
func Call(f any, arg any) any {
	c, ok := f.(*Closure)
	if !ok {
		panic(fmt.Sprintf("weave: value is not callable, got %T", f))
	}
	fnVal := reflect.ValueOf(c.Fn)
	fnType := fnVal.Type()

	// arg == nil carries no Go type information (reflect.ValueOf(nil)
	// is an invalid Value, unusable in Call), so it needs the target
	// parameter's own zero value instead — which, since that parameter
	// is always `any` (every Weave function's argument type), is just
	// an untyped nil `any`, identical in effect to passing arg directly
	// when arg is non-nil.
	argVal := reflect.ValueOf(arg)
	if !argVal.IsValid() {
		argVal = reflect.Zero(fnType.In(1))
	}

	out := fnVal.Call([]reflect.Value{reflect.ValueOf(c.Env), argVal})
	return out[0].Interface()
}
