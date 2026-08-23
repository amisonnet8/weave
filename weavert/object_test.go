package weavert

import (
	"strconv"
	"testing"
)

func TestObjGetSet(t *testing.T) {
	o := NewObject()
	ObjSet(o, "x", 1.0)
	if got := ObjGet(o, "x"); got != 1.0 {
		t.Errorf("ObjGet after ObjSet = %v, want 1", got)
	}
}

func TestObjGet_MissingKeyReturnsNil(t *testing.T) {
	o := NewObject()
	if got := ObjGet(o, "missing"); got != nil {
		t.Errorf("ObjGet(missing) = %v, want nil", got)
	}
}

func TestObjHas(t *testing.T) {
	o := NewObject()
	ObjSet(o, "x", 1.0)
	if ObjHas(o, "x") != true {
		t.Error("ObjHas(x) should be true")
	}
	if ObjHas(o, "y") != false {
		t.Error("ObjHas(y) should be false")
	}
}

func TestObjRemove(t *testing.T) {
	o := NewObject()
	ObjSet(o, "x", 1.0)
	ObjRemove(o, "x")
	if ObjHas(o, "x") != false {
		t.Error("expected x to be removed")
	}
	if got := ObjGet(o, "x"); got != nil {
		t.Errorf("ObjGet after remove = %v, want nil", got)
	}
}

func TestObjGet_NonObjectPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected ObjGet on a non-object to panic")
		}
	}()
	ObjGet(5.0, "x")
}

func TestObjGet_NonStringKeyPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected ObjGet with a non-string key to panic")
		}
	}()
	ObjGet(NewObject(), 5.0)
}

func TestObject_MixedValueTypes(t *testing.T) {
	o := NewObject()
	ObjSet(o, "n", 1.0)
	ObjSet(o, "s", "hi")
	ObjSet(o, "b", true)
	ObjSet(o, "nilv", nil)
	fn := func(arg any) any { return arg }
	ObjSet(o, "f", fn)

	if ObjGet(o, "n") != 1.0 || ObjGet(o, "s") != "hi" || ObjGet(o, "b") != true || ObjGet(o, "nilv") != nil {
		t.Error("expected all scalar values to round-trip unchanged")
	}
	if got := ObjGet(o, "f"); Call(got, 5.0) != 5.0 {
		t.Errorf("expected the function-valued property to remain callable, got %v calling it", got)
	}
}

func TestObjGet_WalksPrototypeChain(t *testing.T) {
	base := NewObject()
	ObjSet(base, "greeting", "hi")
	child := NewObject()
	ObjSet(child, protoKey, base)
	ObjSet(child, "name", "Alice")

	if got := ObjGet(child, "greeting"); got != "hi" {
		t.Errorf("ObjGet(child, greeting) = %v, want hi (inherited via __proto__)", got)
	}
	if got := ObjGet(child, "name"); got != "Alice" {
		t.Errorf("ObjGet(child, name) = %v, want Alice (own property)", got)
	}
}

func TestObjGet_OwnPropertyShadowsPrototype(t *testing.T) {
	base := NewObject()
	ObjSet(base, "x", "base-value")
	child := NewObject()
	ObjSet(child, protoKey, base)
	ObjSet(child, "x", "child-value")

	if got := ObjGet(child, "x"); got != "child-value" {
		t.Errorf("ObjGet(child, x) = %v, want child-value (own property wins)", got)
	}
}

func TestObjGet_MultiLevelPrototypeChain(t *testing.T) {
	grandparent := NewObject()
	ObjSet(grandparent, "tag", "gp")
	parent := NewObject()
	ObjSet(parent, protoKey, grandparent)
	child := NewObject()
	ObjSet(child, protoKey, parent)

	if got := ObjGet(child, "tag"); got != "gp" {
		t.Errorf("ObjGet(child, tag) = %v, want gp (two levels up)", got)
	}
}

func TestObjGet_MissingAtEndOfChainReturnsNil(t *testing.T) {
	base := NewObject()
	child := NewObject()
	ObjSet(child, protoKey, base)

	if got := ObjGet(child, "nope"); got != nil {
		t.Errorf("ObjGet(child, nope) = %v, want nil", got)
	}
}

func TestObjHasAndObjRemove_DoNotWalkPrototypeChain(t *testing.T) {
	base := NewObject()
	ObjSet(base, "x", 1.0)
	child := NewObject()
	ObjSet(child, protoKey, base)

	if ObjHas(child, "x") != false {
		t.Error("ObjHas must not see inherited properties (weave_spec.md §11: obj自身のみ)")
	}
	ObjRemove(child, "x") // no-op: child has no own "x" to remove
	if ObjGet(base, "x") != 1.0 {
		t.Error("ObjRemove on child must not affect the prototype's own property")
	}
}

func TestObjSet_NeverWritesToPrototype(t *testing.T) {
	base := NewObject()
	ObjSet(base, "x", 1.0)
	child := NewObject()
	ObjSet(child, protoKey, base)

	ObjSet(child, "x", 2.0)
	if ObjGet(base, "x") != 1.0 {
		t.Error("writing child.x must not mutate the prototype (weave_spec.md §4.2)")
	}
	if ObjHas(child, "x") != true {
		t.Error("expected child to now have its own x")
	}
}

func TestObjKeys_ExcludesProtoKeyAndSortsForDeterminism(t *testing.T) {
	base := NewObject()
	child := NewObject()
	ObjSet(child, protoKey, base)
	ObjSet(child, "b", 2.0)
	ObjSet(child, "a", 1.0)

	got := ObjKeys(child).([]string)
	want := []string{"a", "b"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("ObjKeys = %v, want %v (__proto__ excluded, sorted)", got, want)
	}
}

