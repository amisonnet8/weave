package weavert

import (
	"strings"
	"testing"
)

func TestCallGoMethod_InvokesRealMethod(t *testing.T) {
	r := strings.NewReader("hello")
	got := CallGoMethod(r, "Len")
	if got != 5.0 {
		t.Fatalf("CallGoMethod(r, Len) = %v, want 5.0 (normalized from int)", got)
	}
}

func TestCallGoMethod_UnknownMethodPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected CallGoMethod with an unknown method to panic")
		}
	}()
	CallGoMethod(strings.NewReader("hi"), "NoSuchMethod")
}

func TestCallGoFunc_InvokesRealFunction(t *testing.T) {
	got := CallGoFunc(strings.NewReader, "hello")
	r, ok := got.(*strings.Reader)
	if !ok {
		t.Fatalf("CallGoFunc(strings.NewReader, ...) = %v (%T), want *strings.Reader", got, got)
	}
	if r.Len() != 5 {
		t.Errorf("r.Len() = %d, want 5", r.Len())
	}
}

func TestCallGoFunc_NormalizesNumericResult(t *testing.T) {
	got := CallGoFunc(func(s string) int { return len(s) }, "hello")
	if got != 5.0 {
		t.Fatalf("CallGoFunc(...) = %v (%T), want 5.0 (normalized from int)", got, got)
	}
}

func TestTypeError_PanicsWithClearMessage(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected TypeError to panic")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("panic value = %v (%T), want a string", r, r)
		}
		for _, want := range []string{"argument 1 to strings.Reader.Len", "^*strings.Reader", "float64"} {
			if !strings.Contains(msg, want) {
				t.Errorf("panic message %q missing %q", msg, want)
			}
		}
	}()
	TypeError("argument 1 to strings.Reader.Len", "^*strings.Reader", 42.0)
}

func TestIsWeaveFunc(t *testing.T) {
	tests := []struct {
		name string
		v    any
		want bool
	}{
		{"a weave closure", func(a any) any { return a }, true},
		{"a differently-shaped func value is still Kind Func", func() {}, true},
		{"number", 1.0, false},
		{"string", "hi", false},
		{"nil", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsWeaveFunc(tt.v); got != tt.want {
				t.Errorf("IsWeaveFunc(%v) = %v, want %v", tt.v, got, tt.want)
			}
		})
	}
}

func TestNormalizeGoValue(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want any
	}{
		{"int", int(5), 5.0},
		{"int32", int32(5), 5.0},
		{"uint64", uint64(5), 5.0},
		{"float32", float32(1.5), 1.5},
		{"string unchanged", "hi", "hi"},
		{"bool unchanged", true, true},
		{"nil unchanged", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeGoValue(tt.in); got != tt.want {
				t.Errorf("NormalizeGoValue(%v) = %v (%T), want %v (%T)", tt.in, got, got, tt.want, tt.want)
			}
		})
	}
}
