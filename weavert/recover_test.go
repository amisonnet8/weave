package weavert

import "testing"

// guarded mirrors exactly what codegen's genRecoverCall emits for
// `recover(handler)` — a native `defer weavert.RecoverPanic(handler)` at
// the very top of some function — so these tests exercise RecoverPanic
// the same way it's actually ever invoked (Go's recover() only has an
// effect when called directly by a deferred function, so RecoverPanic
// itself can't be tested by calling it directly, only through defer).
func guarded(handler any, body func()) {
	defer RecoverPanic(handler)
	body()
}

func TestRecoverPanic_RecoversAndCallsHandlerWithMessage(t *testing.T) {
	var got any
	handler := func(msg any) any {
		got = msg
		return nil
	}
	guarded(handler, func() {
		panic("weave: something went wrong")
	})
	if got != "weave: something went wrong" {
		t.Errorf("handler received %v, want the panic message unchanged", got)
	}
}

func TestRecoverPanic_ConvertsNonStringPanicValueToString(t *testing.T) {
	// A Go asset call panicking with something other than a plain string
	// (weave_spec.md §17 doesn't distinguish where the panic came from)
	// must still reach the handler as a Weave string.
	var got any
	handler := func(msg any) any {
		got = msg
		return nil
	}
	guarded(handler, func() {
		panic(42)
	})
	if got != "42" {
		t.Errorf("handler received %v (%T), want the string \"42\"", got, got)
	}
}

func TestRecoverPanic_NoOpWhenNoPanicOccurred(t *testing.T) {
	called := false
	handler := func(any) any {
		called = true
		return nil
	}
	guarded(handler, func() {})
	if called {
		t.Error("handler must not run when the guarded body didn't panic")
	}
}

func TestRecoverPanic_SuppressesThePanicEntirely(t *testing.T) {
	// The whole point: code after guarded(...) keeps running normally —
	// nothing here should itself panic or otherwise abort the test.
	guarded(func(any) any { return nil }, func() {
		panic("boom")
	})
	// Reaching this line at all is the assertion.
}
