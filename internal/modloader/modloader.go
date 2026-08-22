// Package modloader resolves Weave packages across multiple .weave
// files/directories/.wvz archives (weave_spec.md §17), before sema/
// codegen run.
//
// Load is the main entry point (LoadPackage is a narrower variant used
// only by `weave wvz`'s own pre-archive build check — see its own doc):
// given a root path (a directory, a single .weave file, or a .wvz
// archive — see its own doc for why all three are supported), it
// discovers every .weave file in that package and merges
// them into one package (§17.1), recursively resolves and loads every
// package reached via a top-level `name = package("<path>")` binding
// (§17.2, detecting import cycles per §17.5 and rejecting a non-root
// `func main`, §17.3), renames every non-root package's own top-level
// bindings to a globally-unique flat name and rewrites every reference to
// them accordingly, resolves every `qualifier.name` reference into a
// direct reference to the target package's already-renamed flat name
// (checking that name is exported, §17.2, as it goes — see rewrite.go),
// and finally hands back one fully flat, single-package-shaped *ast.File
// — pooling the root package's own TopLevel/Main with every (already
// renamed) imported package's TopLevel — for sema.Check and
// codegen.Generate to consume exactly as they did before packages
// existed. Neither package needs any awareness of packages at all (see
// CLAUDE.md's "確定した設計判断" on why no AMIVM GVAR mechanism is needed
// even with multiple files: Weave has only one top-level function,
// `main`, so every package's TopLevel — root's own and every imported
// one's — ends up flattened into the same funcGen ahead of Main.Body,
// exactly as a single-file program already works).
//
// There is deliberately no `import`/`pub` syntax. A cross-package
// reference is expressed the same way every other Weave declaration is:
// a top-level `name = package("<path>")` binding, recognized by
// pattern-matching the AssignStmt's RHS — exactly like `gotype(...)`/
// `gofunc(...)` (internal/sema, internal/codegen's goasset.go) — rather
// than a dedicated keyword/AST node. `package(...)` is compile-time-only
// (like gotype/gofunc): it never reaches sema/codegen, and never emits
// any IR of its own. Visibility, in turn, follows Go's own convention
// instead of a `pub` modifier: a top-level binding is visible to
// importers exactly when its own name starts with an uppercase letter
// (weave_spec.md §17.2) — there is no separate "exported" bit to track,
// since the binding's spelling *is* its own visibility.
package modloader

import (
	"archive/zip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/amisonnet8/weave/internal/ast"
	"github.com/amisonnet8/weave/internal/parser"
)

// loadedPackage is one already-fully-processed package: its own merged,
// self-renamed (if non-root), qualifier-resolved TopLevel, plus the
// table any OTHER package importing this one needs (rewrite.go's
// tryResolveQualified).
type loadedPackage struct {
	Name string // package identifier prefix (weave_spec.md §17.4) — a directory's basename, or a .wvz file's basename with the extension stripped; meaningless for the root, which is never prefixed
	Key  string // absolute path (directory or .wvz file) — the cycle-detection/cache key

	TopLevel []ast.Stmt    // this package's own (already renamed/resolved) top-level statements
	Main     *ast.FuncDecl // only ever non-nil for the root package (§17.3)

	Renames map[string]string // bare top-level name -> already-renamed flat name (non-root only)
}

// loader carries the state shared across one whole Load() call: which
// packages are currently on the recursion stack (cycle detection,
// §17.5) and which have already been fully loaded (so a diamond-shaped
// import graph — two different packages both importing a third — loads
// that third package exactly once), the order packages finished loading
// in (used to merge deterministically — imports always finish before
// the package(s) that import them, so their TopLevel side effects
// naturally run first, matching §17.4's evaluation-order rule), and
// every .wvz archive opened along the way (closed once Load returns).
type loader struct {
	visiting map[string]bool
	loaded   map[string]*loadedPackage
	order    []*loadedPackage
	closers  []io.Closer
}

// Load resolves root (a directory, a single .weave file, or a .wvz
// archive) into one fully merged, package-resolved *ast.File.
//
// A single-file root is supported alongside directory/.wvz roots as a
// deliberate CLI convenience (weave_spec.md §17.1's own carve-out):
// passing a specific .weave file compiles *only* that file, as if it
// were the sole file in its own implicit single-file package, ignoring
// any other .weave files that happen to sit alongside it in the same
// directory. This keeps every existing single-file example in examples/
// invocable exactly as before, while directory/.wvz roots gain full,
// spec-faithful multi-file/package support for genuine packages (see
// examples/modules/ for one).
func Load(root string) (*ast.File, error) {
	merged, err := loadMerged(root)
	if err != nil {
		return nil, err
	}
	if merged.Main == nil {
		return nil, fmt.Errorf("missing entry point: expected `func main(): int { ... }` in the root package")
	}
	return merged, nil
}

