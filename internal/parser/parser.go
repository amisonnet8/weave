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

// parseFile parses a Weave source file: an interleaving of ordinary
// top-level statements (weave_spec.md §17's gotype/gofunc/package(...)
// declarations, prototype object literals) and exactly one `func
// main(): int {...}` declaration, in any order. There is no dedicated
// `import` syntax — a cross-package reference is just an ordinary
// `name = package("<path>")` top-level assignment (weave_spec.md
// §17.2), parsed here as a plain ast.AssignStmt like any other; the
// pattern is recognized and fully resolved by internal/modloader, which
// runs before sema/codegen ever see a *File (see modloader's package
// doc comment). See ast.File's doc comment for why top-level statements
// need no dedicated codegen/sema treatment beyond being processed
// against the same scope as main's body.
func (p *parser) parseFile() (*ast.File, error) {
	p.skipNewlines()

	var topLevel []ast.Stmt
	var main *ast.FuncDecl
	for p.peek().Kind != lexer.EOF {
		if p.peek().Kind == lexer.KwFunc {
			fn, err := p.parseFuncDecl()
			if err != nil {
				return nil, err
			}
			if main != nil {
				return nil, fmt.Errorf("line %d: only one `func main` is allowed per file", fn.Line)
			}
			main = fn
		} else {
			stmt, err := p.parseSimpleStmt()
			if err != nil {
				return nil, err
			}
			topLevel = append(topLevel, stmt)
		}
		p.skipNewlines()
	}
	// A single parsed file need not contain `func main` at all — a
	// package member file (weave_spec.md §17.1) legitimately has none.
	// Requiring at least one `func main` somewhere in the whole package
	// is internal/modloader's job, once every file in the package has
	// been merged (see modloader.Load).
	return &ast.File{TopLevel: topLevel, Main: main}, nil
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
	case lexer.KwIf:
		return p.parseIfStmt()
	case lexer.KwWhile:
		return p.parseWhileStmt()
	case lexer.KwFor:
		return p.parseForStmt()
	case lexer.KwBreak:
		return &ast.BreakStmt{Line: p.advance().Line}, nil
	case lexer.KwContinue:
		return &ast.ContinueStmt{Line: p.advance().Line}, nil
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

// parseIfStmt parses `if cond {...} elif cond2 {...} else {...}`
// (weave_spec.md §7). `elif`/`else` must appear on the same line as the
// preceding block's closing `}` — like Go's own `if {} else {}` — since
// a newline there would already have ended the statement (§1: newline
// is a statement terminator) before `elif`/`else` could be seen as a
// continuation of the same if-statement.
func (p *parser) parseIfStmt() (ast.Stmt, error) {
	kw := p.advance() // 'if'
	cond, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	stmt := &ast.IfStmt{Clauses: []ast.IfClause{{Cond: cond, Body: body}}, Line: kw.Line}

	for p.peek().Kind == lexer.KwElif {
		p.advance()
		c, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		b, err := p.parseBlock()
		if err != nil {
			return nil, err
		}
		stmt.Clauses = append(stmt.Clauses, ast.IfClause{Cond: c, Body: b})
	}
	if p.peek().Kind == lexer.KwElse {
		p.advance()
		b, err := p.parseBlock()
		if err != nil {
			return nil, err
		}
		stmt.Else = b
	}
	return stmt, nil
}

func (p *parser) parseWhileStmt() (ast.Stmt, error) {
	kw := p.advance() // 'while'
	cond, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &ast.WhileStmt{Cond: cond, Body: body, Line: kw.Line}, nil
}

// parseForStmt parses `for k, v in obj {...}` (weave_spec.md §7).
func (p *parser) parseForStmt() (ast.Stmt, error) {
	kw := p.advance() // 'for'
	key, err := p.expect(lexer.Ident, "loop key variable")
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.Comma, "','"); err != nil {
		return nil, err
	}
	val, err := p.expect(lexer.Ident, "loop value variable")
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.KwIn, "'in'"); err != nil {
		return nil, err
	}
	obj, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &ast.ForStmt{Key: key.Literal, Value: val.Literal, Obj: obj, Body: body, Line: kw.Line}, nil
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
		val, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		switch target := x.(type) {
		case *ast.Ident:
			return &ast.AssignStmt{Name: target.Name, Value: val, Line: eq.Line}, nil
		case *ast.PropExpr:
			return &ast.PropAssignStmt{Obj: target.Obj, Prop: target.Prop, Value: val, Line: eq.Line}, nil
		default:
			return nil, fmt.Errorf("line %d: left-hand side of `=` must be an identifier or a property access", eq.Line)
		}
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

