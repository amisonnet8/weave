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

func TestTokenize_StringEscapes(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"named escapes", `"\a\b\f\n\r\t\v"`, "\a\b\f\n\r\t\v"},
		{"backslash", `"\\"`, `\`},
		{"double quote", `"say \"hi\""`, `say "hi"`},
		{"hex byte", `"\x41"`, "A"},
		{"octal byte", `"\101"`, "A"},
		{"unicode 4-hex", `"é"`, "é"},
		{"unicode 8-hex", `"\U0001F600"`, "😀"},
		{"raw non-ASCII passes through unchanged", `"あ"`, "あ"},
		{"mixed", `"a\tb\nc"`, "a\tb\nc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toks, err := Tokenize(tt.src)
			if err != nil {
				t.Fatalf("Tokenize(%s): %v", tt.src, err)
			}
			if len(toks) < 1 || toks[0].Kind != String {
				t.Fatalf("Tokenize(%s): got %+v, want a single String token", tt.src, toks)
			}
			if toks[0].Literal != tt.want {
				t.Errorf("Tokenize(%s) = %q, want %q", tt.src, toks[0].Literal, tt.want)
			}
		})
	}
}

func TestTokenize_StringEscapeErrors(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{"unknown escape", `"\q"`},
		{"single quote not valid in a string (rune-only in Go)", `"\'"`},
		{"incomplete hex byte", `"\x4"`},
		{"non-hex digit in \\x", `"\xzz"`},
		{"incomplete unicode 4-hex", `"\u12"`},
		{"incomplete unicode 8-hex", `"\U0001F60"`},
		{"octal too short", `"\45"`},
		{"octal value exceeds 255", `"\777"`},
		{"backslash at end of input", `"\`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Tokenize(tt.src); err == nil {
				t.Errorf("Tokenize(%s): expected an error", tt.src)
			}
		})
	}
}
