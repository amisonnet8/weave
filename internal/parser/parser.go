// Package parser implements a hand-written recursive-descent parser
// (Seed/Cascade's established approach — see CLAUDE.md's ドキュメント構成
// notes on why a parser generator is not used).
package parser

import (
	"fmt"
	"strconv"

	"github.com/amisonnet8/weave/internal/ast"
	"github.com/amisonnet8/weave/internal/lexer"
)

type parser struct {
	toks []lexer.Token
	pos  int
}

// Parse tokenizes and parses src into a *ast.File.
//
// Step 1 scope: only `func main(): int { ... }` is recognized at the top
// level, with a statement grammar limited to bare calls and `return`.
// The parser deliberately stays permissive about *names* it doesn't yet
// understand semantically (e.g. it accepts any identifier as the
// function name or return type) — internal/sema is where those rules
// are enforced, matching Seed/Cascade's split of responsibilities.
func Parse(src string) (*ast.File, error) {
	toks, err := lexer.Tokenize(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	return p.parseFile()
}

func (p *parser) peek() lexer.Token { return p.toks[p.pos] }

func (p *parser) advance() lexer.Token {
	t := p.toks[p.pos]
	if p.pos < len(p.toks)-1 {
		p.pos++
	}
	return t
}

func (p *parser) skipNewlines() {
	for p.peek().Kind == lexer.Newline {
		p.advance()
	}
}

func (p *parser) expect(k lexer.Kind, what string) (lexer.Token, error) {
	if p.peek().Kind != k {
		return lexer.Token{}, fmt.Errorf("line %d: expected %s, got %q", p.peek().Line, what, p.peek().Literal)
	}
	return p.advance(), nil
}

// parseFile parses the single top-level construct Step 1 understands:
// exactly one `func main(): int {...}` declaration. General top-level
// statements (weave_spec.md §17's gotype/gofunc/object declarations)
// arrive once Step 5 (functions) and Step 6 (objects) exist to produce
// them — see ast.File's doc comment.
func (p *parser) parseFile() (*ast.File, error) {
	p.skipNewlines()
	fn, err := p.parseFuncDecl()
	if err != nil {
		return nil, err
	}
	p.skipNewlines()
	if p.peek().Kind != lexer.EOF {
		return nil, fmt.Errorf("line %d: unexpected token %q after func main (top-level statements are not yet supported)", p.peek().Line, p.peek().Literal)
	}
	return &ast.File{Main: fn}, nil
}

func (p *parser) parseFuncDecl() (*ast.FuncDecl, error) {
	kw, err := p.expect(lexer.KwFunc, "'func'")
	if err != nil {
		return nil, err
	}
	name, err := p.expect(lexer.Ident, "function name")
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.LParen, "'('"); err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.RParen, "')'"); err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.Colon, "':'"); err != nil {
		return nil, err
	}
	retType, err := p.expect(lexer.Ident, "return type")
	if err != nil {
		return nil, err
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &ast.FuncDecl{Name: name.Literal, ReturnType: retType.Literal, Body: body, Line: kw.Line}, nil
}

func (p *parser) parseBlock() ([]ast.Stmt, error) {
	if _, err := p.expect(lexer.LBrace, "'{'"); err != nil {
		return nil, err
	}
	p.skipNewlines()
	var stmts []ast.Stmt
	for p.peek().Kind != lexer.RBrace {
		if p.peek().Kind == lexer.EOF {
			return nil, fmt.Errorf("line %d: unterminated block, expected '}'", p.peek().Line)
		}
		stmt, err := p.parseStmt()
		if err != nil {
			return nil, err
		}
		stmts = append(stmts, stmt)
		p.skipNewlines()
	}
	p.advance() // consume '}'
	return stmts, nil
}

func (p *parser) parseStmt() (ast.Stmt, error) {
	switch p.peek().Kind {
	case lexer.KwReturn:
		return p.parseReturnStmt()
	default:
		return p.parseExprStmt()
	}
}

func (p *parser) parseReturnStmt() (ast.Stmt, error) {
	kw := p.advance() // 'return'
	if isStmtEnd(p.peek()) {
		return &ast.ReturnStmt{Line: kw.Line}, nil
	}
	val, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	return &ast.ReturnStmt{Value: val, Line: kw.Line}, nil
}

func (p *parser) parseExprStmt() (ast.Stmt, error) {
	x, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	return &ast.ExprStmt{X: x, Line: exprLine(x)}, nil
}

func isStmtEnd(t lexer.Token) bool {
	return t.Kind == lexer.Newline || t.Kind == lexer.RBrace || t.Kind == lexer.EOF
}

// parseExpr is the expression entry point. It currently only builds a
// primary expression optionally followed by calls — the full operator
// precedence table (weave_spec.md §8) arrives in Step 3.
func (p *parser) parseExpr() (ast.Expr, error) {
	return p.parseCallOrPrimary()
}

func (p *parser) parseCallOrPrimary() (ast.Expr, error) {
	x, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	for p.peek().Kind == lexer.LParen {
		x, err = p.parseCallArgs(x)
		if err != nil {
			return nil, err
		}
	}
	return x, nil
}

func (p *parser) parseCallArgs(callee ast.Expr) (ast.Expr, error) {
	lp := p.advance() // '('
	var args []ast.Expr
	for p.peek().Kind != lexer.RParen {
		arg, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		args = append(args, arg)
		if p.peek().Kind == lexer.Comma {
			p.advance()
			continue
		}
		break
	}
	if _, err := p.expect(lexer.RParen, "')'"); err != nil {
		return nil, err
	}
	return &ast.CallExpr{Callee: callee, Args: args, Line: lp.Line}, nil
}

func (p *parser) parsePrimary() (ast.Expr, error) {
	t := p.peek()
	switch t.Kind {
	case lexer.Ident:
		p.advance()
		return &ast.Ident{Name: t.Literal, Line: t.Line}, nil
	case lexer.Number:
		p.advance()
		v, err := strconv.ParseFloat(t.Literal, 64)
		if err != nil {
			return nil, fmt.Errorf("line %d: invalid number literal %q", t.Line, t.Literal)
		}
		return &ast.NumberLit{Value: v, Line: t.Line}, nil
	case lexer.String:
		p.advance()
		return &ast.StringLit{Value: t.Literal, Line: t.Line}, nil
	case lexer.LParen:
		p.advance()
		x, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(lexer.RParen, "')'"); err != nil {
			return nil, err
		}
		return x, nil
	}
	return nil, fmt.Errorf("line %d: unexpected token %q in expression", t.Line, t.Literal)
}

func exprLine(x ast.Expr) int {
	switch x := x.(type) {
	case *ast.Ident:
		return x.Line
	case *ast.NumberLit:
		return x.Line
	case *ast.StringLit:
		return x.Line
	case *ast.CallExpr:
		return x.Line
	default:
		return 0
	}
}
