package weavert

import (
	"fmt"
	"reflect"
)

// errorType is the reflect.Type of Go's built-in `error` interface —
// used to detect the common `(value, error)` Go return-value idiom
// (CallGoMethod/CallGoFunc's own doc comments).
var errorType = reflect.TypeOf((*error)(nil)).Elem()

// panicIfTrailingError implements the `(value, error)` idiom's single
// rule: if fn/method's declared return types end in a genuine `error`
// (checked via the *type*, not just how many values came back — a
// function that legitimately returns two non-error values must not be
// misread as one), and that slot actually holds a non-nil error at this
// call, panic with it. weave_spec.md has no multi-value concept, so
// there is no way to hand both the value and the error back to Weave
// code — panicking on a non-nil error and returning just the leading
// value otherwise is the closest single-value equivalent, and matches
// how many single-return-value host languages bridge this exact Go
// idiom. A 2+-value return whose last type is *not* `error` (rare, but
// real — e.g. `(int, int)`) is left alone here; only its first value is
// ever visible to Weave (a pre-existing, narrower limitation: weave_spec.md
// never addresses non-error multi-value returns at all).
func panicIfTrailingError(outTypes []reflect.Type, out []reflect.Value) {
	if len(out) < 2 || !outTypes[len(outTypes)-1].Implements(errorType) {
		return
	}
	if errVal := out[len(out)-1].Interface(); errVal != nil {
		panic(fmt.Sprintf("weave: %v", errVal))
	}
}

// CallGoMethod implements a gomethod(...)-declared method call
// (weave_spec.md §15.1, §16): target is a Go value returned by some
// gofunc call (an ordinary Weave `any`, holding whatever the wrapped Go
// function actually returned — no wrapper struct needed, see Step 3's
// genGoFuncCall), and methodName is the *real* Go method name
// gomethod(...) named, already resolved at compile time by
// internal/codegen/goasset.go (no dynamic prototype-chain search, per
// §16 — CLAUDE.md's 後半 Step 4 "確定した設計判断" has the full
// reasoning for why this still needs reflection despite that: target's
// static Go type isn't known to the *generated* code, only to codegen
// itself, so invoking it can't be a literal Go method-call expression).
func CallGoMethod(target any, methodName string, args ...any) any {
	v := reflect.ValueOf(target)
	m := v.MethodByName(methodName)
	if !m.IsValid() {
		panic(fmt.Sprintf("weave: %T has no method %s", target, methodName))
	}
	mType := m.Type()

	argVals := make([]reflect.Value, len(args))
	for i, a := range args {
		if a == nil {
			argVals[i] = reflect.Zero(mType.In(i))
		} else {
			argVals[i] = reflect.ValueOf(a)
		}
	}

	out := m.Call(argVals)
	if len(out) == 0 {
		return nil
	}
	outTypes := make([]reflect.Type, mType.NumOut())
	for i := range outTypes {
		outTypes[i] = mType.Out(i)
	}
	panicIfTrailingError(outTypes, out)
	return NormalizeGoValue(out[0].Interface())
}

// CallGoFunc implements a direct call to a gofunc(...)-declared Go
// function (weave_spec.md §16), invoked via reflection instead of a
// literal Go call expression. internal/codegen/goasset.go's genGoFuncCall
// used to emit a literal `CALL raw : ?pkg.Func arg1 arg2` — which works
// when every argument is a literal Weave constant (an untyped Go
// constant is assignable to whatever concrete parameter type the real
// function wants), but fails go/types the moment an argument is an
// ordinary `any`-typed Weave value (a variable, an object property, ...:
// `cannot use v (variable of type any) as string value`, caught running
// a spawn/gofunc integration example through the real pipeline — see
// CLAUDE.md's 後半 Step 5 "確定した設計判断"). fn is the real Go
// function passed as a value (amivm's `value` operand category accepts
// a bare `?pkg.Func` token, per amivm_spec.md §5 — unlike a `CALL`
// callname, this needs no wrapping), so this can use the exact same
// reflection-based argument coercion CallGoMethod already established
// for the same underlying reason (target's static Go type only exists
// at codegen time, never in the generated source itself).
//
// A wrapped function returning Go's common `(value, error)` shape (e.g.
// os.ReadFile) is handled via panicIfTrailingError: a non-nil error
// panics, otherwise only the leading value is returned to Weave — see
// its own doc comment for the full reasoning.
func CallGoFunc(fn any, args ...any) any {
	fv := reflect.ValueOf(fn)
	fType := fv.Type()

	argVals := make([]reflect.Value, len(args))
	for i, a := range args {
		if a == nil {
			argVals[i] = reflect.Zero(fType.In(i))
		} else {
			argVals[i] = reflect.ValueOf(a).Convert(fType.In(i))
		}
	}

	out := fv.Call(argVals)
	if len(out) == 0 {
		return nil
	}
	outTypes := make([]reflect.Type, fType.NumOut())
	for i := range outTypes {
		outTypes[i] = fType.Out(i)
	}
	panicIfTrailingError(outTypes, out)
	return NormalizeGoValue(out[0].Interface())
}

