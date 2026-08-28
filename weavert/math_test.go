package weavert

import "testing"

func TestFloorCeilRound(t *testing.T) {
	if got := Floor(1.7); got != 1.0 {
		t.Errorf("Floor(1.7) = %v, want 1", got)
	}
	if got := Ceil(1.2); got != 2.0 {
		t.Errorf("Ceil(1.2) = %v, want 2", got)
	}
	if got := Round(1.5); got != 2.0 {
		t.Errorf("Round(1.5) = %v, want 2", got)
	}
}

func TestAbs(t *testing.T) {
	if got := Abs(-3.5); got != 3.5 {
		t.Errorf("Abs(-3.5) = %v, want 3.5", got)
	}
	if got := Abs(3.5); got != 3.5 {
		t.Errorf("Abs(3.5) = %v, want 3.5", got)
	}
}

func TestAbs_NonNumberPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected Abs(\"x\") to panic")
		}
	}()
	Abs("x")
}

func TestSqrt(t *testing.T) {
	if got := Sqrt(9.0); got != 3.0 {
		t.Errorf("Sqrt(9) = %v, want 3", got)
	}
}

func TestSqrt_NegativeArgumentPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected Sqrt(-1) to panic")
		}
	}()
	Sqrt(-1.0)
}

func TestMinMax(t *testing.T) {
	if got := Min(3.0, 5.0); got != 3.0 {
		t.Errorf("Min(3, 5) = %v, want 3", got)
	}
	if got := Max(3.0, 5.0); got != 5.0 {
		t.Errorf("Max(3, 5) = %v, want 5", got)
	}
}

func TestPow(t *testing.T) {
	if got := Pow(2.0, 10.0); got != 1024.0 {
		t.Errorf("Pow(2, 10) = %v, want 1024", got)
	}
}

func TestPow_NonNumberPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected Pow(\"x\", 2) to panic")
		}
	}()
	Pow("x", 2.0)
}
