package lexer

import "testing"

func TestTokenize_HelloWorld(t *testing.T) {
	src := "func main(): int {\n\tprint(\"Hello, Weave!\")\n\treturn 0\n}\n"
	toks, err := Tokenize(src)
	if err != nil {
		t.Fatalf("Tokenize: %v", err)
	}

	want := []Kind{
		KwFunc, Ident, LParen, RParen, Colon, Ident, LBrace, Newline,
		Ident, LParen, String, RParen, Newline,
		KwReturn, Number, Newline,
		RBrace, Newline,
		EOF,
	}
	if len(toks) != len(want) {
		t.Fatalf("got %d tokens, want %d: %+v", len(toks), len(want), toks)
	}
	for i, k := range want {
		if toks[i].Kind != k {
			t.Errorf("token %d: got Kind %d (%q), want %d", i, toks[i].Kind, toks[i].Literal, k)
		}
	}
	if toks[10].Literal != "Hello, Weave!" {
		t.Errorf("string literal: got %q", toks[10].Literal)
	}
}

func TestTokenize_CommentsAndBlankLinesCollapseToOneNewline(t *testing.T) {
	src := "a // comment\n\n\nb\n"
	toks, err := Tokenize(src)
	if err != nil {
		t.Fatalf("Tokenize: %v", err)
	}
	want := []Kind{Ident, Newline, Ident, Newline, EOF}
	if len(toks) != len(want) {
		t.Fatalf("got %d tokens, want %d: %+v", len(toks), len(want), toks)
	}
}

func TestTokenize_UnterminatedString(t *testing.T) {
	if _, err := Tokenize(`"abc`); err == nil {
		t.Fatal("expected an error for an unterminated string literal")
	}
}
