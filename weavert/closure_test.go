package weavert

import "testing"

// weaveEnv mirrors what codegen.envType generates in a real compiled
// program: a locally-declared slice type, distinct from any type
// declared in this package (see Call's doc comment on why reflection,
// not a shared named type, is what makes this work).
type weaveEnv []any

func addClosure(env weaveEnv, arg any) any {
	return env[0].(float64) + arg.(float64)
}

func TestNewClosureAndCall(t *testing.T) {
	env := weaveEnv{5.0}
	c := NewClosure(addClosure, env)
	got := Call(c, 3.0)
	if got != 8.0 {
		t.Errorf("Call = %v, want 8", got)
	}
}

func TestCall_NilArgument(t *testing.T) {
	identity := func(env weaveEnv, arg any) any { return arg }
	c := NewClosure(identity, weaveEnv{})
	if got := Call(c, nil); got != nil {
		t.Errorf("Call(c, nil) = %v, want nil", got)
	}
}

func TestCall_NonClosurePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected Call on a non-closure value to panic")
		}
	}()
	Call(5.0, 1.0)
}
