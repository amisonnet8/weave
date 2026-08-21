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
		return p.parseSimpleStmt()
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

// parseSimpleStmt parses a bare expression statement (e.g. a call like
// `print(...)`) or, if `=` follows, an assignment (weave_spec.md §2:
// there is no declaration keyword — `name = value` both introduces and
// updates a name).
func (p *parser) parseSimpleStmt() (ast.Stmt, error) {
	x, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if p.peek().Kind == lexer.Assign {
		eq := p.advance()
		name, ok := x.(*ast.Ident)
		if !ok {
			return nil, fmt.Errorf("line %d: left-hand side of `=` must be an identifier", eq.Line)
		}
		val, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		return &ast.AssignStmt{Name: name.Name, Value: val, Line: eq.Line}, nil
	}
	return &ast.ExprStmt{X: x, Line: exprLine(x)}, nil
}

func isStmtEnd(t lexer.Token) bool {
	return t.Kind == lexer.Newline || t.Kind == lexer.RBrace || t.Kind == lexer.EOF
}

// parseExpr is the expression entry point: precedence-climbing over
// weave_spec.md §8's table, lowest precedence first. Each level parses
// its higher-precedence operand via the next function down; parseUnary
// is the last stop before postfix calls/primaries.
//
//	parseOr            ||                     (lowest)
//	parseAnd           &&
//	parseEquality      == !=
//	parseComparison    < <= > >=
//	parseAdditive      + -
//	parseMultiplicative * / %
//	parseUnary         unary ! -
//	parseCallOrPrimary ( ) . call              (highest)
func (p *parser) parseExpr() (ast.Expr, error) {
	return p.parseOr()
}

func (p *parser) parseOr() (ast.Expr, error) {
	x, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.peek().Kind == lexer.OrOr {
		op := p.advance()
		y, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		x = &ast.BinaryExpr{Op: "||", X: x, Y: y, Line: op.Line}
	}
	return x, nil
}

func (p *parser) parseAnd() (ast.Expr, error) {
	x, err := p.parseEquality()
	if err != nil {
		return nil, err
	}
	for p.peek().Kind == lexer.AndAnd {
		op := p.advance()
		y, err := p.parseEquality()
		if err != nil {
			return nil, err
		}
		x = &ast.BinaryExpr{Op: "&&", X: x, Y: y, Line: op.Line}
	}
	return x, nil
}

func (p *parser) parseEquality() (ast.Expr, error) {
	x, err := p.parseComparison()
	if err != nil {
		return nil, err
	}
	for p.peek().Kind == lexer.Eq || p.peek().Kind == lexer.Neq {
		op := p.advance()
		y, err := p.parseComparison()
		if err != nil {
			return nil, err
		}
		x = &ast.BinaryExpr{Op: opText(op.Kind), X: x, Y: y, Line: op.Line}
	}
	return x, nil
}

func (p *parser) parseComparison() (ast.Expr, error) {
	x, err := p.parseAdditive()
	if err != nil {
		return nil, err
	}
	for isComparisonOp(p.peek().Kind) {
		op := p.advance()
		y, err := p.parseAdditive()
		if err != nil {
			return nil, err
		}
		x = &ast.BinaryExpr{Op: opText(op.Kind), X: x, Y: y, Line: op.Line}
	}
	return x, nil
}

func (p *parser) parseAdditive() (ast.Expr, error) {
	x, err := p.parseMultiplicative()
	if err != nil {
		return nil, err
	}
	for p.peek().Kind == lexer.Plus || p.peek().Kind == lexer.Minus {
		op := p.advance()
		y, err := p.parseMultiplicative()
		if err != nil {
			return nil, err
		}
		x = &ast.BinaryExpr{Op: opText(op.Kind), X: x, Y: y, Line: op.Line}
	}
	return x, nil
}

func (p *parser) parseMultiplicative() (ast.Expr, error) {
	x, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for isMulOp(p.peek().Kind) {
		op := p.advance()
		y, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		x = &ast.BinaryExpr{Op: opText(op.Kind), X: x, Y: y, Line: op.Line}
	}
	return x, nil
}

func (p *parser) parseUnary() (ast.Expr, error) {
	if p.peek().Kind == lexer.Not || p.peek().Kind == lexer.Minus {
		op := p.advance()
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &ast.UnaryExpr{Op: opText(op.Kind), X: x, Line: op.Line}, nil
	}
	return p.parseCallOrPrimary()
}

func isComparisonOp(k lexer.Kind) bool {
	return k == lexer.Lt || k == lexer.Lte || k == lexer.Gt || k == lexer.Gte
}

func isMulOp(k lexer.Kind) bool {
	return k == lexer.Star || k == lexer.Slash || k == lexer.Percent
}

func opText(k lexer.Kind) string {
	switch k {
	case lexer.Plus:
		return "+"
	case lexer.Minus:
		return "-"
	case lexer.Star:
		return "*"
	case lexer.Slash:
		return "/"
	case lexer.Percent:
		return "%"
	case lexer.Eq:
		return "=="
	case lexer.Neq:
		return "!="
	case lexer.Lt:
		return "<"
	case lexer.Lte:
		return "<="
	case lexer.Gt:
		return ">"
	case lexer.Gte:
		return ">="
	case lexer.AndAnd:
		return "&&"
	case lexer.OrOr:
		return "||"
	case lexer.Not:
		return "!"
	default:
		return "?"
	}
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
	case lexer.KwTrue:
		p.advance()
		return &ast.BoolLit{Value: true, Line: t.Line}, nil
	case lexer.KwFalse:
		p.advance()
		return &ast.BoolLit{Value: false, Line: t.Line}, nil
	case lexer.KwNil:
		p.advance()
		return &ast.NilLit{Line: t.Line}, nil
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
	case *ast.BoolLit:
		return x.Line
	case *ast.NilLit:
		return x.Line
	case *ast.CallExpr:
		return x.Line
	case *ast.BinaryExpr:
		return x.Line
	case *ast.UnaryExpr:
		return x.Line
	default:
		return 0
	}
}
