package main

import (
	"archive/zip"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/amisonnet8/weave/internal/ast"
	"github.com/amisonnet8/weave/internal/modloader"
)

// runWvz implements `weave wvz <directory> [-o output.wvz]`: it packages
// every .weave file directly inside the given directory into a single
// .wvz (ZIP) archive, at the archive's own root (no directory prefix
// inside the ZIP), so that internal/modloader's package(...) resolution
// (weave_spec.md §17.6) can read a .wvz exactly as it would read the
// original directory — via fs.Glob(fsys, "*.weave") against whichever
// fs.FS backs it. Deliberately non-recursive: it only zips the files
// directly in the given directory, matching how loadPackageFiles itself
// only globs a package's own top-level .weave files — a package with a
// nested subdirectory of its own package(...) targets stays that way,
// zipped separately if desired.
//
// dir must be a *package member* directory — something meant to be
// reached via another file's `package("./dir")`, with no `func main` of
// its own (weave_spec.md §17.2/§17.3: an importable package may never
// declare one). A directory that already has `func main` is rejected:
// packaging a standalone, directly-runnable program is already `weave
// build`'s job, so `weave wvz` stays scoped to its one purpose —
// producing something `package(...)` can import.
func runWvz(args []string) error {
	fs := flag.NewFlagSet("wvz", flag.ContinueOnError)
	out := fs.String("o", "", "output .wvz file path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: weave wvz [-o output.wvz] <directory>")
	}
	dir := fs.Arg(0)

	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%q is not a directory", dir)
	}

	outPath := *out
	if outPath == "" {
		outPath = filepath.Base(filepath.Clean(dir)) + ".wvz"
	}

	matches, err := filepath.Glob(filepath.Join(dir, "*.weave"))
	if err != nil {
		return err
	}
	if len(matches) == 0 {
		return fmt.Errorf("no .weave files found in %q", dir)
	}

	// modloader.LoadPackage (unlike modloader.Load, used by
	// build/run/emit-ir/emit-go) tolerates a package with no entry point
	// of its own — the normal case here (see doc above).
	file, err := modloader.LoadPackage(dir)
	if err != nil {
		return err
	}
	if file.Main != nil {
		return fmt.Errorf("%q declares its own `main = fn(args) {...}` — weave wvz only packages *importable* packages (weave_spec.md §17.2/§17.3, which may never declare their own entry point); to build or run it as a standalone program instead, use `weave build`/`weave run`", dir)
	}
	// A synthetic entry point, so the pipeline below (which always needs
	// a real Go func main() to reach `go build`) has something to
	// compile against — this proves the package's own code compiles;
	// it is never written to the archive itself.
	file.Main = &ast.FuncDecl{
		Name:  "main",
		Param: "_",
		Body:  []ast.Stmt{&ast.ReturnStmt{Value: &ast.NumberLit{Value: 0}}},
	}

	if err := validateBuilds(file); err != nil {
		return fmt.Errorf("%q does not build, refusing to archive it:\n%w", dir, err)
	}

	if err := writeWvz(outPath, matches); err != nil {
		return err
	}
	fmt.Println(outPath)
	return nil
}

// validateBuilds proves file actually compiles all the way through to a
// real Go binary — sema.Check, codegen.Generate, amivm, then `go build`
// — before weave wvz is willing to archive its source. Every artifact
// (the scratch Go module, the built binary) is discarded once the check
// passes; only the yes/no answer (and any error) survives.
func validateBuilds(file *ast.File) error {
	ir, err := generateIR(file)
	if err != nil {
		return err
	}
	_, workDir, err := irToGo(ir, false)
	if err != nil {
		return err
	}
	defer os.RemoveAll(workDir)

	tmpOut, err := os.MkdirTemp("", "weave-wvz-validate-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpOut)
	return goToBinary(workDir, filepath.Join(tmpOut, "a.out"))
}

// writeWvz zips the given .weave file paths into outPath, flat (each
// entry named by its own base name, no directory prefix).
func writeWvz(outPath string, weaveFiles []string) error {
	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	for _, path := range weaveFiles {
		content, err := os.ReadFile(path)
		if err != nil {
			zw.Close()
			return err
		}
		w, err := zw.Create(filepath.Base(path))
		if err != nil {
			zw.Close()
			return err
		}
		if _, err := w.Write(content); err != nil {
			zw.Close()
			return err
		}
	}
	return zw.Close()
}
