package modloader

import (
	"archive/zip"
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

// writeWvz zips files (flat, no directory prefix — matching weave wvz's
// own layout) into a new .wvz archive at dir/name, returning dir.
func writeWvz(t *testing.T, dir, name string, files map[string]string) {
	t.Helper()
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for entryName, content := range files {
		w, err := zw.Create(entryName)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
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
		"main.weave": "x = 1\nmain = fn(args) {\n\treturn 0\n}\n",
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
		"main.weave":  "main = fn(args) {\n\treturn 0\n}\n",
		"other.weave": "main = fn(args) {\n\treturn 1\n}\n", // would collide if merged
	})
	if _, err := Load(filepath.Join(dir, "main.weave")); err != nil {
		t.Fatalf("Load: %v", err)
	}
}

func TestLoad_DirectoryMergesFiles(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"a.weave": "x = 1\n",
		"b.weave": "main = fn(args) {\n\treturn x\n}\n",
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
		t.Fatal("expected an error for a package with no entry point")
	}
}

func TestLoad_DuplicateMainInSamePackageIsAnError(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"a.weave": "main = fn(args) {\n\treturn 0\n}\n",
		"b.weave": "main = fn(args) {\n\treturn 1\n}\n",
	})
	if _, err := Load(dir); err == nil {
		t.Fatal("expected an error for two `main = fn(...) {...}` in the same package")
	}
}

