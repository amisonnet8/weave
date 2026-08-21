package weavert

import "testing"

func TestAdd_Numbers(t *testing.T) {
	if got := Add(1.0, 2.0); got != 3.0 {
		t.Errorf("Add(1, 2) = %v, want 3", got)
	}
}

func TestAdd_Strings(t *testing.T) {
	if got := Add("a", "b"); got != "ab" {
		t.Errorf(`Add("a", "b") = %v, want "ab"`, got)
	}
}

func TestAdd_MixedTypesPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected Add(1.0, \"b\") to panic")
		}
	}()
	Add(1.0, "b")
}

func TestSub(t *testing.T) {
	if got := Sub(5.0, 2.0); got != 3.0 {
		t.Errorf("Sub(5, 2) = %v, want 3", got)
	}
}

func TestSub_NonNumberPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected Sub(true, 1.0) to panic")
		}
	}()
	Sub(true, 1.0)
}

func TestMod_UsesFloatingPointModulo(t *testing.T) {
	if got := Mod(5.5, 2.0); got != 1.5 {
		t.Errorf("Mod(5.5, 2) = %v, want 1.5", got)
	}
}

func TestEq(t *testing.T) {
	if Eq(1.0, 1.0) != true {
		t.Error("Eq(1, 1) should be true")
	}
	if Eq(1.0, "1") != false {
		t.Error("Eq(1, \"1\") should be false (no implicit coercion)")
	}
	if Eq(nil, nil) != true {
		t.Error("Eq(nil, nil) should be true")
	}
}

func TestLt_Numbers(t *testing.T) {
	if Lt(1.0, 2.0) != true {
		t.Error("Lt(1, 2) should be true")
	}
	if Lt(2.0, 1.0) != false {
		t.Error("Lt(2, 1) should be false")
	}
}

func TestLt_Strings(t *testing.T) {
	if Lt("a", "b") != true {
		t.Error(`Lt("a", "b") should be true`)
	}
}

func TestLt_MixedTypesPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected Lt(1.0, \"b\") to panic")
		}
	}()
	Lt(1.0, "b")
}

func TestAnd_Or_Not(t *testing.T) {
	if And(true, false) != false {
		t.Error("And(true, false) should be false")
	}
	if Or(true, false) != true {
		t.Error("Or(true, false) should be true")
	}
	if Not(true) != false {
		t.Error("Not(true) should be false")
	}
}

func TestAnd_NonBoolPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected And(1.0, true) to panic")
		}
	}()
	And(1.0, true)
}
