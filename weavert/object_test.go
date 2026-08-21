package weavert

import "testing"

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
	fn := func(env []any, arg any) any { return arg }
	ObjSet(o, "f", NewClosure(fn, []any{}))

	if ObjGet(o, "n") != 1.0 || ObjGet(o, "s") != "hi" || ObjGet(o, "b") != true || ObjGet(o, "nilv") != nil {
		t.Error("expected all scalar values to round-trip unchanged")
	}
	if _, ok := ObjGet(o, "f").(*Closure); !ok {
		t.Error("expected the function-valued property to remain a *Closure")
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

func TestObjKeys_EmptyObject(t *testing.T) {
	got := ObjKeys(NewObject()).([]string)
	if len(got) != 0 {
		t.Errorf("ObjKeys(empty) = %v, want none", got)
	}
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
