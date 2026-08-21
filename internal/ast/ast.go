package ast

// File is the root of a parsed Weave source file.
//
// Step 1 scope: a File holds only the entry point (weave_spec.md §12).
// Weave's top level also allows ordinary statements outside of func main
// (e.g. §17's gotype/gofunc/object declarations), but nothing before
// Step 5 (functions) and Step 6 (objects) can produce one, so File is
// deliberately narrow for now — it grows into a general top-level
// statement list once those constructs exist.
type File struct {
	Main *FuncDecl
}

// FuncDecl is the `func main(): int { ... }` entry point (weave_spec.md
// §12). It is the only construct that uses the `func` keyword; every
// other Weave function is a `fn(a) {...}` expression (§5).
type FuncDecl struct {
	Name       string
	ReturnType string
	Body       []Stmt
	Line       int
}

// Stmt is a Weave statement.
type Stmt interface {
	stmtNode()
}

// ExprStmt is an expression evaluated for its side effect (e.g. a bare
// call like `print(...)`).
type ExprStmt struct {
	X    Expr
	Line int
}

// ReturnStmt is `return` or `return <expr>` (weave_spec.md §7 control
// flow keywords; §12 for main's `return <exit code>`).
type ReturnStmt struct {
	Value Expr // nil for a bare `return`
	Line  int
}

// AssignStmt is `name = value` (weave_spec.md §2, §4.1). Weave has no
// declaration keyword: the first assignment to a name introduces it in
// the current scope (§2), and later assignments update it.
type AssignStmt struct {
	Name  string
	Value Expr
	Line  int
}

// IfClause is one `if`/`elif` condition-and-body pair.
type IfClause struct {
	Cond Expr
	Body []Stmt
}

// IfStmt is `if cond {...} elif cond2 {...} else {...}` (weave_spec.md
// §7). Clauses always has at least one element: Clauses[0] is the `if`,
// any further elements are `elif`s in source order. Else is nil when
// there is no `else` block.
type IfStmt struct {
	Clauses []IfClause
	Else    []Stmt
	Line    int
}

// WhileStmt is `while cond {...}` (weave_spec.md §7).
type WhileStmt struct {
	Cond Expr
	Body []Stmt
	Line int
}

// BreakStmt and ContinueStmt are `break`/`continue` (weave_spec.md §7):
// valid inside a `while` (and, from Step 8, a `for`), affecting only the
// innermost enclosing loop.
type BreakStmt struct{ Line int }
type ContinueStmt struct{ Line int }

func (*ExprStmt) stmtNode()     {}
func (*ReturnStmt) stmtNode()   {}
func (*AssignStmt) stmtNode()   {}
func (*IfStmt) stmtNode()       {}
func (*WhileStmt) stmtNode()    {}
func (*BreakStmt) stmtNode()    {}
func (*ContinueStmt) stmtNode() {}

// Expr is a Weave expression.
type Expr interface {
	exprNode()
}

// Ident is a bare identifier reference (a variable, or a callee name).
type Ident struct {
	Name string
	Line int
}

// NumberLit is a numeric literal. Weave does not distinguish integers
// from floats (weave_spec.md §2): both literal shapes fold into the same
// float64-valued node.
type NumberLit struct {
	Value float64
	Line  int
}

// StringLit is a string literal (weave_spec.md §3). Value holds the
// decoded string content (lexer.Token.Literal already strips the
// surrounding quotes).
type StringLit struct {
	Value string
	Line  int
}

// BoolLit is `true` or `false`.
type BoolLit struct {
	Value bool
	Line  int
}

// NilLit is the literal `nil` — Weave's single "value absence" value
// (weave_spec.md §2).
type NilLit struct {
	Line int
}

// CallExpr is a function call `Callee(Args...)`. Through Step 2, Callee
// is always an *Ident naming a builtin (print); general callees
// (currying, method dispatch) arrive in later steps.
type CallExpr struct {
	Callee Expr
	Args   []Expr
	Line   int
}

// BinaryExpr is a binary operator expression (weave_spec.md §8): Op is
// one of "+" "-" "*" "/" "%" "==" "!=" "<" "<=" ">" ">=" "&&" "||".
type BinaryExpr struct {
	Op   string
	X, Y Expr
	Line int
}

// UnaryExpr is a prefix unary operator expression (weave_spec.md §8):
// Op is "!" or "-".
type UnaryExpr struct {
	Op   string
	X    Expr
	Line int
}

func (*Ident) exprNode()      {}
func (*NumberLit) exprNode()  {}
func (*StringLit) exprNode()  {}
func (*BoolLit) exprNode()    {}
func (*NilLit) exprNode()     {}
func (*CallExpr) exprNode()   {}
func (*BinaryExpr) exprNode() {}
func (*UnaryExpr) exprNode()  {}
