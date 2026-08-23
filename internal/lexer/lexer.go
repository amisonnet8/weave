package lexer

import (
	"fmt"
	"strconv"
	"strings"
)

// Lexer tokenizes Weave source code (weave_spec.md §1, §3, §8, §13).
type Lexer struct {
	src  []rune
	pos  int
	line int
}

// New creates a Lexer over src.
func New(src string) *Lexer {
	return &Lexer{src: []rune(src), pos: 0, line: 1}
}

// Tokenize scans the whole source and returns its token stream, ending
// with a single EOF token.
func Tokenize(src string) ([]Token, error) {
	l := New(src)
	var toks []Token
	for {
		tok, err := l.next()
		if err != nil {
			return nil, err
		}
		toks = append(toks, tok)
		if tok.Kind == EOF {
			return toks, nil
		}
	}
}

func (l *Lexer) peekRune() rune {
	if l.pos >= len(l.src) {
		return 0
	}
	return l.src[l.pos]
}

func (l *Lexer) peekRuneAt(offset int) rune {
	if l.pos+offset >= len(l.src) {
		return 0
	}
	return l.src[l.pos+offset]
}

func (l *Lexer) advanceRune() rune {
	r := l.src[l.pos]
	l.pos++
	if r == '\n' {
		l.line++
	}
	return r
}

func (l *Lexer) next() (Token, error) {
	if err := l.skipSpacesAndComments(); err != nil {
		return Token{}, err
	}

	line := l.line
	if l.pos >= len(l.src) {
		return Token{Kind: EOF, Line: line}, nil
	}

	r := l.peekRune()

	if r == '\n' {
		if err := l.skipNewlines(); err != nil {
			return Token{}, err
		}
		return Token{Kind: Newline, Line: line}, nil
	}

	switch {
	case isIdentStart(r):
		return l.lexIdent(line), nil
	case isDigit(r):
		return l.lexNumber(line), nil
	case r == '"':
		return l.lexString(line)
	}

	return l.lexOperator(line)
}

// skipSpacesAndComments skips spaces, tabs, `//` line comments, and
// `/* ... */` block comments, but leaves newlines in place: they are
// significant statement terminators (weave_spec.md §1). A block comment
// is always treated as pure whitespace — even one spanning several
// physical lines never itself produces a Newline token (only an actual
// `\n` in the source outside of a comment does); this keeps Weave's
// newline-is-a-terminator model simple, unlike Go's own block comments,
// which participate in automatic semicolon insertion. Block comments
// don't nest (same as Go): the first `*/` found closes the comment,
// regardless of how many `/*` appeared before it.
func (l *Lexer) skipSpacesAndComments() error {
	for l.pos < len(l.src) {
		r := l.peekRune()
		switch {
		case r == ' ' || r == '\t' || r == '\r':
			l.pos++
		case r == '/' && l.peekRuneAt(1) == '/':
			for l.pos < len(l.src) && l.peekRune() != '\n' {
				l.pos++
			}
		case r == '/' && l.peekRuneAt(1) == '*':
			if err := l.skipBlockComment(); err != nil {
				return err
			}
		default:
			return nil
		}
	}
	return nil
}

// skipBlockComment consumes a `/* ... */` comment, positioned at the
// opening `/`. advanceRune (not a bare l.pos++) tracks line numbers as it
// goes, so a comment spanning several lines still leaves l.line correct
// for whatever token follows it.
func (l *Lexer) skipBlockComment() error {
	startLine := l.line
	l.advanceRune() // '/'
	l.advanceRune() // '*'
	for {
		if l.pos >= len(l.src) {
			return fmt.Errorf("line %d: unterminated block comment", startLine)
		}
		if l.peekRune() == '*' && l.peekRuneAt(1) == '/' {
			l.advanceRune() // '*'
			l.advanceRune() // '/'
			return nil
		}
		l.advanceRune()
	}
}

// skipNewlines consumes one or more newlines (interleaved with
// whitespace/comments), collapsing them into a single Newline token.
func (l *Lexer) skipNewlines() error {
	for {
		if err := l.skipSpacesAndComments(); err != nil {
			return err
		}
		if l.pos < len(l.src) && l.peekRune() == '\n' {
			l.advanceRune()
			continue
		}
		return nil
	}
}