// LoadPackage resolves root exactly like Load, but does not require the
// root package to declare `func main` of its own — a package meant to
// be *imported* (via another file's `package(...)`) legitimately has
// none. This is used by `weave wvz` (cmd/weave/wvz.go) to validate that
// a package-member directory's own code actually compiles before
// archiving it: the caller is expected to splice in a synthetic `main`
// before handing the result to sema.Check/codegen.Generate, since
// codegen.Generate itself unconditionally dereferences file.Main.
func LoadPackage(root string) (*ast.File, error) {
	return loadMerged(root)
}

// loadMerged does the actual work behind Load/LoadPackage: it resolves
// root into one fully merged *ast.File, leaving the "does the root
// package have `func main`" decision to the caller.
func loadMerged(root string) (*ast.File, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(absRoot)
	if err != nil {
		return nil, err
	}

	var loc *pkgLocation
	var onlyFile string
	if info.IsDir() || strings.EqualFold(filepath.Ext(absRoot), ".wvz") {
		loc, err = openPkgLocation(absRoot)
	} else {
		// A single .weave file: treat its own directory as the package
		// location, restricted to just this one file (see Load's doc).
		loc, err = openPkgLocation(filepath.Dir(absRoot))
		onlyFile = filepath.Base(absRoot)
	}
	if err != nil {
		return nil, err
	}

	ld := &loader{visiting: map[string]bool{}, loaded: map[string]*loadedPackage{}}
	defer ld.closeAll()
	ld.trackCloser(loc.closer)

	if _, err := ld.loadPackage(loc, onlyFile, true); err != nil {
		return nil, err
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

func (ld *loader) trackCloser(c io.Closer) {
	if c != nil {
		ld.closers = append(ld.closers, c)
	}
}

func (ld *loader) closeAll() {
	for _, c := range ld.closers {
		c.Close()
	}
}

// pkgLocation identifies where one package's .weave files live: either a
// real OS directory or a .wvz (ZIP) archive (weave_spec.md §17.6),
// abstracted behind an fs.FS so loadPackageFiles doesn't need to care
// which. key is the absolute path used for cycle detection/dedup; dir is
// the real filesystem directory a *nested* `package("<relative path>")`
// call found inside this package should be resolved against — for a
// directory package that's the directory itself, for a .wvz package
// that's the .wvz file's own containing directory (a .wvz has no
// filesystem directory of its own to anchor further relative paths, so
// its container stands in for it, exactly as a plain directory anchors
// its own nested packages today). closer is non-nil only for a .wvz (the
// open zip reader, closed once the whole Load() call finishes).
type pkgLocation struct {
	fsys   fs.FS
	key    string
	name   string
	dir    string
	closer io.Closer
}

// openPkgLocation opens absPath (already absolute) as a package source —
// a directory or a .wvz file, chosen by absPath's own shape rather than
// a flag, so `package("./mathutil")` and `package("./mathutil.wvz")` are
// interchangeable at every call site (weave_spec.md §17.6).
func openPkgLocation(absPath string) (*pkgLocation, error) {
	info, err := os.Stat(absPath)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return &pkgLocation{
			fsys: os.DirFS(absPath),
			key:  absPath,
			name: filepath.Base(absPath),
			dir:  absPath,
		}, nil
	}
	if !strings.EqualFold(filepath.Ext(absPath), ".wvz") {
		return nil, fmt.Errorf("%q is neither a directory nor a .wvz archive", absPath)
	}
	zr, err := zip.OpenReader(absPath)
	if err != nil {
		return nil, fmt.Errorf("opening %q: %w", absPath, err)
	}
	return &pkgLocation{
		fsys:   zr,
		key:    absPath,
		name:   strings.TrimSuffix(filepath.Base(absPath), filepath.Ext(absPath)),
		dir:    filepath.Dir(absPath),
		closer: zr,
	}, nil
}

// resolvePkgLocation resolves relPath (a package("<path>") argument)
// against baseDir — the loading package's own pkgLocation.dir — into the
// pkgLocation it names.
func resolvePkgLocation(baseDir, relPath string) (*pkgLocation, error) {
	absPath, err := filepath.Abs(filepath.Join(baseDir, relPath))
	if err != nil {
		return nil, err
	}
	return openPkgLocation(absPath)
}

// loadPackage loads, merges, self-renames (if non-root), and
// qualifier-resolves the package at loc (restricted to onlyFile alone
// when it's non-empty — see Load's doc). isRoot controls both whether
// `func main` is allowed (§17.3) and whether this package's own
// top-level bindings get a name prefix at all (§17.4 — root's own names
// are never rewritten, since there is exactly one root and no risk of
// collision with itself).
func (ld *loader) loadPackage(loc *pkgLocation, onlyFile string, isRoot bool) (*loadedPackage, error) {
	if pkg, ok := ld.loaded[loc.key]; ok {
		return pkg, nil
	}
	if ld.visiting[loc.key] {
		return nil, fmt.Errorf("circular import: package %q (or one it imports) imports itself, directly or indirectly (weave_spec.md §17.5)", loc.name)
	}
	ld.visiting[loc.key] = true
	defer delete(ld.visiting, loc.key)

	file, err := loadPackageFiles(loc, onlyFile)
	if err != nil {
		return nil, err
	}

	if !isRoot {
		if file.Main != nil {
			return nil, fmt.Errorf("line %d: `func main` can only be declared in the root package (weave_spec.md §17.3), found in package %q", file.Main.Line, loc.name)
		}
		if !isValidPackageName(loc.name) {
			return nil, fmt.Errorf("package %q can't be used as an import prefix — must look like a Weave identifier (letters, digits, underscore; not starting with a digit)", loc.name)
		}
	}

	// package(...) declarations are extracted (and, transitively, their
	// own imports resolved) before this package's own top-level
	// bindings are renamed — see extractPackageDecls's own doc for why
	// this ordering matters.
	quals, strippedTopLevel, err := ld.extractPackageDecls(file.TopLevel, loc)
	if err != nil {
		return nil, err
	}
	file.TopLevel = strippedTopLevel

	var renames map[string]string
	if !isRoot {
		renames = collectRenames(file.TopLevel, loc.name)
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

	pkg := &loadedPackage{
		Name: loc.name, Key: loc.key,
		TopLevel: file.TopLevel, Main: file.Main,
		Renames: renames,
	}
	ld.loaded[loc.key] = pkg
	ld.order = append(ld.order, pkg)
	return pkg, nil
}

// extractPackageDecls walks topLevel once, in source order, recognizing
// every `name = package("<path>")` binding: it loads (recursively) the
// target package, records it in the returned quals table keyed by the
// *original* (pre-rename) qualifier name, and drops the AssignStmt
// itself — it never reaches sema/codegen, exactly like gotype/gofunc
// (CLAUDE.md's 確定した設計判断). Every OTHER top-level AssignStmt that
// reassigns a name already present in quals removes it instead: a
// qualifier reused for an ordinary value silently stops being treated as
// one from that point on, mirroring sema/codegen's own goStaticVars
// behavior for gofunc-tracked variables rather than hard-erroring (a
// deliberate relaxation from this feature's first iteration — see
// CLAUDE.md).
//
// This must run *before* collectRenames/applySelfRename sees topLevel:
// collectRenames renames every remaining AssignStmt's own Name
// unconditionally, and a `qualifier.member` reference is recognized by
// rewriter.tryResolveQualified matching the qualifier's *original* bare
// name against quals — if a package-decl's own binding were left in
// topLevel long enough to be renamed too, quals and the renames table
// would end up disagreeing about the qualifier's spelling, and ordinary
// bare-identifier rewriting would race the qualifier check on the very
// same name (see tryResolveQualified's own doc for why the qualifier
// check must run first and be exhaustive). Removing package-decl
// bindings before renaming even starts sidesteps that entirely: the
// qualifier's name is never a renames key at all.
func (ld *loader) extractPackageDecls(topLevel []ast.Stmt, loc *pkgLocation) (map[string]*loadedPackage, []ast.Stmt, error) {
	quals := map[string]*loadedPackage{}
	var stripped []ast.Stmt
	for _, stmt := range topLevel {
		a, ok := stmt.(*ast.AssignStmt)
		if !ok {
			stripped = append(stripped, stmt)
			continue
		}
		path, isPkgDecl, err := packageDeclArg(a.Value)
		if err != nil {
			return nil, nil, fmt.Errorf("line %d: %w", a.Line, err)
		}
		if !isPkgDecl {
			delete(quals, a.Name)
			stripped = append(stripped, stmt)
			continue
		}
		targetLoc, err := resolvePkgLocation(loc.dir, path)
		if err != nil {
			return nil, nil, fmt.Errorf("line %d: %w", a.Line, err)
		}
		ld.trackCloser(targetLoc.closer)
		target, err := ld.loadPackage(targetLoc, "", false)
		if err != nil {
			return nil, nil, err
		}
		quals[a.Name] = target
	}
	return quals, stripped, nil
}

// packageDeclArg reports whether value is exactly `package("<path>")` —
// a call to the reserved builtin `package` with a single string-literal
// argument — mirroring gotype/gofunc's own AssignStmt-RHS pattern
// (CLAUDE.md's 確定した設計判断, "gotype/gofuncはASTの新設無しでパターン
// 認識"). Any OTHER call shaped like `package(...)` (wrong arity, a
// non-literal argument) is a compile-time error here rather than being
// silently passed through — matching the project's standing rule that
// syntactic misuse is caught before it can produce a confusing amivm/go
// error downstream (CLAUDE.md's 意味検証の責任分担). A `package(...)`
// call that isn't the direct RHS of a top-level assignment at all (e.g.
// nested in an expression, or inside func main's body) is simply left
// alone here — it falls through to sema, which rejects it as an
// undefined name (there is no runtime `package` value), giving a clear
// enough error without any extra plumbing.
func packageDeclArg(value ast.Expr) (path string, isPkgDecl bool, err error) {
	call, ok := value.(*ast.CallExpr)
	if !ok {
		return "", false, nil
	}
	callee, ok := call.Callee.(*ast.Ident)
	if !ok || callee.Name != "package" {
		return "", false, nil
	}
	if len(call.Args) != 1 {
		return "", false, fmt.Errorf("`package(...)` takes exactly one argument (a path string literal), got %d (weave_spec.md §17.2)", len(call.Args))
	}
	lit, ok := call.Args[0].(*ast.StringLit)
	if !ok {
		return "", false, fmt.Errorf("`package(...)`'s argument must be a string literal path — it is resolved at compile time (internal/modloader), so a computed expression has nothing to evaluate it against")
	}
	return lit.Value, true, nil
}

// mergedFile is the intermediate result of merging every .weave file in
// one package location, before package(...) resolution/renaming.
type mergedFile struct {
	TopLevel []ast.Stmt
	Main     *ast.FuncDecl
}

// loadPackageFiles parses and merges every .weave file in loc (or just
// onlyFile, if given — see Load's doc) into one mergedFile, in
// deterministic (sorted-path) order, enforcing that at most one file in
// the package declares `func main`.
func loadPackageFiles(loc *pkgLocation, onlyFile string) (*mergedFile, error) {
	var names []string
	if onlyFile != "" {
		names = []string{onlyFile}
	} else {
		matches, err := fs.Glob(loc.fsys, "*.weave")
		if err != nil {
			return nil, err
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("no .weave files found in %q", loc.key)
		}
		sort.Strings(matches)
		names = matches
	}

	merged := &mergedFile{}
	for _, name := range names {
		src, err := fs.ReadFile(loc.fsys, name)
		if err != nil {
			return nil, err
		}
		f, err := parser.Parse(string(src))
		if err != nil {
			return nil, fmt.Errorf("%s (in %s): %w", name, loc.key, err)
		}
		merged.TopLevel = append(merged.TopLevel, f.TopLevel...)
		if f.Main != nil {
			if merged.Main != nil {
				return nil, fmt.Errorf("%s (in %s): only one `func main` is allowed per package (weave_spec.md §17.3)", name, loc.key)
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
// every *ast.AssignStmt in topLevel needs an entry, exported or not (a
// private binding can still be referenced by an exported one in the same
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

// isExported reports whether name is visible outside its own package
// (weave_spec.md §17.2): Go's own convention — an uppercase first letter
// — replacing this feature's original `pub` keyword. There is no
// separate bit to track: the binding's own spelling is its visibility.
func isExported(name string) bool {
	if name == "" {
		return false
	}
	r := []rune(name)[0]
	return r >= 'A' && r <= 'Z'
}
