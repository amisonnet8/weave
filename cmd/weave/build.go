package main

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/amisonnet8/weave/internal/ast"
	"github.com/amisonnet8/weave/internal/codegen"
	"github.com/amisonnet8/weave/internal/modloader"
	"github.com/amisonnet8/weave/internal/sema"
	"github.com/amisonnet8/weave/weavert"
)

// generateIR runs sema.Check + codegen.Generate against an
// already-loaded *ast.File, returning the resulting AMIVM-IR text. Per
// CLAUDE.md, this (plus modloader.Load itself) is the full extent of
// what Weave itself is responsible for; everything past this point
// hands off to an external tool (amivm, then go build).
//
// Factored out of compileToIR so a caller that constructs/patches its
// own *ast.File can skip modloader.Load's entry-point requirement —
// `weave wvz`'s pre-archive build check (cmd/weave/wvz.go) uses this to
// validate a package-member directory (no `func main` of its own) by
// splicing in a synthetic one before calling this function, since
// codegen.Generate itself unconditionally dereferences file.Main.
func generateIR(file *ast.File) (string, error) {
	if err := sema.Check(file); err != nil {
		return "", fmt.Errorf("semantic error: %w", err)
	}
	ir, err := codegen.Generate(file)
	if err != nil {
		return "", fmt.Errorf("codegen error: %w", err)
	}
	return ir, nil
}

// compileToIR runs Weave's own share of the pipeline — modloader.Load
// (parsing plus, per weave_spec.md §15, resolving multi-file packages and
// imports into one flat *ast.File) followed by generateIR. srcPath may
// name either a single .weave file or a package directory (see
// modloader.Load's own doc for the distinction).
func compileToIR(srcPath string) (string, error) {
	file, err := modloader.Load(srcPath)
	if err != nil {
		return "", fmt.Errorf("load error: %w", err)
	}
	return generateIR(file)
}

// irToGo runs ir through amivm, returning the generated Go source and
// the scratch module directory it was written into. The caller owns
// workDir and must remove it once done — compileToBinary (and `weave
// wvz`'s own pre-archive build check) keeps it around to run `go build`
// in the same module.
//
// amivm's own build requires its output directory to be a Go module (its
// cross-package type-checking is module-aware), so the Go file is
// generated inside a scratch module rather than as a bare file.
//
// verbose prints the IR before invoking amivm, passes -v through to
// amivm (showing its own type-checking trace and the final Go source),
// and echoes amivm's output — mirroring amivm's own -v behavior
// (CLAUDE.md) at each stage of Weave's larger pipeline.
func irToGo(ir string, verbose bool) (goSrc, workDir string, err error) {
	if verbose {
		fmt.Println("=== AMIVM-IR ===")
		fmt.Print(ir)
	}

	workDir, err = os.MkdirTemp("", "weave-build-*")
	if err != nil {
		return "", "", err
	}
	cleanup := func() { os.RemoveAll(workDir) }

	modPath := filepath.Join(workDir, "go.mod")
	if err := os.WriteFile(modPath, []byte("module weavebuild\n\ngo 1.21\n"), 0o644); err != nil {
		cleanup()
		return "", "", err
	}
	if err := writeWeavert(workDir); err != nil {
		cleanup()
		return "", "", err
	}

	irPath := filepath.Join(workDir, "main.ir")
	if err := os.WriteFile(irPath, []byte(ir), 0o644); err != nil {
		cleanup()
		return "", "", err
	}

	goPath := filepath.Join(workDir, "main.go")
	// -i is safe to pass unconditionally even for a program that never
	// calls into weavert: amivm drops an unused import mapping on its
	// own (see ignored/seed/CLAUDE.md's amivm CLI notes).
	amivmArgs := []string{irPath, "-o", goPath, "-i", "weavert=weavebuild/weavert"}
	if verbose {
		amivmArgs = append(amivmArgs, "-v")
	}
	out, runErr := exec.Command("amivm", amivmArgs...).CombinedOutput()
	if verbose && len(out) > 0 {
		os.Stdout.Write(out)
	}
	if runErr != nil {
		cleanup()
		if verbose {
			return "", "", fmt.Errorf("amivm failed")
		}
		return "", "", fmt.Errorf("amivm:\n%s", out)
	}

	goBytes, err := os.ReadFile(goPath)
	if err != nil {
		cleanup()
		return "", "", err
	}
	return string(goBytes), workDir, nil
}

// compileToGo compiles srcPath to IR (compileToIR) and runs it through
// amivm (irToGo), returning the generated Go source and the scratch
// module directory it was written into.
func compileToGo(srcPath string, verbose bool) (goSrc, workDir string, err error) {
	ir, err := compileToIR(srcPath)
	if err != nil {
		return "", "", err
	}
	return irToGo(ir, verbose)
}

// goToBinary runs `go build` inside workDir (a scratch module produced
// by irToGo), writing the resulting executable to outPath.
func goToBinary(workDir, outPath string) error {
	absOut, err := filepath.Abs(outPath)
	if err != nil {
		return err
	}
	buildCmd := exec.Command("go", "build", "-o", absOut, ".")
	buildCmd.Dir = workDir
	if out, err := buildCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go build:\n%s", out)
	}
	return nil
}

// compileToBinary runs the full Weave → AMIVM-IR → Go → binary pipeline
// and writes the resulting executable to outPath.
func compileToBinary(srcPath, outPath string, verbose bool) error {
	_, workDir, err := compileToGo(srcPath, verbose)
	if err != nil {
		return err
	}
	defer os.RemoveAll(workDir)
	return goToBinary(workDir, outPath)
}

// writeWeavert copies weavert's own embedded source (see
// weavert/embed.go) into workDir/weavert, so it becomes an ordinary
// subpackage of the scratch build module — "weavebuild/weavert" — with
// no separate module or replace directive needed. embed.go itself is
// skipped: copying it too would work (its own //go:embed *.go would
// just re-embed the copy, harmlessly), but the embed.FS it declares
// serves no purpose once copied out. Mirrors Seed's writeSeedrt
// (ignored/seed/cmd/seed/build.go).
func writeWeavert(workDir string) error {
	dir := filepath.Join(workDir, "weavert")
	if err := os.Mkdir(dir, 0o755); err != nil {
		return err
	}
	return fs.WalkDir(weavert.Source, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || name == "embed.go" {
			return nil
		}
		content, err := fs.ReadFile(weavert.Source, name)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, name), content, 0o644)
	})
}
