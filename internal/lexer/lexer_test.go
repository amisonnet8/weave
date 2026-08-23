package lexer

import "testing"

func TestTokenize_HelloWorld(t *testing.T) {
	src := "main = fn(args) {\n\tprint(\"Hello, Weave!\")\n\treturn 0\n}\n"
	toks, err := Tokenize(src)
	if err != nil {
		t.Fatalf("Tokenize: %v", err)
	}

	want := []Kind{
		Ident, Assign, KwFn, LParen, Ident, RParen, LBrace, Newline,
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

func TestTokenize_BlockCommentMidLine(t *testing.T) {
	// weave_spec.md §1: /* ... */ can appear mid-line, same as Go.
	src := "a /* comment */ b\n"
	toks, err := Tokenize(src)
	if err != nil {
		t.Fatalf("Tokenize: %v", err)
	}
	want := []Kind{Ident, Ident, Newline, EOF}
	if len(toks) != len(want) {
		t.Fatalf("got %d tokens, want %d: %+v", len(toks), len(want), toks)
	}
	for i, k := range want {
		if toks[i].Kind != k {
			t.Errorf("token %d: got Kind %d, want %d", i, toks[i].Kind, k)
		}
	}
}

func TestTokenize_BlockCommentSpanningLinesActsAsPureWhitespace(t *testing.T) {
	// A block comment containing a newline does NOT itself produce a
	// Newline token — unlike Go's ASI (where a multi-line block comment
	// counts as a newline for semicolon insertion), Weave's newlines are
	// literal terminators (weave_spec.md §1) with no synthesized-token
	// concept to begin with, so the simpler, consistent choice is: only
	// an actual `\n` outside any comment ends a statement.
	src := "a /* line one\nline two */ b\n"
	toks, err := Tokenize(src)
	if err != nil {
		t.Fatalf("Tokenize: %v", err)
	}
	want := []Kind{Ident, Ident, Newline, EOF}
	if len(toks) != len(want) {
		t.Fatalf("got %d tokens, want %d: %+v", len(toks), len(want), toks)
	}
	for i, k := range want {
		if toks[i].Kind != k {
			t.Errorf("token %d: got Kind %d, want %d", i, toks[i].Kind, k)
		}
	}
}

func TestTokenize_BlockCommentsDoNotNest(t *testing.T) {
	// weave_spec.md §1: block comments don't nest, same as Go — the
	// first `*/` closes the comment regardless of any `/*` seen since.
	// "/* /* */ */" therefore leaves a stray " */" as real tokens after
	// the comment closes at the first "*/".
	src := "/* /* */ */\n"
	toks, err := Tokenize(src)
	if err != nil {
		t.Fatalf("Tokenize: %v", err)
	}
	// After the comment "/* /* */" closes, "*/" remains: a Star token
	// then a Slash token (neither combines into a single operator).
	want := []Kind{Star, Slash, Newline, EOF}
	if len(toks) != len(want) {
		t.Fatalf("got %d tokens, want %d: %+v", len(toks), len(want), toks)
	}
	for i, k := range want {
		if toks[i].Kind != k {
			t.Errorf("token %d: got Kind %d, want %d", i, toks[i].Kind, k)
		}
	}
}

func TestTokenize_UnterminatedBlockCommentIsAnError(t *testing.T) {
	if _, err := Tokenize("a /* never closed\n"); err == nil {
		t.Fatal("expected an error for an unterminated block comment")
	}
}

func TestTokenize_LineCommentInsideBlockCommentIsLiteralText(t *testing.T) {
	// "//" has no special meaning once inside a block comment — only the
	// closing "*/" matters.
	src := "a /* // still a comment */ b\n"
	toks, err := Tokenize(src)
	if err != nil {
		t.Fatalf("Tokenize: %v", err)
	}
	want := []Kind{Ident, Ident, Newline, EOF}
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
