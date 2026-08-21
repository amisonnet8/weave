package modloader

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/amisonnet8/weave/internal/ast"
)

// writeFiles creates dir/name -> content for each entry, creating parent
// directories as needed, and returns dir.
func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func assignNames(stmts []ast.Stmt) []string {
	var names []string
	for _, s := range stmts {
		if a, ok := s.(*ast.AssignStmt); ok {
			names = append(names, a.Name)
		}
	}
	return names
}

func TestLoad_SingleFile(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"main.weave": "x = 1\nfunc main(): int {\n\treturn 0\n}\n",
	})
	file, err := Load(filepath.Join(dir, "main.weave"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if file.Main == nil {
		t.Fatal("Main is nil")
	}
	if got := assignNames(file.TopLevel); len(got) != 1 || got[0] != "x" {
		t.Errorf("TopLevel names = %v, want [x]", got)
	}
}

func TestLoad_SingleFileIgnoresSiblings(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"main.weave":  "func main(): int {\n\treturn 0\n}\n",
		"other.weave": "func main(): int {\n\treturn 1\n}\n", // would collide if merged
	})
	if _, err := Load(filepath.Join(dir, "main.weave")); err != nil {
		t.Fatalf("Load: %v", err)
	}
}

func TestLoad_DirectoryMergesFiles(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"a.weave": "x = 1\n",
		"b.weave": "func main(): int {\n\treturn x\n}\n",
	})
	file, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if file.Main == nil {
		t.Fatal("Main is nil")
	}
	if got := assignNames(file.TopLevel); len(got) != 1 || got[0] != "x" {
		t.Errorf("TopLevel names = %v, want [x] (from a.weave, merged ahead of b.weave's main)", got)
	}
}

func TestLoad_MissingMainIsAnError(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"a.weave": "x = 1\n",
	})
	if _, err := Load(dir); err == nil {
		t.Fatal("expected an error for a package with no func main")
	}
}

func TestLoad_DuplicateMainInSamePackageIsAnError(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"a.weave": "func main(): int {\n\treturn 0\n}\n",
		"b.weave": "func main(): int {\n\treturn 1\n}\n",
	})
	if _, err := Load(dir); err == nil {
		t.Fatal("expected an error for two `func main` in the same package")
	}
}

func TestLoad_ImportResolvesQualifiedCallToFlatIdent(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"mathutil/clamp.weave": "pub clamp = fn(v) { return v }\n",
		"main.weave": `import mathutil "./mathutil"
func main(): int {
	return mathutil.clamp(1)
}
`,
	})
	file, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	ret, ok := file.Main.Body[0].(*ast.ReturnStmt)
	if !ok {
		t.Fatalf("Main.Body[0] = %#v, want *ast.ReturnStmt", file.Main.Body[0])
	}
	call, ok := ret.Value.(*ast.CallExpr)
	if !ok {
		t.Fatalf("return value = %#v, want *ast.CallExpr", ret.Value)
	}
	callee, ok := call.Callee.(*ast.Ident)
	if !ok {
		t.Fatalf("Callee = %#v, want a plain *ast.Ident (not a PropExpr — §9's self-injection must not apply)", call.Callee)
	}
	if callee.Name != "mathutil_clamp" {
		t.Errorf("Callee.Name = %q, want %q", callee.Name, "mathutil_clamp")
	}
	if got := assignNames(file.TopLevel); len(got) != 1 || got[0] != "mathutil_clamp" {
		t.Errorf("TopLevel names = %v, want [mathutil_clamp]", got)
	}
}

func TestLoad_PrivateMemberNotExportedIsAnError(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"mathutil/clamp.weave": "helper = fn(v) { return v }\n", // no `pub`
		"main.weave": `import mathutil "./mathutil"
func main(): int {
	return mathutil.helper(1)
}
`,
	})
	if _, err := Load(dir); err == nil {
		t.Fatal("expected an error referencing a non-pub member")
	}
}

func TestLoad_UnknownMemberIsAnError(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"mathutil/clamp.weave": "pub clamp = fn(v) { return v }\n",
		"main.weave": `import mathutil "./mathutil"
func main(): int {
	return mathutil.nope(1)
}
`,
	})
	if _, err := Load(dir); err == nil {
		t.Fatal("expected an error referencing an undeclared member")
	}
}

func TestLoad_MainInNonRootPackageIsAnError(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"sub/x.weave": "func main(): int {\n\treturn 0\n}\n",
		"main.weave": `import sub "./sub"
func main(): int {
	return 0
}
`,
	})
	if _, err := Load(dir); err == nil {
		t.Fatal("expected an error for `func main` in a non-root package")
	}
}

func TestLoad_CircularImportIsAnError(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"a/a.weave": `import b "../b"
pub fromA = 1
`,
		"b/b.weave": `import a "../a"
pub fromB = 1
`,
		"main.weave": `import a "./a"
func main(): int {
	return 0
}
`,
	})
	if _, err := Load(dir); err == nil {
		t.Fatal("expected an error for a circular import")
	}
}

func TestLoad_QualifierShadowIsAnError(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"mathutil/clamp.weave": "pub clamp = fn(v) { return v }\n",
		"main.weave": `import mathutil "./mathutil"
func main(): int {
	mathutil = 5
	return 0
}
`,
	})
	if _, err := Load(dir); err == nil {
		t.Fatal("expected an error assigning to a name that shadows an import qualifier")
	}
}

func TestLoad_DuplicateQualifierDifferentPathIsAnError(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"a/a.weave": "pub x = 1\n",
		"b/b.weave": "pub x = 1\n",
		"main.weave": `import shared "./a"
import shared "./b"
func main(): int {
	return 0
}
`,
	})
	if _, err := Load(dir); err == nil {
		t.Fatal("expected an error reusing a qualifier name for two different import paths")
	}
}

func TestLoad_InvalidPackageNameIsAnError(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"my-utils/x.weave": "pub y = 1\n",
		"main.weave": `import u "./my-utils"
func main(): int {
	return 0
}
`,
	})
	if _, err := Load(dir); err == nil {
		t.Fatal("expected an error for a package directory name that isn't identifier-shaped")
	}
}

func TestLoad_DiamondImportLoadsSharedPackageOnce(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"shared/shared.weave": "pub value = 1\n",
		"a/a.weave": `import shared "../shared"
pub fromA = shared.value
`,
		"b/b.weave": `import shared "../shared"
pub fromB = shared.value
`,
		"main.weave": `import a "./a"
import b "./b"
func main(): int {
	return 0
}
`,
	})
	file, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	count := 0
	for _, name := range assignNames(file.TopLevel) {
		if name == "shared_value" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("shared_value appears %d times in merged TopLevel, want exactly 1 (diamond import must load shared once)", count)
	}
}

func TestLoad_SamePackagePrivateHelperIsUsableFromPub(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"mathutil/clamp.weave": `pub clamp = fn(v, lo, hi) {
	if v < lo { return lo }
	if v > hi { return hi }
	return v
}
square = fn(x) { return x * x }
pub clampSquare = fn(v, lo, hi) {
	return square(clamp(v, lo, hi))
}
`,
		"main.weave": `import mathutil "./mathutil"
func main(): int {
	return mathutil.clampSquare(15, 0, 10)
}
`,
	})
	if _, err := Load(dir); err != nil {
		t.Fatalf("Load: %v", err)
	}
}
