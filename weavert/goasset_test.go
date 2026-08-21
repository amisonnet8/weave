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
