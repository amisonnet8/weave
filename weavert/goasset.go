package weavert

import (
	"fmt"
	"reflect"
)

// CallGoFuncList implements an untyped (no goReturns(...)/goParams(...))
// gofunc(...)-declared call (weave_spec.md §15.2) via reflection: fn is
// the real Go function passed as a value (amivm's `value` operand
// category accepts a bare `?pkg.Func` token directly, per
// amivm_spec.md §5 — see internal/codegen/goasset.go's genGoFuncCall),
// invoked with args coerced to each parameter's real type via
// reflect.Value.Convert (Weave's numbers are always float64, so the
// only conversions that ever actually happen are float64->int and
// similar well-defined numeric narrowings/widenings — never the
// int->string rune-conversion trap Seed/Cascade documented, since a
// float64->string Convert simply isn't legal in Go and fails loudly
// instead of silently doing the wrong thing).
//
// Every Go function/method call — typed or not — now always returns a
// Weave list (weave_spec.md §15.2's "常にlist" rule, a from-scratch
// redesign of the old single-scalar-or-(value,error) behavior — see
// CLAUDE.md's design-decision note on why): this collects ALL of fn's
// actual return values, in order, each run through NormalizeGoValue,
// with no special detection of Go's `(value, error)` idiom at all — the
// caller decides what to do with whatever sits at the last position via
// the ordinary `at(...)`/`raiseIfError(...)` builtins, exactly like any
// other list element.
func CallGoFuncList(fn any, args ...any) any {
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

	return newList(fv.Call(argVals))
}

// CallGoMethodList implements an untyped gomethod(...)-declared method
// call (weave_spec.md §15.1/§16) via reflection: target is a Go value
// returned by some earlier gofunc call (an ordinary Weave `any`), and
// methodName is the *real* Go method name gomethod(...) named, already
// resolved at compile time (internal/codegen/goasset.go's
// genGoMethodCall — no dynamic prototype-chain search, per §16).
// Same "always a list" landing point as CallGoFuncList above — see its
// own doc comment for why there's no separate (value, error) handling
// here any more.
func CallGoMethodList(target any, methodName string, args ...any) any {
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

	return newList(m.Call(argVals))
}

// newList boxes a reflect-obtained return-value slice into a Weave list
// (weave_spec.md §3 — the same weavert.Object every list(...) literal
// and every typed Go-asset call (internal/codegen/goasset.go's
// genBoxList) produces), normalizing each element individually via
// NormalizeGoValue.
func newList(out []reflect.Value) any {
	list := Object{}
	for i, v := range out {
		list[listKey(i)] = NormalizeGoValue(v.Interface())
	}
	return list
}

// RaiseIfError implements the `raiseIfError(...)` builtin (weave_spec.md
// §11/§15.4): if v holds a non-nil value implementing Go's built-in
// `error` interface, panics with a clear message; otherwise (nil, or any
// non-error value) it's a no-op. This is the explicit, composable
// replacement for the old goError(...)-declared automatic panic — under
// the "every Go call returns a list" design, error-checking is no
// longer folded into the type declaration itself; the caller extracts
// the position it expects an error at (via `at(...)`) and decides
// whether/when to check it, exactly like any other list element.
func RaiseIfError(v any) any {
	if err, ok := v.(error); ok && err != nil {
		panic(fmt.Sprintf("weave: %v", err))
	}
	return nil
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
// to ever actually be called — unlike CallGoMethodList/CallGoFuncList's
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
// Both CallGoMethodList/CallGoFuncList (via newList, above) and
// genBoxList's native path (internal/codegen/goasset.go) route their
// per-element results through this.
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
