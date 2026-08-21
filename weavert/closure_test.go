package weavert

import "testing"

func TestCall_InvokesNativeGoClosure(t *testing.T) {
	base := 5.0
	adder := func(arg any) any { return base + arg.(float64) }
	got := Call(adder, 3.0)
	if got != 8.0 {
		t.Errorf("Call = %v, want 8", got)
	}
}

func TestCall_CapturesByReference(t *testing.T) {
	// Native Go closures capture the variable, not a value snapshot —
	// unlike the previous env-slice scheme, a later mutation of the
	// captured variable is visible the next time the closure runs (see
	// closure.go's doc comment).
	n := 1.0
	getN := func(any) any { return n }
	n = 2.0
	if got := Call(getN, nil); got != 2.0 {
		t.Errorf("Call(getN, nil) = %v, want 2 (reference capture)", got)
	}
}

func TestCall_NilArgument(t *testing.T) {
	identity := func(arg any) any { return arg }
	if got := Call(identity, nil); got != nil {
		t.Errorf("Call(identity, nil) = %v, want nil", got)
	}
}

func TestCall_NonFunctionPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected Call on a non-function value to panic")
		}
	}()
	Call(5.0, 1.0)
}