// TestObjKeys_ListPositionsSortNumerically confirms weave_spec.md §7's
// fix: a list(...)-shaped object with 10+ elements enumerates in true
// numeric order (not "10" before "2"), and each key ObjKeys returns is
// still the plain unpadded string a Weave program actually wrote — the
// zero-padding (listKey/normalizeKey) is purely an internal storage
// detail (weave_spec.md §3.1).
func TestObjKeys_ListPositionsSortNumerically(t *testing.T) {
	l := NewObject()
	for i := 0; i < 12; i++ {
		ObjSet(l, strconv.Itoa(i), float64(i))
	}
	got := ObjKeys(l).([]string)
	want := []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11"}
	if len(got) != len(want) {
		t.Fatalf("ObjKeys = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ObjKeys[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestKeyNormalization_UnpaddedAndPaddedFormsAgree confirms
// ObjGet/ObjSet/ObjHas/ObjRemove/ObjAt/ObjSetAt all resolve a list
// position to the same underlying storage slot, regardless of whether
// the caller spells the key as a plain digit string ("5"), an already-
// padded one ("0000000005"), or a numeric index (5.0 via ObjAt) —
// weave_spec.md §3.1's own point that padding is invisible from Weave.
func TestKeyNormalization_UnpaddedAndPaddedFormsAgree(t *testing.T) {
	l := NewObject()
	ObjSet(l, "5", "via unpadded ObjSet")

	if v := ObjAt(l, 5.0); v != "via unpadded ObjSet" {
		t.Errorf("ObjAt(l, 5.0) = %v, want the value set via ObjSet(l, \"5\", ...)", v)
	}
	if ObjHas(l, "5") != true {
		t.Error("ObjHas(l, \"5\") = false, want true")
	}
	if ObjHas(l, "0000000005") != true {
		t.Error("ObjHas(l, \"0000000005\") = false, want true (same position, padded spelling)")
	}

	ObjSetAt(l, 5.0, "via ObjSetAt")
	if v := ObjGet(l, "5"); v != "via ObjSetAt" {
		t.Errorf("ObjGet(l, \"5\") = %v, want the value ObjSetAt(l, 5.0, ...) just wrote", v)
	}

	ObjRemove(l, "5")
	if ObjHas(l, "0000000005") != false {
		t.Error("ObjHas(l, \"0000000005\") = true after ObjRemove(l, \"5\"), want false (same position)")
	}
}

func TestNormalizeKey(t *testing.T) {
	tests := []struct{ in, want string }{
		{"5", "0000000005"},
		{"0", "0000000000"},
		{"007", "0000000007"}, // pre-padded input still lands on the same key
		{"0000000005", "0000000005"},
		{"x", "x"},   // non-digit: untouched
		{"", ""},     // empty: untouched
		{"5x", "5x"}, // mixed: not all-digit, untouched
	}
	for _, tt := range tests {
		if got := normalizeKey(tt.in); got != tt.want {
			t.Errorf("normalizeKey(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestStripListKeyPadding(t *testing.T) {
	tests := []struct{ in, want string }{
		{"0000000005", "5"},
		{"0000000000", "0"},
		{"x", "x"}, // not exactly listKeyWidth digits: untouched
		{"5", "5"}, // too short to be a padded key: untouched
	}
	for _, tt := range tests {
		if got := stripListKeyPadding(tt.in); got != tt.want {
			t.Errorf("stripListKeyPadding(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestObjKeys_EmptyObject(t *testing.T) {
	got := ObjKeys(NewObject()).([]string)
	if len(got) != 0 {
		t.Errorf("ObjKeys(empty) = %v, want none", got)
	}
}

func TestObjAt(t *testing.T) {
	l := NewObject()
	ObjSet(l, "0", "first")
	ObjSet(l, "1", "second")

	if got := ObjAt(l, 0.0); got != "first" {
		t.Errorf("ObjAt(l, 0) = %v, want first", got)
	}
	if got := ObjAt(l, 1.0); got != "second" {
		t.Errorf("ObjAt(l, 1) = %v, want second", got)
	}
}

func TestObjAt_OutOfRangePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected ObjAt with an out-of-range index to panic")
		}
	}()
	ObjAt(NewObject(), 0.0)
}

func TestObjAt_NonNumberIndexPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected ObjAt with a non-number index to panic")
		}
	}()
	l := NewObject()
	ObjSet(l, "0", "first")
	ObjAt(l, "0")
}

func TestObjSetAt(t *testing.T) {
	l := NewObject()
	ObjSet(l, "0", "first")
	ObjSet(l, "1", "second")

	ObjSetAt(l, 1.0, "updated")
	if got := ObjAt(l, 1.0); got != "updated" {
		t.Errorf("ObjAt(l, 1) after ObjSetAt = %v, want updated", got)
	}
	if got := ObjAt(l, 0.0); got != "first" {
		t.Errorf("ObjAt(l, 0) = %v, want unchanged first", got)
	}
}

func TestObjSetAt_OutOfRangePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected ObjSetAt with an out-of-range index to panic")
		}
	}()
	ObjSetAt(NewObject(), 0.0, "x")
}

func TestObjSetAt_NonNumberIndexPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected ObjSetAt with a non-number index to panic")
		}
	}()
	l := NewObject()
	ObjSet(l, "0", "first")
	ObjSetAt(l, "0", "x")
}

func TestKeyAt(t *testing.T) {
	keys := ObjKeys(func() any {
		o := NewObject()
		ObjSet(o, "x", 1.0)
		return o
	}())
	if got := KeyAt(keys, 0.0); got != "x" {
		t.Errorf("KeyAt(keys, 0) = %v, want x", got)
	}
}
