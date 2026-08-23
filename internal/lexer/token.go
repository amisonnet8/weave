package lexer

// Kind identifies the lexical category of a Token.
type Kind int

const (
	EOF Kind = iota
	Newline
	Ident
	Number
	String

	// structural keywords (weave_spec.md §13). Builtin function names
	// (print, string, len, has, remove, list, spawn, send, ask, reply,
	// gotype, gofunc, gomethod, package) are reserved words but not
	// syntax keywords: they lex as plain Ident and are resolved as
	// ordinary calls, matching Seed/Cascade's convention. This
	// includes package(...) (weave_spec.md §17.2) — there is
	// deliberately no `import`/`pub` syntax; see modloader's package
	// doc comment. `main` (the entry point, weave_spec.md §12) follows
	// this same pattern now too — `main = fn(args) {...}` is an
	// ordinary-looking assignment, not a keyword; the `func` keyword
	// that used to introduce it has been retired entirely, since
	// nothing else in the language ever used it.
	KwFn
	KwTrue
	KwFalse
	KwNil
	KwIf
	KwElif
	KwElse
	KwWhile
	KwFor
	KwIn
	KwBreak
	KwContinue
	KwReturn

	// punctuation
	LParen
	RParen
	LBrace
	RBrace
	LBracket
	RBracket
	Comma
	Colon
	Dot

	// operators (weave_spec.md §8)
	Plus
	Minus
	Star
	Slash
	Percent
	Assign
	Eq
	Neq
	Lt
	Lte
	Gt
	Gte
	AndAnd
	OrOr
	Not
)

var keywords = map[string]Kind{
	"fn":       KwFn,
	"true":     KwTrue,
	"false":    KwFalse,
	"nil":      KwNil,
	"if":       KwIf,
	"elif":     KwElif,
	"else":     KwElse,
	"while":    KwWhile,
	"for":      KwFor,
	"in":       KwIn,
	"break":    KwBreak,
	"continue": KwContinue,
	"return":   KwReturn,
}

// Token is a single lexical token together with its source line (1-based).
type Token struct {
	Kind    Kind
	Literal string
	Line    int
}
