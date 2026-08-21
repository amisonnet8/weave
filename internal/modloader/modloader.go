// Package modloader resolves Weave packages and imports across multiple
// .weave files/directories (weave_spec.md §17), before sema/codegen run.
//
// Load is the sole entry point: given a root path (a directory, per
// §17.1's "パッケージ=ディレクトリ", or a single .weave file — see its own
// doc for why both are supported), it discovers every .weave file in that
// directory and merges them into one package (§17.1), recursively resolves
// and loads every package reached via `import` (§17.2, detecting import
// cycles per §17.5 and rejecting a non-root `func main`, §17.3), renames
// every non-root package's own top-level bindings to a globally-unique
// flat name and rewrites every reference to them accordingly, resolves
// every `qualifier.name` reference into a direct reference to the target
// package's already-renamed flat name (validating `pub`, §17.2, as it
// goes — see rewrite.go), and finally hands back one fully flat,
// single-package-shaped *ast.File — pooling the root package's own
// TopLevel/Main with every (already renamed) imported package's TopLevel
// — for sema.Check and codegen.Generate to consume exactly as they did
// before packages existed. Neither package needs any awareness of
// packages/imports/pub at all (see CLAUDE.md's "確定した設計判断" on why
// no AMIVM GVAR mechanism is needed even with multiple files: Weave has
// only one top-level function, `main`, so every package's TopLevel —
// root's own and every imported one's — ends up flattened into the same
// funcGen ahead of Main.Body, exactly as a single-file program already
// works).
package modloader

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/amisonnet8/weave/internal/ast"
	"github.com/amisonnet8/weave/internal/parser"
)

// loadedPackage is one already-fully-processed package: its own merged,
// self-renamed (if non-root), qualifier-resolved TopLevel, plus the
// tables any OTHER package importing this one needs (rewrite.go's
// resolveQualifiedName).
type loadedPackage struct {
	Name string // directory basename — this package's own identifier prefix (weave_spec.md §17.4); meaningless for the root, which is never prefixed
	Dir  string // absolute directory path — the cycle-detection/cache key

	TopLevel []ast.Stmt    // this package's own (already renamed/resolved) top-level statements
	Main     *ast.FuncDecl // only ever non-nil for the root package (§17.3)

	Renames  map[string]string // bare top-level name -> already-renamed flat name (non-root only)
	PubNames map[string]bool   // flat (already-renamed) name -> true, for every `pub` binding this package declares
}

// loader carries the state shared across one whole Load() call: which
// directories are currently on the recursion stack (cycle detection,
// §17.5) and which have already been fully loaded (so a diamond-shaped
// import graph — two different packages both importing a third — loads
// that third package exactly once), plus the order packages finished
// loading in (used to merge deterministically — imports always finish
// before the package(s) that import them, so their TopLevel side effects
// naturally run first, matching §17.4's evaluation-order rule).
type loader struct {
	visiting map[string]bool
	loaded   map[string]*loadedPackage
	order    []*loadedPackage
}

// Load resolves root (a directory, or a single .weave file) into one
// fully merged, package-resolved *ast.File.
//
// A single-file root is supported alongside directory roots as a
// deliberate CLI convenience (weave_spec.md §17.1's own carve-out):
// passing a specific .weave file compiles *only* that file, as if it were
// the sole file in its own implicit single-file package, ignoring any
// other .weave files that happen to sit alongside it in the same
// directory. This keeps every existing single-file example in examples/
// invocable exactly as before, while directory roots gain full,
// spec-faithful multi-file/import support for genuine packages (see
// examples/modules/ for one).
func Load(root string) (*ast.File, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(absRoot)
	if err != nil {
		return nil, err
	}

	var dir, onlyFile string
	if info.IsDir() {
		dir = absRoot
	} else {
		dir = filepath.Dir(absRoot)
		onlyFile = absRoot
	}

	ld := &loader{visiting: map[string]bool{}, loaded: map[string]*loadedPackage{}}
	rootPkg, err := ld.loadPackage(dir, onlyFile, true)
	if err != nil {
		return nil, err
	}
	if rootPkg.Main == nil {
		return nil, fmt.Errorf("missing entry point: expected `func main(): int { ... }` in the root package")
	}

	merged := &ast.File{}
	for _, pkg := range ld.order {
		merged.TopLevel = append(merged.TopLevel, pkg.TopLevel...)
		if pkg.Main != nil {
			merged.Main = pkg.Main
		}
	}
	return merged, nil
}

