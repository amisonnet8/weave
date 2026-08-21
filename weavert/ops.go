package weavert

import (
	"fmt"
	"math"
	"strings"
)

// Every Weave value is a Go `any` (weave_spec.md §2; CLAUDE.md's Weave特有
// の設計課題 1), and Go does not allow arithmetic operators directly on
// `any`-typed operands even when their underlying values would support
// it — so every Weave operator (weave_spec.md §8) is implemented here as
// a runtime-dispatching function rather than a native AMIVM instruction
// (ADD/SUB/...), which only accepts concretely-typed operands. The
// "compile a static fast path when both operand types are known"
// optimization §14 sketches is deliberately deferred — see CLAUDE.md's
// Step 3 "確定した設計判断".

// Add implements `+`: numeric addition if both operands are numbers,
// string concatenation if both are strings. Any other combination is a
// runtime type error — Weave does not coerce between numbers and
// strings implicitly (use string()).
func Add(a, b any) any {
	if an, ok := a.(float64); ok {
		if bn, ok := b.(float64); ok {
			return an + bn
		}
		panic(binOpError("+", a, b))
	}
	if as, ok := a.(string); ok {
		if bs, ok := b.(string); ok {
			return as + bs
		}
		panic(binOpError("+", a, b))
	}
	panic(binOpError("+", a, b))
}

// Sub, Mul, Div, Mod implement `- * / %`: numeric-only arithmetic
// (weave_spec.md §8). % uses floating-point modulo (math.Mod) since
// Weave does not distinguish integers from floats.
func Sub(a, b any) any { return numOp("-", a, b, func(x, y float64) float64 { return x - y }) }
func Mul(a, b any) any { return numOp("*", a, b, func(x, y float64) float64 { return x * y }) }
func Div(a, b any) any { return numOp("/", a, b, func(x, y float64) float64 { return x / y }) }
func Mod(a, b any) any { return numOp("%", a, b, math.Mod) }

func numOp(op string, a, b any, f func(x, y float64) float64) any {
	an, aok := a.(float64)
	bn, bok := b.(float64)
	if !aok || !bok {
		panic(binOpError(op, a, b))
	}
	return f(an, bn)
}

// Eq and Neq implement `== !=`: value equality across any two Weave
// values, with no restriction to a single type — values of different
// dynamic types simply compare unequal, matching Go's own `any`-to-`any`
// comparison (valid here because every Weave value through Step 3 has a
// comparable underlying Go type).
func Eq(a, b any) any  { return a == b }
func Neq(a, b any) any { return a != b }

// Lt, Lte, Gt, Gte implement `< <= > >=`: ordered comparison of two
// numbers or two strings (weave_spec.md §8: "順序比較(数値・文字列)").
// Comparing across types, or comparing bool/nil (which have no
// ordering), is a runtime type error.
func Lt(a, b any) any  { return ordCmp("<", a, b) < 0 }
func Lte(a, b any) any { return ordCmp("<=", a, b) <= 0 }
func Gt(a, b any) any  { return ordCmp(">", a, b) > 0 }
func Gte(a, b any) any { return ordCmp(">=", a, b) >= 0 }

func ordCmp(op string, a, b any) int {
	if an, ok := a.(float64); ok {
		bn, ok := b.(float64)
		if !ok {
			panic(binOpError(op, a, b))
		}
		switch {
		case an < bn:
			return -1
		case an > bn:
			return 1
		default:
			return 0
		}
	}
	if as, ok := a.(string); ok {
		bs, ok := b.(string)
		if !ok {
			panic(binOpError(op, a, b))
		}
		return strings.Compare(as, bs)
	}
	panic(binOpError(op, a, b))
}

// And, Or, Not implement `&& || !`: boolean logic (weave_spec.md §8).
// Both operands must be bool.
//
// Known gap (see CLAUDE.md's Step 3 "確定した設計判断"): And/Or are
// plain function calls, so Go evaluates both operands before the call —
// Weave's `&&`/`||` do not yet short-circuit. Proper short-circuiting
// needs branching (IF/GOTO/LABEL), which arrives with control flow in
// Step 4; this will move from a runtime call to codegen-level branching
// then.
func And(a, b any) any { return boolOp("&&", a, b, func(x, y bool) bool { return x && y }) }
func Or(a, b any) any  { return boolOp("||", a, b, func(x, y bool) bool { return x || y }) }

func boolOp(op string, a, b any, f func(x, y bool) bool) any {
	ab, aok := a.(bool)
	bb, bok := b.(bool)
	if !aok || !bok {
		panic(binOpError(op, a, b))
	}
	return f(ab, bb)
}

// Not implements unary `!`.
func Not(a any) any {
	ab, ok := a.(bool)
	if !ok {
		panic(fmt.Sprintf("weave: unary ! requires a bool, got %T", a))
	}
	return !ab
}

func binOpError(op string, a, b any) string {
	return fmt.Sprintf("weave: %T %s %T: invalid operand types", a, op, b)
}