func TestLoad_PackageCallResolvesQualifiedCallToFlatIdent(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"mathutil/clamp.weave": "Clamp = fn(v) { return v }\n",
		"main.weave": `mathutil = package("./mathutil")
main = fn(args) {
	return mathutil.Clamp(1)
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
	if callee.Name != "mathutil_Clamp" {
		t.Errorf("Callee.Name = %q, want %q", callee.Name, "mathutil_Clamp")
	}
	if got := assignNames(file.TopLevel); len(got) != 1 || got[0] != "mathutil_Clamp" {
		t.Errorf("TopLevel names = %v, want [mathutil_Clamp] (the package(...) binding itself must be stripped, like gotype/gofunc)", got)
	}
}

func TestLoad_LowercaseMemberNotExportedIsAnError(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"mathutil/clamp.weave": "helper = fn(v) { return v }\n", // lowercase: not exported
		"main.weave": `mathutil = package("./mathutil")
main = fn(args) {
	return mathutil.helper(1)
}
`,
	})
	if _, err := Load(dir); err == nil {
		t.Fatal("expected an error referencing a non-exported (lowercase) member")
	}
}

func TestLoad_UnknownMemberIsAnError(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"mathutil/clamp.weave": "Clamp = fn(v) { return v }\n",
		"main.weave": `mathutil = package("./mathutil")
main = fn(args) {
	return mathutil.Nope(1)
}
`,
	})
	if _, err := Load(dir); err == nil {
		t.Fatal("expected an error referencing an undeclared member")
	}
}

// TestLoad_MainInNonRootPackageIsDemotedToOrdinaryBinding verifies
// weave_spec.md §15.3's option (a) resolution: a non-root package's own
// `main = fn(args) {...}` is never an error — it is silently demoted
// into an ordinary top-level binding (renamed like any other, here
// `sub_main`), never treated as an entry point. Only the root package's
// own `main` becomes the actual entry point.
func TestLoad_MainInNonRootPackageIsDemotedToOrdinaryBinding(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"sub/x.weave": "main = fn(args) {\n\treturn 0\n}\n",
		"main.weave": `sub = package("./sub")
main = fn(args) {
	return 0
}
`,
	})
	file, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if file.Main == nil {
		t.Fatal("Main is nil (want the root package's own entry point)")
	}
	got := assignNames(file.TopLevel)
	found := false
	for _, name := range got {
		if name == "sub_main" {
			found = true
		}
		if name == "main" {
			t.Errorf("TopLevel contains a bare %q binding — the non-root package's main must be renamed, not left as-is", name)
		}
	}
	if !found {
		t.Errorf("TopLevel names = %v, want sub_main present (the non-root package's own main, demoted and renamed)", got)
	}
}

func TestLoad_CircularImportIsAnError(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"a/a.weave": `b = package("../b")
FromA = 1
`,
		"b/b.weave": `a = package("../a")
FromB = 1
`,
		"main.weave": `a = package("./a")
main = fn(args) {
	return 0
}
`,
	})
	if _, err := Load(dir); err == nil {
		t.Fatal("expected an error for a circular import")
	}
}

// TestLoad_QualifierReassignmentIsGracefullyDegraded verifies the
// deliberate relaxation from this feature's first iteration: reusing a
// qualifier's name for an ordinary value silently drops its package
// tracking (mirroring sema/codegen's own goStaticVars behavior for
// gofunc-tracked variables) rather than being a hard compile error.
func TestLoad_QualifierReassignmentIsGracefullyDegraded(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"mathutil/clamp.weave": "Clamp = fn(v) { return v }\n",
		"main.weave": `mathutil = package("./mathutil")
mathutil = 5
main = fn(args) {
	return mathutil
}
`,
	})
	file, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// mathutil's own content is still loaded and merged (package("...")
	// really did run, and its bindings' evaluation isn't undone by a
	// later reassignment in the importing file) — only the *qualifier*
	// tracking for `mathutil.member` lookups is dropped.
	got := assignNames(file.TopLevel)
	if len(got) != 2 || got[0] != "mathutil_Clamp" || got[1] != "mathutil" {
		t.Errorf("TopLevel names = %v, want [mathutil_Clamp mathutil]", got)
	}
}

// TestLoad_ReassigningBindingNameToADifferentPackageIsAnError checks the
// flip side of the internal renaming prefix now coming from the binding
// name (weave_spec.md §15.4) rather than the directory name: reusing the
// same binding name for two genuinely different packages — even within
// one file, via reassignment — would make both packages' own renamed
// bindings collide on the very same prefix (both would emit `shared_X`).
// This used to be silently allowed ("the second one wins", back when the
// prefix came from the directory name and "a"/"b" never collided) but is
// now a compile-time error instead, per CLAUDE.md's "検討中の今後の対応"
// §1 — the same collision this whole feature was redesigned to catch,
// just reached via reassignment instead of two same-named directories.
func TestLoad_ReassigningBindingNameToADifferentPackageIsAnError(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"a/a.weave": "X = 1\n",
		"b/b.weave": "X = 2\n",
		"main.weave": `shared = package("./a")
shared = package("./b")
main = fn(args) {
	return shared.X
}
`,
	})
	if _, err := Load(dir); err == nil {
		t.Fatal("expected an error: reusing binding name \"shared\" for a second, different package would collide with the first package's own internal renaming prefix")
	}
}

// TestLoad_DirectoryNameNeedNotBeIdentifierShaped verifies that a
// package's internal renaming prefix now comes entirely from the
// importer's own chosen binding name (weave_spec.md §15.4), not from the
// directory name — so a directory name that wouldn't itself be a valid
// Weave identifier (hyphens, here) is no longer rejected, since it is
// never used as a prefix at all. This replaces the old
// TestLoad_InvalidPackageNameIsAnError, which enforced identifier-shaped
// directory names purely because the directory name used to be the
// prefix source; that constraint no longer serves any purpose.
func TestLoad_DirectoryNameNeedNotBeIdentifierShaped(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"my-utils/x.weave": "Y = 1\n",
		"main.weave": `u = package("./my-utils")
main = fn(args) {
	return u.Y
}
`,
	})
	file, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := assignNames(file.TopLevel); len(got) != 1 || got[0] != "u_Y" {
		t.Errorf("TopLevel names = %v, want [u_Y] (the internal prefix comes from the binding name %q, not the directory name)", got, "u")
	}
}

// TestLoad_SameDirectoryNameDifferentPackagesNoLongerCollide is the
// direct regression test for the bug CLAUDE.md's "検討中の今後の対応" §1
// (and weave_spec.md's former §15.4/§18.8 "known undetected bug") set out
// to fix: two unrelated packages that happen to share a directory
// basename ("utils") used to have their renamed bindings silently
// collide, because the old renaming prefix was the directory basename.
// Now that the prefix comes from the importer's own chosen binding name
// instead, importing both under different names produces no collision at
// all.
func TestLoad_SameDirectoryNameDifferentPackagesNoLongerCollide(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"foo/utils/x.weave": `Helper = fn(x) { return "A:" + string(x) }`,
		"bar/utils/x.weave": `Helper = fn(x) { return "B:" + string(x) }`,
		"main.weave": `utilsA = package("./foo/utils")
utilsB = package("./bar/utils")
main = fn(args) {
	return 0
}
`,
	})
	file, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := assignNames(file.TopLevel)
	hasA, hasB := false, false
	for _, name := range got {
		if name == "utilsA_Helper" {
			hasA = true
		}
		if name == "utilsB_Helper" {
			hasB = true
		}
	}
	if !hasA || !hasB {
		t.Errorf("TopLevel names = %v, want both utilsA_Helper and utilsB_Helper present (no collision despite both directories being named \"utils\")", got)
	}
}

// TestLoad_SameBindingNameForDifferentPackagesAcrossFilesIsAnError checks
// the cross-file shape of the same new rule verified by
// TestLoad_ReassigningBindingNameToADifferentPackageIsAnError: two
// different *packages* (here, x and y, each imported by the root) that
// each independently chose the same binding name ("utils") for two
// genuinely different target packages must be rejected — left unchecked,
// x's own `utils_Value` and y's own `utils_Value` would collide in the
// final merged program exactly like same-named directories used to
// (weave_spec.md §15.4).
func TestLoad_SameBindingNameForDifferentPackagesAcrossFilesIsAnError(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"utilsA/a.weave": "Value = 1\n",
		"utilsB/b.weave": "Value = 2\n",
		"x/x.weave": `utils = package("../utilsA")
FromX = utils.Value
`,
		"y/y.weave": `utils = package("../utilsB")
FromY = utils.Value
`,
		"main.weave": `x = package("./x")
y = package("./y")
main = fn(args) {
	return 0
}
`,
	})
	if _, err := Load(dir); err == nil {
		t.Fatal("expected an error: package x and package y both used binding name \"utils\" for two different target packages")
	}
}

func TestLoad_DiamondImportLoadsSharedPackageOnce(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"shared/shared.weave": "Value = 1\n",
		"a/a.weave": `shared = package("../shared")
FromA = shared.Value
`,
		"b/b.weave": `shared = package("../shared")
FromB = shared.Value
`,
		"main.weave": `a = package("./a")
b = package("./b")
main = fn(args) {
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
		if name == "shared_Value" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("shared_Value appears %d times in merged TopLevel, want exactly 1 (diamond import must load shared once)", count)
	}
}

