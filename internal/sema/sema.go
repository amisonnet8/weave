package sema

import (
	"fmt"

	"github.com/amisonnet8/weave/internal/ast"
)

// Check validates file ahead of code generation.
//
// Step 1 scope: only main's shape (name, declared return type) is
// checked. Per CLAUDE.md's "意味検証の責任分担", Weave's semantic checks
// stay narrower than Seed/Cascade's overall — most value-type errors are
// a runtime concern here rather than something sema can catch ahead of
// time — but scope/name rules like this one are exactly the kind of
// thing sema is still responsible for.
func Check(file *ast.File) error {
	if file.Main == nil {
		return fmt.Errorf("missing entry point: expected `func main(): int { ... }`")
	}
	if file.Main.Name != "main" {
		return fmt.Errorf("line %d: `func` may only declare `main` (weave_spec.md §12), got %q", file.Main.Line, file.Main.Name)
	}
	if file.Main.ReturnType != "int" {
		return fmt.Errorf("line %d: main must return `int`, got %q", file.Main.Line, file.Main.ReturnType)
	}
	return nil
}
