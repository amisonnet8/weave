package weavert

import "testing"

func TestToString(t *testing.T) {
	tests := []struct {
		name string
		v    any
		want string
	}{
		{"nil", nil, "nil"},
		{"string", "hi", "hi"},
		{"true", true, "true"},
		{"false", false, "false"},
		{"whole number", 5.0, "5"},
		{"fractional number", 1.5, "1.5"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ToString(tt.v); got != tt.want {
				t.Errorf("ToString(%v) = %q, want %q", tt.v, got, tt.want)
			}
		})
	}
}

func TestLen_Object(t *testing.T) {
	o := NewObject()
	ObjSet(o, "x", 1.0)
	ObjSet(o, "y", 2.0)
	if got := Len(o); got != 2.0 {
		t.Errorf("Len(object) = %v, want 2", got)
	}
}

func TestLen_String(t *testing.T) {
	if got := Len("hello"); got != 5.0 {
		t.Errorf("Len(hello) = %v, want 5", got)
	}
}

func TestLen_StringCountsRunesNotBytes(t *testing.T) {
	if got := Len("日本語"); got != 3.0 {
		t.Errorf("Len(日本語) = %v, want 3 (rune count, not byte count)", got)
	}
}

func TestLen_InvalidTypePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected Len(5.0) to panic")
		}
	}()
	Len(5.0)
}
