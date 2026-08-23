// Package weavert is Weave's own Go runtime, embedded into every
// compiled program (see embed.go and cmd/weave/build.go's
// writeWeavert). It exists for the handful of operations Weave's
// dynamic value model (every value is a Go `any` — weave_spec.md §2;
// CLAUDE.md's Weave特有の設計課題 1) needs that AMIVM-IR's native
// instructions can't express directly.
package weavert

import (
	"fmt"
	"os"
	"strconv"
	"unicode/utf8"
)

// Print implements the `print` builtin (weave_spec.md §11): write v to
// stdout followed by a newline, using the same rendering as the
// `string` builtin (ToString) — see its doc comment for why that isn't
// just a bare ?fmt.Println call.
func Print(v any) {
	fmt.Println(ToString(v))
}

// ToString implements the `string` builtin (weave_spec.md §11):
// convert any Weave value to its string form. This exists instead of a
// bare ?fmt.Sprintf("%v", ...) call because Go's own formatting of a
// nil `any` reads as "<nil>", not Weave's "nil" (confirmed empirically
// in Step 2 — see CLAUDE.md's Step 2 "確定した設計判断"); numbers,
// bools, and strings otherwise already render the way Go's own %v does
// (also confirmed empirically), so those cases are spelled out mainly
// for clarity rather than to override anything.
func ToString(v any) string {
	if v == nil {
		return "nil"
	}
	switch x := v.(type) {
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	default:
		// Objects and closures have no format weave_spec.md defines —
		// Go's own %v is a reasonable, unspecified-but-harmless fallback.
		return fmt.Sprintf("%v", x)
	}
}

// Len implements the `len` builtin (weave_spec.md §11): an object's own
// property count, or a string's character (rune, not byte) count. Also
// reused internally by for-in's iteration (ObjKeys's own []string
// result) — see internal/codegen's genForStmt.
func Len(v any) any {
	switch x := v.(type) {
	case Object:
		return float64(len(x))
	case string:
		return float64(utf8.RuneCountInString(x))
	case []string:
		return float64(len(x))
	default:
		panic(fmt.Sprintf("weave: len() requires an object or a string, got %T", v))
	}
}

// Args returns the process's command-line arguments as a Weave list
// (weave_spec.md §3/§12 — the same numeric-keyed weavert.Object every
// list(...) literal uses), including the program's own invocation name
// at position 0 (Go's own os.Args convention, kept as-is rather than
// sliced — weave_spec.md §12's own design-decision note explains the
// choice). Elements are always plain strings — command-line arguments
// have no numeric type to normalize into, unlike a Go-asset call result
// (weavert/goasset.go's NormalizeGoValue). Called once, by `!main`'s own
// wrapper, before weave_main is ever invoked (internal/codegen/codegen.go's
// Generate) — main's own body never calls this directly.
func Args() any {
	list := Object{}
	for i, a := range os.Args {
		list[listKey(i)] = a
	}
	return list
}

// ExitCode converts a Weave value into the Go int the `!main` wrapper
// needs for os.Exit (weave_spec.md §12: main always returns `int`, the
// one place Weave's dynamic value model touches a native Go type). This
// is Weave's first runtime type check — see CLAUDE.md's design question
// 6 ("実行時型エラーの表現"): a type assertion + panic is the working
// hypothesis until a real error-reporting story exists.
func ExitCode(v any) int {
	n, ok := v.(float64)
	if !ok {
		panic(fmt.Sprintf("weave: exit code must be a number, got %T", v))
	}
	return int(n)
}
