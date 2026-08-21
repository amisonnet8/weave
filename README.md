# Weave

A dynamically-typed programming language, implemented in Go, that unifies property lookup, method dispatch, and actor message passing behind a single mechanism (name-based property search over a prototype chain) — compiling to Go source via AMIVM-IR.

> [日本語版 README はこちら](README_ja.md)

## Status

Weave's front end (lexer, parser, semantic checker, and AMIVM-IR code generator) implements the language described in [`weave_spec.md`](weave_spec.md): dynamic values (numbers, strings, booleans, `nil`), operators, control flow, functions/closures/currying, prototype-based objects and method dispatch, built-in functions, `for`-`in`, and the actor model (`spawn`/`send`/`ask`/`reply`). Static Go-asset integration (`gotype`/`gofunc`/`gomethod`, §15–16) is not implemented yet.

## Pipeline

```
Weave source (.weave)
  ↓ (Weave — this repository)
AMIVM-IR (.ir)
  ↓ amivm (external tool, github.com/amisonnet8/amivm)
Go source (.go)
  ↓ go build
executable
```

Weave's own responsibility stops at emitting AMIVM-IR. Turning that into Go source is [amivm](https://github.com/amisonnet8/amivm)'s job, and turning that into an executable is a plain `go build` — both are separate tools `weave` shells out to, not something this repository implements itself.

## Requirements

- Go, matching the version in [`go.mod`](go.mod).
- [`amivm`](https://github.com/amisonnet8/amivm) on your `PATH`.

## Install

```sh
go install github.com/amisonnet8/amivm/cmd/amivm@latest
go install github.com/amisonnet8/weave/cmd/weave@latest
```

Both land in `$GOBIN` (or `$GOPATH/bin` if unset) — make sure that directory is on your `PATH`. Since every Weave build ends in a plain `go build`, having Go installed already covers every dependency `weave` needs at runtime; there's nothing else to fetch.

## Usage

```
weave <command> [flags] <file.weave>
```

| Command | Output |
|---|---|
| `build` | a native executable |
| `run` | compiles and immediately runs, streaming its stdin/stdout/stderr |
| `emit-ir` | the AMIVM-IR |
| `emit-go` | the Go source (via amivm) |
| `help` | this command list |

`build`, `emit-ir`, and `emit-go` accept:

| Flag | Description |
|---|---|
| `-o <file>` | output file path (default: derived from the input path, e.g. `foo.weave` → `foo`/`foo.ir`/`foo.go`) |
| `-v` | show each pipeline stage's output as it runs (the generated IR, amivm's own `-v` trace, the final Go source) |

## Example

```weave
func main(): int {
	base = {
		greet: fn(self) { print(self.name + " says hi") }
	}
	alice = { __proto__: base, name: "Alice" }
	alice.greet()

	add = fn(a) fn(b) { return a + b }
	print(add(5)(3))

	return 0
}
```

```sh
$ weave run hello.weave
Alice says hi
8
```

More runnable examples covering scalars, operators, control flow, closures/currying, objects/prototypes, built-ins/`for`-`in`, and actors live in [`examples/`](examples/).

## Language

**The only authoritative specification is [`weave_spec.md`](weave_spec.md).** If any other document (including this README) disagrees with it, `weave_spec.md` wins.

## Repository layout

```
cmd/weave/          CLI entry point (this README's `weave` commands)
internal/lexer/      tokenizing
internal/parser/     parsing → AST
internal/ast/        AST definitions
internal/sema/       semantic analysis (scope resolution, syntax-level checks — Weave's
                      dynamic typing means most value-type errors are a runtime concern
                      instead; see CLAUDE.md)
internal/codegen/    AST → AMIVM-IR
weavert/             Weave's Go runtime library (every Weave value is dynamically typed,
                      so operators, objects, closures, and actors all route through here
                      rather than AMIVM's native instructions — see CLAUDE.md), embedded
                      into every weave build
examples/            runnable .weave sample programs, one group per language feature
weave_spec.md        the Weave language specification (the only authoritative one)
CLAUDE.md            project conventions for AI-assisted development
```