func TestLoad_SamePackagePrivateHelperIsUsableFromExported(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"mathutil/clamp.weave": `Clamp = fn(v, lo, hi) {
	if v < lo { return lo }
	if v > hi { return hi }
	return v
}
square = fn(x) { return x * x }
ClampSquare = fn(v, lo, hi) {
	return square(Clamp(v, lo, hi))
}
`,
		"main.weave": `mathutil = package("./mathutil")
main = fn(args) {
	return mathutil.ClampSquare(15, 0, 10)
}
`,
	})
	if _, err := Load(dir); err != nil {
		t.Fatalf("Load: %v", err)
	}
}

func TestLoad_WvzArchiveIsUsableAsAPackage(t *testing.T) {
	dir := t.TempDir()
	writeWvz(t, dir, "mathutil.wvz", map[string]string{
		"clamp.weave": "Clamp = fn(v) { return v }\n",
	})
	if err := os.WriteFile(filepath.Join(dir, "main.weave"), []byte(`mathutil = package("./mathutil.wvz")
main = fn(args) {
	return mathutil.Clamp(1)
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	file, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := assignNames(file.TopLevel); len(got) != 1 || got[0] != "mathutil_Clamp" {
		t.Errorf("TopLevel names = %v, want [mathutil_Clamp]", got)
	}
}

func TestLoad_WvzArchiveAsRootWorks(t *testing.T) {
	dir := t.TempDir()
	writeWvz(t, dir, "prog.wvz", map[string]string{
		"main.weave": "main = fn(args) {\n\treturn 0\n}\n",
	})
	if _, err := Load(filepath.Join(dir, "prog.wvz")); err != nil {
		t.Fatalf("Load: %v", err)
	}
}