// TypeError panics with a clear, Weave-flavored message for a failed
// static Go type check (internal/codegen/goasset.go's genAssertOrTypeError)
// — the one place a typed gotype/gomethod/gofunc declaration's ASSERT
// still needs a helper at all: everywhere else on that path is fully
// native (ASSERT/FGET/CALL), but formatting a message that includes the
// mismatched value's own dynamic type (%T) can't be done with a fixed
// set of native instructions. desc names where the check happened (e.g.
// "argument 1 to strings.Reader.Len"), want is the AMIVM type token that
// was expected (e.g. "^*strings.Reader"), and got is the value that
// failed to match it.
func TypeError(desc, want string, got any) {
	panic(fmt.Sprintf("weave: %s: expected %s, got %T", desc, want, got))
}

// IsWeaveFunc reports whether v holds a Weave closure — every closure is
// a bare Go func(any) any value (closure.go's genFuncLit), which Go's
// own interface type assertion can't check directly: an unnamed func
// type never satisfies an assertion to any named type, regardless of
// its signature (internal/codegen/shape.go's genCheckShapeCall explains
// why in full). reflect.Kind() sidesteps that without needing the value
// to ever actually be called — unlike CallGoMethod/CallGoFunc's
// reflection, there's no equivalent "reflect tax" here, just one cheap
// Kind() check. Used by checkShape(...)'s "function" type hint.
func IsWeaveFunc(v any) bool {
	if v == nil {
		return false
	}
	return reflect.TypeOf(v).Kind() == reflect.Func
}

// NormalizeGoValue converts a raw Go numeric result to Weave's own
// unified number representation (weave_spec.md §2: every Weave number
// is a float64, integers and floats aren't distinguished — CLAUDE.md's
// Step 2 "確定した設計判断"). Without this, a Go asset call returning a
// native `int` (e.g. `(*strings.Reader).Len() int`) would silently
// break any later Weave-native operation expecting float64 (arithmetic,
// comparisons, main's own exit code) — caught by running
// examples/gomethods.weave through the full pipeline (see CLAUDE.md's
// 後半 Step 4 "確定した設計判断"). Strings, bools, float64 itself,
// nil, and any Go struct/pointer (returned as-is per §15.2, since it
// has no Weave equivalent to convert to) all pass through unchanged.
// Both CallGoMethod and genGoFuncCall's direct native calls
// (internal/codegen/goasset.go) route their results through this.
func NormalizeGoValue(v any) any {
	switch x := v.(type) {
	case int:
		return float64(x)
	case int8:
		return float64(x)
	case int16:
		return float64(x)
	case int32:
		return float64(x)
	case int64:
		return float64(x)
	case uint:
		return float64(x)
	case uint8:
		return float64(x)
	case uint16:
		return float64(x)
	case uint32:
		return float64(x)
	case uint64:
		return float64(x)
	case float32:
		return float64(x)
	case []byte:
		// Weave has no separate "byte slice" concept (weave_spec.md §2's
		// only textual value is string) — []byte is common enough as a
		// return type (os.ReadFile, io.ReadAll, ...) that converting it
		// via Go's own string(b) is the natural bridge, exactly like the
		// numeric cases above convert to Weave's own unified number type.
		return string(x)
	default:
		return v
	}
}
