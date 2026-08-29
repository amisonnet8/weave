package weavert

import "fmt"

// RecoverPanic implements the `recover(handler)` builtin (weave_spec.md
// §17): codegen's genRecoverCall lowers `recover(handler)` to a single
// native AMIVM `DEFER ?weavert.RecoverPanic <handler>` instruction —
// `defer weavert.RecoverPanic(handler)` in the generated Go — placed
// exactly where the Weave source calls recover(...), so this function
// itself runs, deferred, at the point its own enclosing Go function
// returns, precisely matching Go's own `defer`+`recover()` idiom (this
// is *why* codegen doesn't route recover(...) through the usual
// weavert.Call+CALL pattern every other builtin here uses — Go's
// recover() only has an effect when called directly by a deferred
// function, so the deferral has to be genuinely native, not laundered
// through an extra call frame).
//
// If a panic is in flight when this runs (recover() returns non-nil), it
// is suppressed here — the enclosing Go function then returns normally,
// using its return type's own zero value (Go's ordinary behavior for an
// unnamed return recovered mid-flight, which this deliberately relies on
// rather than working around: nil for an ^any-returning closure — no
// different from a body that never explicitly returns at all, weave_spec.md
// §5 — or 0 for weave_main's own ^int, which becomes exit code 0). handler
// is then invoked with the panic's message, converted to a Weave string
// via fmt.Sprint (works uniformly whether the recovered value is one of
// Weave's own "weave: ..." panic messages, weavert/goasset.go's
// RaiseIfError, or an arbitrary Go value from deep inside a reflect-based
// gofunc/gomethod call). handler's own return value is discarded — Go's
// defer/recover semantics only let a *named* return value be modified
// from within a deferred function, and Weave's compiled functions never
// use one (see weave_spec.md §17 for the guarded-helper-closure pattern
// this implies for callers who need a fallback result, not just a
// side effect).
func RecoverPanic(handler any) {
	if r := recover(); r != nil {
		Call(handler, fmt.Sprint(r))
	}
}