// loadPackage loads, merges, self-renames (if non-root), and
// qualifier-resolves the package rooted at dir (restricted to onlyFile
// alone when it's non-empty — see Load's doc). isRoot controls both
// whether `func main` is allowed (§17.3) and whether this package's own
// top-level bindings get a name prefix at all (§17.4 — root's own names
// are never rewritten, since there is exactly one root and no risk of
// collision with itself).
func (ld *loader) loadPackage(dir, onlyFile string, isRoot bool) (*loadedPackage, error) {
	if pkg, ok := ld.loaded[dir]; ok {
		return pkg, nil
	}
	if ld.visiting[dir] {
		return nil, fmt.Errorf("circular import: package %q (or one it imports) imports itself, directly or indirectly (weave_spec.md §17.5)", filepath.Base(dir))
	}
	ld.visiting[dir] = true
	defer delete(ld.visiting, dir)

	file, err := loadPackageFiles(dir, onlyFile)
	if err != nil {
		return nil, err
	}

	name := filepath.Base(dir)
	if !isRoot {
		if file.Main != nil {
			return nil, fmt.Errorf("line %d: `func main` can only be declared in the root package (weave_spec.md §17.3), found in package %q", file.Main.Line, name)
		}
		if !isValidPackageName(name) {
			return nil, fmt.Errorf("package directory %q can't be used as an import prefix — must look like a Weave identifier (letters, digits, underscore; not starting with a digit)", name)
		}
	}

	// Imports are resolved (and, transitively, THEIR OWN imports) before
	// this package's own body is rewritten, so that by the time we get to
	// qualifier resolution below, every imported package's Renames/
	// PubNames table is already finalized — and so that ld.order records
	// them ahead of this package (§17.4's evaluation-order rule).
	quals := map[string]*loadedPackage{}
	for _, imp := range file.Imports {
		targetDir, err := filepath.Abs(filepath.Join(dir, imp.Path))
		if err != nil {
			return nil, err
		}
		target, err := ld.loadPackage(targetDir, "", false)
		if err != nil {
			return nil, err
		}
		if existing, dup := quals[imp.Qualifier]; dup && existing.Dir != target.Dir {
			return nil, fmt.Errorf("line %d: import qualifier %q is already used for a different package in this package", imp.Line, imp.Qualifier)
		}
		quals[imp.Qualifier] = target
	}

	var renames map[string]string
	pubBareNames := map[string]bool{}
	if !isRoot {
		renames = collectRenames(file.TopLevel, name)
		collectPubBareNames(file.TopLevel, pubBareNames)
		applySelfRename(file.TopLevel, renames)
	}

	rw := &rewriter{renames: renames, quals: quals}
	if err := rw.rewriteStmts(file.TopLevel); err != nil {
		return nil, err
	}
	if file.Main != nil {
		if err := rw.rewriteStmts(file.Main.Body); err != nil {
			return nil, err
		}
	}

	pubNames := map[string]bool{}
	for bare := range pubBareNames {
		pubNames[renames[bare]] = true
	}

	pkg := &loadedPackage{
		Name: name, Dir: dir,
		TopLevel: file.TopLevel, Main: file.Main,
		Renames: renames, PubNames: pubNames,
	}
	ld.loaded[dir] = pkg
	ld.order = append(ld.order, pkg)
	return pkg, nil
}

// mergedFile is the intermediate result of merging every .weave file in
// one package directory, before import resolution/renaming.
type mergedFile struct {
	Imports  []ast.Import
	TopLevel []ast.Stmt
	Main     *ast.FuncDecl
}

// loadPackageFiles parses and merges every .weave file in dir (or just
// onlyFile, if given — see Load's doc) into one mergedFile, in
// deterministic (sorted-path) order, enforcing that at most one file in
// the package declares `func main`.
func loadPackageFiles(dir, onlyFile string) (*mergedFile, error) {
	var paths []string
	if onlyFile != "" {
		paths = []string{onlyFile}
	} else {
		matches, err := filepath.Glob(filepath.Join(dir, "*.weave"))
		if err != nil {
			return nil, err
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("no .weave files found in %q", dir)
		}
		sort.Strings(matches)
		paths = matches
	}

	merged := &mergedFile{}
	for _, path := range paths {
		src, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		f, err := parser.Parse(string(src))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		merged.Imports = append(merged.Imports, f.Imports...)
		merged.TopLevel = append(merged.TopLevel, f.TopLevel...)
		if f.Main != nil {
			if merged.Main != nil {
				return nil, fmt.Errorf("%s: only one `func main` is allowed per package (weave_spec.md §17.3)", path)
			}
			merged.Main = f.Main
		}
	}
	return merged, nil
}

// collectRenames builds this (non-root) package's own bare-name ->
// prefixed-name table (weave_spec.md §17.4) for every top-level
// `name = value` binding — unlike Cascade's typed declaration categories
// (struct/func/let), Weave has exactly one kind of top-level binding, so
// every *ast.AssignStmt in topLevel needs an entry, `pub` or not (a
// private binding can still be referenced by a `pub` one in the same
// package, so it still needs a collision-free flat name).
func collectRenames(topLevel []ast.Stmt, prefix string) map[string]string {
	renames := map[string]string{}
	for _, stmt := range topLevel {
		if a, ok := stmt.(*ast.AssignStmt); ok {
			renames[a.Name] = prefix + "_" + a.Name
		}
	}
	return renames
}

// collectPubBareNames records the ORIGINAL (pre-rename) bare name of
// every top-level binding marked `pub` (weave_spec.md §17.2) — called
// before applySelfRename mutates those same bindings' own Name fields,
// so it must run first.
func collectPubBareNames(topLevel []ast.Stmt, out map[string]bool) {
	for _, stmt := range topLevel {
		if a, ok := stmt.(*ast.AssignStmt); ok && a.Pub {
			out[a.Name] = true
		}
	}
}

// applySelfRename renames every top-level binding's own Name field in
// place, using the bare -> prefixed table collectRenames built. This only
// touches each binding's identity; every REFERENCE to it (elsewhere in
// this same package's own files) is handled separately by
// rewriter.rewriteStmts, using the identical renames table.
func applySelfRename(topLevel []ast.Stmt, renames map[string]string) {
	for _, stmt := range topLevel {
		if a, ok := stmt.(*ast.AssignStmt); ok {
			a.Name = renames[a.Name]
		}
	}
}

func isValidPackageName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		isLetter := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_'
		isDigit := r >= '0' && r <= '9'
		if i == 0 && !isLetter {
			return false
		}
		if !isLetter && !isDigit {
			return false
		}
	}
	return true
}
