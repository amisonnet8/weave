package weavert

import "fmt"

// Every Weave object is represented as a Go map[string]any, boxed into
// `any` like every other Weave value (weave_spec.md §4; §14's own
// "MPTYPE ^string ^any" sketch). Object operations route through
// weavert instead of AMIVM's native MPTYPE/MPMAKE/MSET/MGET for the
// same reason arithmetic does (weavert/ops.go's package doc comment):
// Go doesn't allow map indexing on an `any`-typed variable, only on one
// statically typed as a map — and since every Weave variable is
// uniformly `^any` (CLAUDE.md's Step 2 "確定した設計判断"), that
// assertion has to happen somewhere regardless. Doing it here,
// uniformly, matches the same deliberate simplicity-over-native-fast-path
// choice Step 3 made for operators (see CLAUDE.md's Step 3 "確定した
// 設計判断") — and unlike closures (Step 5), objects need no
// AMIVM-visible type at all, since every operation on one is a plain
// weavert.* CALL: there's nothing here for a SLMAKE-style deftype
// restriction to even trip over.

// Object is the concrete Go representation of a Weave object
// (weave_spec.md §4.1).
type Object map[string]any

// NewObject creates an empty object (weave_spec.md §3's `{ }`).
func NewObject() any {
	return Object{}
}

func objOf(v any) Object {
	o, ok := v.(Object)
	if !ok {
		panic(fmt.Sprintf("weave: value is not an object, got %T", v))
	}
	return o
}

func keyOf(v any) string {
	s, ok := v.(string)
	if !ok {
		panic(fmt.Sprintf("weave: property name must be a string, got %T", v))
	}
	return s
}

// ObjGet implements property reads `obj.name` (weave_spec.md §4.1),
// restricted through Step 6 to an object's own properties — reading a
// name the object doesn't have returns nil, exactly like a plain Go map
// index would (§4.2's prototype-chain walk-up on a miss arrives in
// Step 7).
func ObjGet(obj any, key any) any {
	return objOf(obj)[keyOf(key)]
}

// ObjSet implements property writes `obj.name = value`, always applied
// to obj itself (weave_spec.md §4.2: a write never touches a
// prototype).
func ObjSet(obj any, key any, val any) any {
	objOf(obj)[keyOf(key)] = val
	return nil
}

// ObjHas implements the `has` builtin (weave_spec.md §11): does obj
// itself (not a prototype) have this property.
func ObjHas(obj any, key any) any {
	_, ok := objOf(obj)[keyOf(key)]
	return ok
}

// ObjRemove implements the `remove` builtin (weave_spec.md §11):
// delete a property from obj itself.
func ObjRemove(obj any, key any) any {
	delete(objOf(obj), keyOf(key))
	return nil
}
