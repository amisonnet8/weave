package weavert

import "fmt"

// numeric is every Go numeric kind a "?[]T" slice type hint
// (weave_spec.md §15.4) can name, other than byte/uint8 (which keeps
// its own dedicated →string conversion — see object.go's listKey
// neighbors and internal/codegen/goasset.go's nativeReturnConversion).
type numeric interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint16 | ~uint32 | ~uint64 |
		~float32 | ~float64
}

// numericSliceToList and the concrete IntsToList/Int8sToList/...
// functions below implement the return side of a general "?[]T" slice
// type hint (weave_spec.md §15.4, T other than byte/uint8): boxing a
// real Go slice into a Weave list (weave_spec.md §3), one element per
// position, each converted to float64 the same way an ordinary scalar
// numeric return already is (NormalizeGoValue).
//
// internal/codegen/goasset.go's nativeReturnConversion picks the
// concrete function name (IntsToList, StringsToList, ...) at compile
// time, from the "?[]T" hint string itself — T is always statically
// known, so there is deliberately no single "any slice" entry point
// here doing runtime dispatch (e.g. a Go type switch): a type switch
// only matches a value's *exact* dynamic type, and the raw slice value
// reaching this boundary may have passed through a named SLTYPE-backed
// stand-in type (goTypeToken) rather than the literal `[]int` a type
// switch's `case []int:` would require — see genNativeGoMethodCall's
// own doc comment for the full reasoning (the same issue METHOD itself
// was introduced to solve, one level up, for the method-value
// extraction step). Each function below instead takes its slice as a
// *concretely typed parameter*, so passing a named stand-in argument
// into it is resolved by Go's assignability rules (which don't care
// about naming), never identity.
func numericSliceToList[T numeric](s []T) any {
	list := Object{}
	for i, x := range s {
		list[listKey(i)] = float64(x)
	}
	return list
}

func IntsToList(s []int) any         { return numericSliceToList(s) }
func Int8sToList(s []int8) any       { return numericSliceToList(s) }
func Int16sToList(s []int16) any     { return numericSliceToList(s) }
func Int32sToList(s []int32) any     { return numericSliceToList(s) }
func Int64sToList(s []int64) any     { return numericSliceToList(s) }
func UintsToList(s []uint) any       { return numericSliceToList(s) }
func Uint16sToList(s []uint16) any   { return numericSliceToList(s) }
func Uint32sToList(s []uint32) any   { return numericSliceToList(s) }
func Uint64sToList(s []uint64) any   { return numericSliceToList(s) }
func Float32sToList(s []float32) any { return numericSliceToList(s) }
func Float64sToList(s []float64) any { return numericSliceToList(s) }

func StringsToList(s []string) any {
	list := Object{}
	for i, x := range s {
		list[listKey(i)] = x
	}
	return list
}

func BoolsToList(s []bool) any {
	list := Object{}
	for i, x := range s {
		list[listKey(i)] = x
	}
	return list
}

// listToNumericSlice and the concrete ListToInts/ListToInt8s/... below
// implement the parameter side of a general "?[]T" slice type hint —
// converting a Weave list back into a real Go []T, one validated
// element at a time (a plain type assertion per element, panicking
// with a clear message on the first mismatch — no reflect, same spirit
// as genAssertOrTypeError's own ASSERT-based checks elsewhere on the
// typed path). Each function's return type is the genuine, unnamed
// []T Go itself infers — internal/codegen/goasset.go's
// genAssertGeneralSliceParam only needs a named SLTYPE-backed stand-in
// (goTypeToken) to declare the *AMIVM* VAR receiving this value (AMIVM
// has no literal slice type token at all), never to call this function
// itself — the call argument (a Weave `any`) doesn't care about that
// either way.
func listToNumericSlice[T numeric](v any) []T {
	o := objOf(v)
	n := len(o)
	out := make([]T, n)
	for i := 0; i < n; i++ {
		elem := ObjAt(o, float64(i))
		f, ok := elem.(float64)
		if !ok {
			panic(fmt.Sprintf("weave: list element %d is not a number, got %T", i, elem))
		}
		out[i] = T(f)
	}
	return out
}

func ListToInts(v any) []int         { return listToNumericSlice[int](v) }
func ListToInt8s(v any) []int8       { return listToNumericSlice[int8](v) }
func ListToInt16s(v any) []int16     { return listToNumericSlice[int16](v) }
func ListToInt32s(v any) []int32     { return listToNumericSlice[int32](v) }
func ListToInt64s(v any) []int64     { return listToNumericSlice[int64](v) }
func ListToUints(v any) []uint       { return listToNumericSlice[uint](v) }
func ListToUint16s(v any) []uint16   { return listToNumericSlice[uint16](v) }
func ListToUint32s(v any) []uint32   { return listToNumericSlice[uint32](v) }
func ListToUint64s(v any) []uint64   { return listToNumericSlice[uint64](v) }
func ListToFloat32s(v any) []float32 { return listToNumericSlice[float32](v) }
func ListToFloat64s(v any) []float64 { return listToNumericSlice[float64](v) }

func ListToStrings(v any) []string {
	o := objOf(v)
	n := len(o)
	out := make([]string, n)
	for i := 0; i < n; i++ {
		elem := ObjAt(o, float64(i))
		s, ok := elem.(string)
		if !ok {
			panic(fmt.Sprintf("weave: list element %d is not a string, got %T", i, elem))
		}
		out[i] = s
	}
	return out
}

func ListToBools(v any) []bool {
	o := objOf(v)
	n := len(o)
	out := make([]bool, n)
	for i := 0; i < n; i++ {
		elem := ObjAt(o, float64(i))
		b, ok := elem.(bool)
		if !ok {
			panic(fmt.Sprintf("weave: list element %d is not a bool, got %T", i, elem))
		}
		out[i] = b
	}
	return out
}
