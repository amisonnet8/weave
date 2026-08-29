// Command weave is the Weave language toolchain: it compiles .weave
// source through AMIVM-IR (via the external amivm CLI) and go build into a
// native executable.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) < 1 {
		printUsage(os.Stderr)
		return fmt.Errorf("no command given")
	}

	cmd, rest := args[0], args[1:]
	switch cmd {
	case "build":
		return runBuild(rest)
	case "run":
		return runRun(rest)
	case "emit-ir":
		return runEmitIR(rest)
	case "emit-go":
		return runEmitGo(rest)
	case "wvz":
		return runWvz(rest)
	case "help", "-h", "--help":
		printUsage(os.Stdout)
		return nil
	default:
		printUsage(os.Stderr)
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `Weave is a compiler for the Weave programming language.

Usage:

	weave <command> [flags] <file.weave | package-dir | package.wvz>

A package-dir (or a .wvz archive of one, weave_spec.md §15.6) compiles
every .weave file in that package as one program; a single file compiles
only that file, ignoring any siblings in the same directory.

Commands:

	build      compile to a native executable
	run        compile and immediately run, streaming its stdin/stdout/stderr
	emit-ir    compile to AMIVM-IR
	emit-go    compile to Go source (via amivm)
	wvz        package a directory's .weave files into a .wvz archive
	help       show this help message

Flags (build, emit-ir, emit-go):

	-o <file>  output file path (default: derived from the input path)
	-v         show each pipeline stage's output as it runs

Flags (wvz):

	-o <file>  output .wvz path (default: <directory-name>.wvz)
`)
}

// outputFlags are the -o/-v flags shared by build, emit-ir, and emit-go.
func outputFlags(name string) (fs *flag.FlagSet, out *string, verbose *bool) {
	fs = flag.NewFlagSet(name, flag.ContinueOnError)
	out = fs.String("o", "", "output file path")
	verbose = fs.Bool("v", false, "show each pipeline stage's output as it runs")
	return fs, out, verbose
}

// parseOneSrcArg parses fs against args and returns the single required
// <file.weave> positional argument.
func parseOneSrcArg(fs *flag.FlagSet, args []string) (string, error) {
	if err := fs.Parse(args); err != nil {
		return "", err
	}
	if fs.NArg() != 1 {
		return "", fmt.Errorf("usage: weave %s [-o file] [-v] <file.weave | package-dir>", fs.Name())
	}
	return fs.Arg(0), nil
}

func runBuild(args []string) error {
	fs, out, verbose := outputFlags("build")
	srcPath, err := parseOneSrcArg(fs, args)
	if err != nil {
		return err
	}
	outPath := *out
	if outPath == "" {
		outPath = defaultOutPath(srcPath, "")
	}
	if err := compileToBinary(srcPath, outPath, *verbose); err != nil {
		return err
	}
	fmt.Println(outPath)
	return nil
}

func runRun(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: weave run <file.weave | package-dir>")
	}
	srcPath := args[0]

	tmp, err := os.MkdirTemp("", "weave-run-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	binPath := filepath.Join(tmp, "a.out")
	if err := compileToBinary(srcPath, binPath, false); err != nil {
		return err
	}

	runCmd := exec.Command(binPath)
	runCmd.Stdin = os.Stdin
	runCmd.Stdout = os.Stdout
	runCmd.Stderr = os.Stderr
	if err := runCmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		return err
	}
	return nil
}

func runEmitIR(args []string) error {
	fs, out, verbose := outputFlags("emit-ir")
	srcPath, err := parseOneSrcArg(fs, args)
	if err != nil {
		return err
	}

	ir, err := compileToIR(srcPath)
	if err != nil {
		return err
	}
	if *verbose {
		fmt.Println("=== AMIVM-IR ===")
		fmt.Print(ir)
	}

	outPath := *out
	if outPath == "" {
		outPath = defaultOutPath(srcPath, ".ir")
	}
	if err := os.WriteFile(outPath, []byte(ir), 0o644); err != nil {
		return err
	}
	fmt.Println(outPath)
	return nil
}

func runEmitGo(args []string) error {
	fs, out, verbose := outputFlags("emit-go")
	srcPath, err := parseOneSrcArg(fs, args)
	if err != nil {
		return err
	}

	goSrc, workDir, err := compileToGo(srcPath, *verbose)
	if err != nil {
		return err
	}
	defer os.RemoveAll(workDir)

	outPath := *out
	if outPath == "" {
		outPath = defaultOutPath(srcPath, ".go")
	}
	if err := os.WriteFile(outPath, []byte(goSrc), 0o644); err != nil {
		return err
	}
	fmt.Println(outPath)
	return nil
}

// defaultOutPath derives an output path from srcPath by stripping its
// .weave/.wvz extension and appending ext (e.g. ".ir", ".go", or "" for
// a build's bare executable name).
//
// srcPath may name a package directory or a .wvz archive rather than a
// single file (weave_spec.md §15.1, §15.6) — TrimSuffix(".weave") would
// then be a no-op, which for a directory would otherwise leave the
// output path identical to the source directory itself (`go build -o
// <existing dir>` writes *into* that directory instead of naming a new
// file — silently dropping a build artifact into the source tree). So a
// directory or .wvz srcPath instead derives the name from its own base
// name (extension stripped for .wvz), written to the current directory —
// matching `go build ./mypackage`'s own default (no -o) of naming the
// binary after the package directory.
func defaultOutPath(srcPath, ext string) string {
	if info, err := os.Stat(srcPath); err == nil && info.IsDir() {
		return filepath.Base(srcPath) + ext
	}
	if strings.EqualFold(filepath.Ext(srcPath), ".wvz") {
		return strings.TrimSuffix(filepath.Base(srcPath), filepath.Ext(srcPath)) + ext
	}
	return strings.TrimSuffix(srcPath, ".weave") + ext
}
