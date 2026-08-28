package weavert

import (
	"fmt"
	"math"
)

// Math functions (weave_spec.md §11) round out weave_spec.md §8's
// arithmetic operators, which cover +-*/ % but nothing beyond — same
// weavert-uniformly-for-every-numeric-operation rationale as ops.go
// (Add/Sub/.../Mod).

// numArg asserts v is a Weave number, panicking with a message naming
// which builtin (desc) rejected it — mirrors strings.go's stringArg.
func numArg(desc string, v any) float64 {
	n, ok := v.(float64)
	if !ok {
		panic(fmt.Sprintf("weave: %s requires a number, got %T", desc, v))
	}
	return n
}

// Floor/Ceil/Round/Abs implement the `floor`/`ceil`/`round`/`abs`
// builtins.
func Floor(v any) any { return math.Floor(numArg("floor(...)", v)) }
func Ceil(v any) any  { return math.Ceil(numArg("ceil(...)", v)) }
func Round(v any) any { return math.Round(numArg("round(...)", v)) }
func Abs(v any) any   { return math.Abs(numArg("abs(...)", v)) }

// Sqrt implements the `sqrt` builtin. A negative argument is a runtime
// type/domain error (weave_spec.md §11) rather than Go's own math.Sqrt
// behavior of silently producing NaN — Weave has no way to represent or
// usefully propagate NaN as a value (weave_spec.md §2 doesn't define one),
// so surfacing the mistake immediately, the same way weavert/ops.go's
// binOpError does for a type mismatch, is more useful than a NaN quietly
// poisoning every arithmetic expression that touches it afterward.
func Sqrt(v any) any {
	n := numArg("sqrt(...)", v)
	if n < 0 {
		panic(fmt.Sprintf("weave: sqrt(...): negative argument %v", n))
	}
	return math.Sqrt(n)
}

// Min/Max implement the `min`/`max` builtins — always exactly two
// arguments, matching every other fixed-arity builtin in this project
// rather than the variadic min/max some languages offer (no existing
// builtin here takes a variable number of arguments except list(...)
// itself, which is a deliberately different, array-building construct).
func Min(a, b any) any { return math.Min(numArg("min(...)", a), numArg("min(...)", b)) }
func Max(a, b any) any { return math.Max(numArg("max(...)", a), numArg("max(...)", b)) }

// Pow implements the `pow` builtin: base raised to the exp power.
func Pow(base any, exp any) any {
	return math.Pow(numArg("pow(...)", base), numArg("pow(...)", exp))
}
