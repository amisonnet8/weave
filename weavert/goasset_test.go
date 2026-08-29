package weavert

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCallGoMethodList_InvokesRealMethodAndBoxesIntoAList(t *testing.T) {
	r := strings.NewReader("hello")
	got := CallGoMethodList(r, "Len")
	list, ok := got.(Object)
	if !ok {
		t.Fatalf("CallGoMethodList(...) = %v (%T), want Object", got, got)
	}
	if len(list) != 1 {
		t.Fatalf("expected a 1-element list, got %v", list)
	}
	if v := ObjAt(list, 0.0); v != 5.0 {
		t.Errorf("list[0] = %v, want 5.0 (normalized from int)", v)
	}
}

func TestCallGoMethodList_UnknownMethodPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected CallGoMethodList with an unknown method to panic")
		}
	}()
	CallGoMethodList(strings.NewReader("hi"), "NoSuchMethod")
}

func TestCallGoFuncList_InvokesRealFunctionAndBoxesIntoAList(t *testing.T) {
	got := CallGoFuncList(strings.NewReader, "hello")
	list, ok := got.(Object)
	if !ok {
		t.Fatalf("CallGoFuncList(...) = %v (%T), want Object", got, got)
	}
	v := ObjAt(list, 0.0)
	r, ok := v.(*strings.Reader)
	if !ok {
		t.Fatalf("list[0] = %v (%T), want *strings.Reader", v, v)
	}
	if r.Len() != 5 {
		t.Errorf("r.Len() = %d, want 5", r.Len())
	}
}

func TestCallGoFuncList_NormalizesNumericResult(t *testing.T) {
	got := CallGoFuncList(func(s string) int { return len(s) }, "hello")
	list := got.(Object)
	if v := ObjAt(list, 0.0); v != 5.0 {
		t.Fatalf("list[0] = %v (%T), want 5.0 (normalized from int)", v, v)
	}
}

// TestCallGoFuncList_ValueErrorIdiom exercises the real motivating case
// (Go's `os.ReadFile(name string) ([]byte, error)`): every return value
// comes back as its own list element now — no automatic error-panicking,
// no dropping of the trailing error — weave_spec.md §14.2's "常にlist"
// rule means the caller decides what to do with each position via
// `at(...)`/`raiseIfError(...)`.
func TestCallGoFuncList_ValueErrorIdiom_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(path, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := CallGoFuncList(os.ReadFile, path).(Object)
	if len(got) != 2 {
		t.Fatalf("expected a 2-element list (value, error), got %v", got)
	}
	s, ok := ObjAt(got, 0.0).(string)
	if !ok || s != "hi" {
		t.Errorf("list[0] = %v (%T), want \"hi\" (normalized from []byte)", ObjAt(got, 0.0), ObjAt(got, 0.0))
	}
	if v := ObjAt(got, 1.0); v != nil {
		t.Errorf("list[1] = %v, want nil (no error)", v)
	}
}

func TestCallGoFuncList_ValueErrorIdiom_ErrorIsJustAListElement(t *testing.T) {
	got := CallGoFuncList(os.ReadFile, filepath.Join(t.TempDir(), "does-not-exist.txt")).(Object)
	if len(got) != 2 {
		t.Fatalf("expected a 2-element list (value, error), got %v", got)
	}
	if v := ObjAt(got, 0.0); v != "" {
		t.Errorf("list[0] = %v, want \"\" (zero value on failure)", v)
	}
	err, ok := ObjAt(got, 1.0).(error)
	if !ok || err == nil || !strings.Contains(err.Error(), "no such file") {
		t.Errorf("list[1] = %v (%T), want a real os error mentioning \"no such file\"", ObjAt(got, 1.0), ObjAt(got, 1.0))
	}
}

// TestCallGoFuncList_MultipleNonErrorValues confirms every return value
// (not just the first) survives now — the old "only the first of a
// non-error multi-value return is visible" limitation is gone under the
// always-list design.
func TestCallGoFuncList_MultipleNonErrorValues(t *testing.T) {
	got := CallGoFuncList(func() (int, int) { return 1, 2 }).(Object)
	if ObjAt(got, 0.0) != 1.0 || ObjAt(got, 1.0) != 2.0 {
		t.Errorf("CallGoFuncList(...) = %v, want {0:1.0, 1:2.0}", got)
	}
}

func TestCallGoMethodList_ValueErrorIdiom(t *testing.T) {
	r := &namedErrorMethod{}
	got := CallGoMethodList(r, "Fail").(Object)
	if len(got) != 2 {
		t.Fatalf("expected a 2-element list (value, error), got %v", got)
	}
	err, ok := ObjAt(got, 1.0).(error)
	if !ok || err == nil || err.Error() != "boom" {
		t.Errorf("list[1] = %v (%T), want an error \"boom\"", ObjAt(got, 1.0), ObjAt(got, 1.0))
	}
}

type namedErrorMethod struct{}

func (namedErrorMethod) Fail() (string, error) { return "", errors.New("boom") }

func TestRaiseIfError_NonNilErrorPanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected RaiseIfError to panic on a non-nil error")
		}
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, "boom") {
			t.Errorf("panic value = %v, want a message mentioning the error text", r)
		}
	}()
	RaiseIfError(errors.New("boom"))
}

func TestRaiseIfError_NilIsANoOp(t *testing.T) {
	if got := RaiseIfError(nil); got != nil {
		t.Errorf("RaiseIfError(nil) = %v, want nil", got)
	}
}

func TestRaiseIfError_NonErrorValueIsANoOp(t *testing.T) {
	if got := RaiseIfError("not an error"); got != nil {
		t.Errorf("RaiseIfError(non-error) = %v, want nil (silently ignored)", got)
	}
	if got := RaiseIfError(42.0); got != nil {
		t.Errorf("RaiseIfError(42.0) = %v, want nil", got)
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
