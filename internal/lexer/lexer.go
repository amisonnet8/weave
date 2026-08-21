package lexer

import (
	"fmt"
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
	l.skipSpacesAndComments()

	line := l.line
	if l.pos >= len(l.src) {
		return Token{Kind: EOF, Line: line}, nil
	}

	r := l.peekRune()

	if r == '\n' {
		l.skipNewlines()
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

// skipSpacesAndComments skips spaces, tabs, and // comments, but leaves
// newlines in place: they are significant statement terminators
// (weave_spec.md §1).
func (l *Lexer) skipSpacesAndComments() {
	for l.pos < len(l.src) {
		r := l.peekRune()
		switch {
		case r == ' ' || r == '\t' || r == '\r':
			l.pos++
		case r == '/' && l.peekRuneAt(1) == '/':
			for l.pos < len(l.src) && l.peekRune() != '\n' {
				l.pos++
			}
		default:
			return
		}
	}
}

// skipNewlines consumes one or more newlines (interleaved with
// whitespace/comments), collapsing them into a single Newline token.
func (l *Lexer) skipNewlines() {
	for {
		l.skipSpacesAndComments()
		if l.pos < len(l.src) && l.peekRune() == '\n' {
			l.advanceRune()
			continue
		}
		return
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
		b.WriteRune(r)
		l.pos++
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