func isIdentStart(r rune) bool {
	return r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func isIdentPart(r rune) bool {
	return isIdentStart(r) || isDigit(r)
}

func isDigit(r rune) bool {
	return r >= '0' && r <= '9'
}

func (l *Lexer) lexIdent(line int) Token {
	start := l.pos
	for l.pos < len(l.src) && isIdentPart(l.peekRune()) {
		l.pos++
	}
	lit := string(l.src[start:l.pos])
	if kw, ok := keywords[lit]; ok {
		return Token{Kind: kw, Literal: lit, Line: line}
	}
	return Token{Kind: Ident, Literal: lit, Line: line}
}

// lexNumber scans an integer or float literal (weave_spec.md §3). Weave
// does not distinguish integers from floats at the language level (§2),
// but the lexer still recognizes both literal shapes; the parser folds
// them into a single numeric literal value.
func (l *Lexer) lexNumber(line int) Token {
	start := l.pos
	for l.pos < len(l.src) && isDigit(l.peekRune()) {
		l.pos++
	}
	if l.peekRune() == '.' && isDigit(l.peekRuneAt(1)) {
		l.pos++ // consume '.'
		for l.pos < len(l.src) && isDigit(l.peekRune()) {
			l.pos++
		}
	}
	return Token{Kind: Number, Literal: string(l.src[start:l.pos]), Line: line}
}

// lexString scans a double-quoted string literal (weave_spec.md §3),
// decoding backslash escapes as it goes so that ast.StringLit.Value
// always holds the true runtime string content — never the raw escaped
// source text. This mirrors Go's own lexer (which decodes an interpreted
// string literal's escapes at parse time, not at compile time), and is
// what lets codegen.go's `strconv.Quote(e.Value)` round-trip correctly:
// Quote re-escapes whatever the decoded value actually contains (e.g. a
// real newline byte becomes `\n` again in the emitted IR token), rather
// than double-escaping already-literal backslash text.
func (l *Lexer) lexString(line int) (Token, error) {
	l.pos++ // consume opening '"'
	var b strings.Builder
	for {
		if l.pos >= len(l.src) {
			return Token{}, fmt.Errorf("line %d: unterminated string literal", line)
		}
		r := l.peekRune()
		if r == '"' {
			l.pos++
			return Token{Kind: String, Literal: b.String(), Line: line}, nil
		}
		if r == '\n' {
			return Token{}, fmt.Errorf("line %d: unterminated string literal", line)
		}
		if r == '\\' {
			if err := l.lexEscape(line, &b); err != nil {
				return Token{}, err
			}
			continue
		}
		b.WriteRune(r)
		l.pos++
	}
}

// lexEscape decodes one backslash escape sequence inside a string
// literal, starting at the '\' itself. The accepted forms match Go's own
// escape rules for interpreted (double-quoted) string literals exactly
// — `\'` (valid only in Go *rune* literals) is deliberately not accepted
// here, since Weave has no rune/char literal type to share it with (§2:
// Weave's only textual value is `string`). AMIVM's own string-literal
// token classification (amivm_spec.md §5, "'\'に続く任意の1文字'"、
// 個々のエスケープの妥当性はgo/typesの再パースに委ねる) is intentionally
// permissive and leaves real validation to Go — Weave does the opposite,
// rejecting anything Go itself wouldn't accept right here at lex time,
// so a typo like `\q` becomes a clear Weave-level error instead of a
// confusing amivm/go/types failure downstream (weave_spec.md §1 and
// CLAUDE.md's "意味検証の責任分担": IR-facing syntactic correctness is
// Weave's own job, not amivm's).
func (l *Lexer) lexEscape(line int, b *strings.Builder) error {
	l.pos++ // consume '\'
	if l.pos >= len(l.src) {
		return fmt.Errorf("line %d: unterminated string literal", line)
	}
	switch r := l.peekRune(); r {
	case 'a':
		b.WriteByte('\a')
		l.pos++
	case 'b':
		b.WriteByte('\b')
		l.pos++
	case 'f':
		b.WriteByte('\f')
		l.pos++
	case 'n':
		b.WriteByte('\n')
		l.pos++
	case 'r':
		b.WriteByte('\r')
		l.pos++
	case 't':
		b.WriteByte('\t')
		l.pos++
	case 'v':
		b.WriteByte('\v')
		l.pos++
	case '\\':
		b.WriteByte('\\')
		l.pos++
	case '"':
		b.WriteByte('"')
		l.pos++
	case 'x':
		l.pos++
		v, err := l.readFixedDigits(line, 2, 16, "hex")
		if err != nil {
			return err
		}
		b.WriteByte(byte(v))
	case 'u':
		l.pos++
		v, err := l.readFixedDigits(line, 4, 16, "hex")
		if err != nil {
			return err
		}
		b.WriteRune(rune(v))
	case 'U':
		l.pos++
		v, err := l.readFixedDigits(line, 8, 16, "hex")
		if err != nil {
			return err
		}
		b.WriteRune(rune(v))
	case '0', '1', '2', '3', '4', '5', '6', '7':
		v, err := l.readFixedDigits(line, 3, 8, "octal")
		if err != nil {
			return err
		}
		if v > 255 {
			return fmt.Errorf("line %d: octal escape value %d exceeds 255", line, v)
		}
		b.WriteByte(byte(v))
	default:
		return fmt.Errorf("line %d: unknown escape sequence \\%c in string literal", line, r)
	}
	return nil
}

// readFixedDigits reads exactly n digits in the given base (16 for
// \x/\u/\U, 8 for octal \NNN — Go requires the exact count in every
// case, not "as many as match"), returning their combined value.
func (l *Lexer) readFixedDigits(line int, n, base int, kind string) (int64, error) {
	start := l.pos
	for i := 0; i < n; i++ {
		if l.pos >= len(l.src) || l.peekRune() == '\n' {
			return 0, fmt.Errorf("line %d: unterminated string literal", line)
		}
		if digitValue(l.peekRune()) >= base {
			return 0, fmt.Errorf("line %d: invalid %s escape (need %d digits)", line, kind, n)
		}
		l.pos++
	}
	v, err := strconv.ParseInt(string(l.src[start:l.pos]), base, 64)
	if err != nil {
		return 0, fmt.Errorf("line %d: invalid %s escape: %w", line, kind, err)
	}
	return v, nil
}

// digitValue returns r's value as a base-36 digit (0-9, then a-z/A-Z),
// or a value >= 36 if r isn't a digit at all — callers only care whether
// the result is below their own base.
func digitValue(r rune) int {
	switch {
	case r >= '0' && r <= '9':
		return int(r - '0')
	case r >= 'a' && r <= 'z':
		return int(r-'a') + 10
	case r >= 'A' && r <= 'Z':
		return int(r-'A') + 10
	default:
		return 99
	}
}

func (l *Lexer) lexOperator(line int) (Token, error) {
	r := l.advanceRune()
	two := func(next rune, twoKind, oneKind Kind) Token {
		if l.peekRune() == next {
			l.pos++
			return Token{Kind: twoKind, Literal: string(r) + string(next), Line: line}
		}
		return Token{Kind: oneKind, Literal: string(r), Line: line}
	}

	switch r {
	case '(':
		return Token{Kind: LParen, Literal: "(", Line: line}, nil
	case ')':
		return Token{Kind: RParen, Literal: ")", Line: line}, nil
	case '{':
		return Token{Kind: LBrace, Literal: "{", Line: line}, nil
	case '}':
		return Token{Kind: RBrace, Literal: "}", Line: line}, nil
	case '[':
		return Token{Kind: LBracket, Literal: "[", Line: line}, nil
	case ']':
		return Token{Kind: RBracket, Literal: "]", Line: line}, nil
	case ',':
		return Token{Kind: Comma, Literal: ",", Line: line}, nil
	case ':':
		return Token{Kind: Colon, Literal: ":", Line: line}, nil
	case '.':
		return Token{Kind: Dot, Literal: ".", Line: line}, nil
	case '+':
		return Token{Kind: Plus, Literal: "+", Line: line}, nil
	case '-':
		return Token{Kind: Minus, Literal: "-", Line: line}, nil
	case '*':
		return Token{Kind: Star, Literal: "*", Line: line}, nil
	case '/':
		return Token{Kind: Slash, Literal: "/", Line: line}, nil
	case '%':
		return Token{Kind: Percent, Literal: "%", Line: line}, nil
	case '=':
		return two('=', Eq, Assign), nil
	case '!':
		return two('=', Neq, Not), nil
	case '<':
		return two('=', Lte, Lt), nil
	case '>':
		return two('=', Gte, Gt), nil
	case '&':
		if l.peekRune() == '&' {
			l.pos++
			return Token{Kind: AndAnd, Literal: "&&", Line: line}, nil
		}
	case '|':
		if l.peekRune() == '|' {
			l.pos++
			return Token{Kind: OrOr, Literal: "||", Line: line}, nil
		}
	}

	return Token{}, fmt.Errorf("line %d: unexpected character %q", line, r)
}
