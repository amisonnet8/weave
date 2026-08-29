package weavert

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// String operations (weave_spec.md §11/§18.9) route through weavert for
// the same reason every other operator/builtin does (weavert/ops.go's
// package doc comment): every Weave value is a Go `any`, and Go doesn't
// let you call a string method on an `any`-typed variable without first
// asserting its concrete type — that assertion has to happen somewhere,
// and doing it here (uniformly, with a Weave-flavored panic message
// instead of a raw Go type-assertion failure) is the same choice
// ops.go/object.go already made.
//
// Indices are rune-based throughout (IndexOf, Substring), matching
// Len's own "character count, not byte count" contract (weavert.go's
// Len) — a Weave program has no way to observe UTF-8 byte offsets
// directly, so exposing them here would be an inconsistent surprise.

// stringArg asserts v is a Weave string, panicking with a message naming
// which builtin (desc, e.g. "contains(...)") rejected it — every string
// builtin below funnels its argument checks through this so the error
// shape stays uniform.
func stringArg(desc string, v any) string {
	s, ok := v.(string)
	if !ok {
		panic(fmt.Sprintf("weave: %s requires a string, got %T", desc, v))
	}
	return s
}

// nonNegativeIntArg asserts v is a Weave number that is both whole and
// non-negative (weave_spec.md §2: Weave has no separate integer type, so
// this is a runtime check, not something sema could ever catch) —
// Substring's start/end and Repeat's count both need this.
func nonNegativeIntArg(desc string, v any) int {
	n, ok := v.(float64)
	if !ok || n != math.Trunc(n) || n < 0 {
		panic(fmt.Sprintf("weave: %s requires a non-negative whole number, got %v", desc, v))
	}
	return int(n)
}

// Contains implements the `contains` builtin: does s contain sub as a
// substring (weave_spec.md §11)?
func Contains(s any, sub any) any {
	return strings.Contains(stringArg("contains(...)", s), stringArg("contains(...)", sub))
}

// IndexOf implements the `indexOf` builtin: the rune position of sub's
// first occurrence in s, or -1 if sub doesn't occur at all (the common
// "not found" convention most languages use — unlike at(...)'s
// deliberately hard-error-on-miss design, weave_spec.md §11, "not
// present" is indexOf's normal, expected result, not a programming
// mistake). strings.Index gives a byte offset; converting the *prefix*
// (not the whole string) to a rune count turns that into the rune offset
// Len's own character-counting contract expects.
func IndexOf(s any, sub any) any {
	str := stringArg("indexOf(...)", s)
	subStr := stringArg("indexOf(...)", sub)
	bytePos := strings.Index(str, subStr)
	if bytePos < 0 {
		return -1.0
	}
	return float64(utf8.RuneCountInString(str[:bytePos]))
}

// Substring implements the `substring` builtin: the half-open range
// [start, end) of s, by rune position (weave_spec.md §11). Like
// ObjAt/ObjSetAt's own numeric indices (object.go), an out-of-range or
// backwards range panics immediately rather than silently clamping —
// consistent with this project's established "an index is far more
// likely a programming mistake than an intentionally-partial request"
// stance (see ObjAt's own doc comment).
func Substring(s any, start any, end any) any {
	str := stringArg("substring(...)", s)
	runes := []rune(str)
	startN := nonNegativeIntArg("substring(...)", start)
	endN := nonNegativeIntArg("substring(...)", end)
	if startN > len(runes) || endN > len(runes) || startN > endN {
		panic(fmt.Sprintf("weave: substring(...): invalid range [%d, %d) for a %d-character string", startN, endN, len(runes)))
	}
	return string(runes[startN:endN])
}

// Upper/Lower implement the `upper`/`lower` builtins: Unicode-aware case
// conversion (weave_spec.md §11), matching Len's own Unicode-aware
// character counting rather than treating strings as raw ASCII bytes.
func Upper(s any) any { return strings.ToUpper(stringArg("upper(...)", s)) }
func Lower(s any) any { return strings.ToLower(stringArg("lower(...)", s)) }

// Trim implements the `trim` builtin: strip leading/trailing whitespace
// (weave_spec.md §11), using Go's own (Unicode-aware) definition of
// whitespace.
func Trim(s any) any { return strings.TrimSpace(stringArg("trim(...)", s)) }

// Replace implements the `replace` builtin: replace every occurrence of
// old with new (weave_spec.md §11) — always all occurrences, never just
// the first, since a separate "replace at most N" variant isn't
// something weave_spec.md's builtins offer for any other operation
// either (keeping this one simple rather than growing an optional-count
// parameter no other builtin has a precedent for).
func Replace(s any, old any, replacement any) any {
	return strings.ReplaceAll(stringArg("replace(...)", s), stringArg("replace(...)", old), stringArg("replace(...)", replacement))
}

// Split implements the `split` builtin: break s into a Weave list
// (weave_spec.md §3's numeric-keyed object, same shape `list(...)`
// produces) of the substrings between each occurrence of sep.
func Split(s any, sep any) any {
	parts := strings.Split(stringArg("split(...)", s), stringArg("split(...)", sep))
	list := Object{}
	for i, p := range parts {
		list[listKey(i)] = p
	}
	return list
}

// Join implements the `join` builtin: the inverse of Split — concatenate
// a Weave list's own string elements (weave_spec.md §3), in the same
// position order `for k, v in list` would visit them, with sep between
// each pair. Mirrors ObjKeys's own key handling (object.go: skip
// protoKey, sort the raw padded keys for correct numeric order) rather
// than calling ObjKeys itself, since it needs each element's *value* as
// well as its key.
func Join(list any, sep any) any {
	o := objOf(list)
	sepStr := stringArg("join(...)", sep)
	keys := make([]string, 0, len(o))
	for k := range o {
		if k == protoKey {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, stringArg("join(...)", o[k]))
	}
	return strings.Join(parts, sepStr)
}

// Repeat implements the `repeat` builtin: s repeated n times
// (weave_spec.md §11).
func Repeat(s any, n any) any {
	str := stringArg("repeat(...)", s)
	count := nonNegativeIntArg("repeat(...)", n)
	return strings.Repeat(str, count)
}

// ToNumber implements the `toNumber` builtin: parse a string into a
// Weave number (weave_spec.md §11) — the inverse of the `string`
// builtin's number→string direction (ToString above), which weave_spec.md
// §18.9 used to flag as a real, if undocumented, gap (nothing converted
// the other way natively). No leading/trailing whitespace is trimmed
// first — strconv.ParseFloat's own strictness is kept as-is, matching
// the same raw-Go-function behavior a direct `gofunc("?strconv.ParseFloat", ...)`
// call would already have.
func ToNumber(s any) any {
	str := stringArg("toNumber(...)", s)
	n, err := strconv.ParseFloat(str, 64)
	if err != nil {
		panic(fmt.Sprintf("weave: toNumber(%q): %v", str, err))
	}
	return n
}
