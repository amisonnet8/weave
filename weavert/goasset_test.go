package weavert

import (
	"errors"
	"os"
	"path/filepath"
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

// TestCallGoFunc_ValueErrorIdiom_Success exercises the real motivating
// case (Go's `os.ReadFile(name string) ([]byte, error)`): on success,
// only the leading value comes back (normalized from []byte to a plain
// Weave string, NormalizeGoValue's own job) — the trailing nil error is
// dropped, not panicked on.
func TestCallGoFunc_ValueErrorIdiom_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(path, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := CallGoFunc(os.ReadFile, path)
	s, ok := got.(string)
	if !ok {
		t.Fatalf("CallGoFunc(os.ReadFile, ...) = %v (%T), want string", got, got)
	}
	if s != "hi" {
		t.Errorf("got %q, want %q", s, "hi")
	}
}

// TestCallGoFunc_ValueErrorIdiom_ErrorPanics exercises the failure half
// of the same idiom: a non-nil trailing error panics with a message
// derived from the error itself, rather than being silently dropped.
func TestCallGoFunc_ValueErrorIdiom_ErrorPanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected CallGoFunc to panic on a non-nil trailing error")
		}
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, "no such file") {
			t.Errorf("panic value = %v, want a message mentioning the real os error", r)
		}
	}()
	CallGoFunc(os.ReadFile, filepath.Join(t.TempDir(), "does-not-exist.txt"))
}

// TestCallGoFunc_TwoNonErrorValuesStillReturnsFirst documents the
// narrower, still-open limitation: a multi-value return whose last
// value is *not* an error is left alone — only the first value is ever
// visible to Weave, silently.
func TestCallGoFunc_TwoNonErrorValuesStillReturnsFirst(t *testing.T) {
	got := CallGoFunc(func() (int, int) { return 1, 2 })
	if got != 1.0 {
		t.Errorf("CallGoFunc(...) = %v, want 1.0 (the first of two non-error return values)", got)
	}
}

func TestCallGoMethod_ValueErrorIdiom(t *testing.T) {
	r := &namedErrorMethod{}
	defer func() {
		if recover() == nil {
			t.Fatal("expected CallGoMethod to panic on a non-nil trailing error")
		}
	}()
	CallGoMethod(r, "Fail")
}

type namedErrorMethod struct{}

func (namedErrorMethod) Fail() (string, error) { return "", errors.New("boom") }

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
		{"[]byte becomes string", []byte("hi"), "hi"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeGoValue(tt.in); got != tt.want {
				t.Errorf("NormalizeGoValue(%v) = %v (%T), want %v (%T)", tt.in, got, got, tt.want, tt.want)
			}
		})
	}
}
