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
