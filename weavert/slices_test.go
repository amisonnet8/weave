package weavert

import "testing"

func TestIntsToList(t *testing.T) {
	got := IntsToList([]int{1, 2, 3}).(Object)
	if len(got) != 3 {
		t.Fatalf("expected a 3-element list, got %v", got)
	}
	for i, want := range []float64{1, 2, 3} {
		if v := ObjAt(got, float64(i)); v != want {
			t.Errorf("list[%d] = %v, want %v (normalized to float64)", i, v, want)
		}
	}
}

func TestStringsToList(t *testing.T) {
	got := StringsToList([]string{"a", "b"}).(Object)
	if ObjAt(got, 0.0) != "a" || ObjAt(got, 1.0) != "b" {
		t.Errorf("StringsToList = %v, want {0:a, 1:b}", got)
	}
}

func TestBoolsToList(t *testing.T) {
	got := BoolsToList([]bool{true, false}).(Object)
	if ObjAt(got, 0.0) != true || ObjAt(got, 1.0) != false {
		t.Errorf("BoolsToList = %v, want {0:true, 1:false}", got)
	}
}

func TestFloat64sToList_EmptySlice(t *testing.T) {
	got := Float64sToList(nil).(Object)
	if len(got) != 0 {
		t.Errorf("Float64sToList(nil) = %v, want an empty list", got)
	}
}

func TestListToInts(t *testing.T) {
	l := NewObject()
	ObjSet(l, "0", 1.0)
	ObjSet(l, "1", 2.0)
	ObjSet(l, "2", 3.0)
	got := ListToInts(l)
	want := []int{1, 2, 3}
	if len(got) != len(want) {
		t.Fatalf("ListToInts = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ListToInts[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestListToInts_NonNumberElementPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected ListToInts to panic on a non-number element")
		}
	}()
	l := NewObject()
	ObjSet(l, "0", "not a number")
	ListToInts(l)
}

func TestListToStrings(t *testing.T) {
	l := NewObject()
	ObjSet(l, "0", "a")
	ObjSet(l, "1", "b")
	got := ListToStrings(l)
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("ListToStrings = %v, want [a b]", got)
	}
}

func TestListToStrings_NonStringElementPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected ListToStrings to panic on a non-string element")
		}
	}()
	l := NewObject()
	ObjSet(l, "0", 5.0)
	ListToStrings(l)
}

func TestListToBools(t *testing.T) {
	l := NewObject()
	ObjSet(l, "0", true)
	ObjSet(l, "1", false)
	got := ListToBools(l)
	if len(got) != 2 || got[0] != true || got[1] != false {
		t.Errorf("ListToBools = %v, want [true false]", got)
	}
}

// TestSliceListRoundTrip confirms XsToList/ListToXs are true inverses —
// converting a Go slice to a Weave list and back yields the original
// values (weave_spec.md §15.4).
func TestSliceListRoundTrip(t *testing.T) {
	original := []int{5, -3, 0, 42}
	list := IntsToList(original)
	back := ListToInts(list)
	if len(back) != len(original) {
		t.Fatalf("round-trip length = %d, want %d", len(back), len(original))
	}
	for i, want := range original {
		if back[i] != want {
			t.Errorf("round-trip[%d] = %v, want %v", i, back[i], want)
		}
	}
}
