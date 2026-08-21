// Package weavert is Weave's own Go runtime, embedded into every
// compiled program (see embed.go and cmd/weave/build.go's
// writeWeavert). It exists for the handful of operations Weave's
// dynamic value model (every value is a Go `any` — weave_spec.md §2;
// CLAUDE.md's Weave特有の設計課題 1) needs that AMIVM-IR's native
// instructions can't express directly.
package weavert

import "fmt"

// Print implements the `print` builtin (weave_spec.md §11): write v to
// stdout followed by a newline. This exists instead of a bare
// ?fmt.Println call because Go's own formatting of a nil `any` reads as
// "<nil>", not Weave's "nil".
func Print(v any) {
	if v == nil {
		fmt.Println("nil")
		return
	}
	fmt.Println(v)
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
