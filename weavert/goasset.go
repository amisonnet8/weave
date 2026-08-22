package weavert

import (
	"fmt"
	"reflect"
)

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
	return NormalizeGoValue(out[0].Interface())
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
	default:
		return v
	}
}