// parseCallOrPrimary handles weave_spec.md §8's highest-precedence
// level: a primary expression followed by any mix of calls (`(args)`)
// and property accesses (`.name`), left to right — e.g. `obj.method(a).field`.
func (p *parser) parseCallOrPrimary() (ast.Expr, error) {
	x, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	for {
		switch p.peek().Kind {
		case lexer.LParen:
			x, err = p.parseCallArgs(x)
		case lexer.Dot:
			x, err = p.parsePropAccess(x)
		default:
			return x, nil
		}
		if err != nil {
			return nil, err
		}
	}
}

func (p *parser) parsePropAccess(obj ast.Expr) (ast.Expr, error) {
	dot := p.advance() // '.'
	name, err := p.expect(lexer.Ident, "property name")
	if err != nil {
		return nil, err
	}
	return &ast.PropExpr{Obj: obj, Prop: name.Literal, Line: dot.Line}, nil
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
	case lexer.KwFn:
		return p.parseFuncLit()
	case lexer.LBrace:
		return p.parseObjectLit()
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

// parseObjectLit parses `{ }` or `{ x: 1, y: 2 }` (weave_spec.md §3,
// §4.1). Newlines are allowed around fields and after the trailing
// comma, so a literal can be spread across multiple lines like a block.
func (p *parser) parseObjectLit() (ast.Expr, error) {
	lb := p.advance() // '{'
	p.skipNewlines()
	var fields []ast.ObjectField
	for p.peek().Kind != lexer.RBrace {
		key, err := p.expect(lexer.Ident, "property name")
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(lexer.Colon, "':'"); err != nil {
			return nil, err
		}
		p.skipNewlines()
		val, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		fields = append(fields, ast.ObjectField{Name: key.Literal, Value: val})
		p.skipNewlines()
		if p.peek().Kind == lexer.Comma {
			p.advance()
			p.skipNewlines()
			continue
		}
		break
	}
	if _, err := p.expect(lexer.RBrace, "'}'"); err != nil {
		return nil, err
	}
	return &ast.ObjectLit{Fields: fields, Line: lb.Line}, nil
}

// parseFuncLit parses `fn(params) body` (weave_spec.md §5), where body
// is either a `{...}` block or — for the chained-currying sugar
// `fn(a) fn(b) {...}` — another `fn(...)` literal directly, with no
// braces around the outer body at all. Either way, and regardless of
// whether the parameter list itself has one name or several
// (`fn(a, b) {...}` sugar, also §5), the result is always a chain of
// single-Param FuncLits: every level but the innermost wraps the next
// in a single implicit `return` (see ast.FuncLit's doc comment).
func (p *parser) parseFuncLit() (ast.Expr, error) {
	kw := p.advance() // 'fn'
	if _, err := p.expect(lexer.LParen, "'('"); err != nil {
		return nil, err
	}
	var params []string
	for p.peek().Kind != lexer.RParen {
		name, err := p.expect(lexer.Ident, "parameter name")
		if err != nil {
			return nil, err
		}
		params = append(params, name.Literal)
		if p.peek().Kind == lexer.Comma {
			p.advance()
			continue
		}
		break
	}
	if _, err := p.expect(lexer.RParen, "')'"); err != nil {
		return nil, err
	}
	if len(params) == 0 {
		return nil, fmt.Errorf("line %d: fn(...) requires at least one parameter (weave_spec.md §5)", kw.Line)
	}

	var body []ast.Stmt
	if p.peek().Kind == lexer.KwFn {
		inner, err := p.parseFuncLit()
		if err != nil {
			return nil, err
		}
		body = []ast.Stmt{&ast.ReturnStmt{Value: inner, Line: kw.Line}}
	} else {
		b, err := p.parseBlock()
		if err != nil {
			return nil, err
		}
		body = b
	}

	// Fold multiple params into nested single-Param literals, innermost
	// first: fn(a, b) {body} becomes fn(a) { return fn(b) {body} }.
	lit := &ast.FuncLit{Param: params[len(params)-1], Body: body, Line: kw.Line}
	for i := len(params) - 2; i >= 0; i-- {
		lit = &ast.FuncLit{
			Param: params[i],
			Body:  []ast.Stmt{&ast.ReturnStmt{Value: lit, Line: kw.Line}},
			Line:  kw.Line,
		}
	}
	return lit, nil
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
	case *ast.FuncLit:
		return x.Line
	case *ast.ObjectLit:
		return x.Line
	case *ast.PropExpr:
		return x.Line
	default:
		return 0
	}
}
