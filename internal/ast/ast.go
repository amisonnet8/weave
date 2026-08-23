package ast

// File is the root of a parsed Weave source file.
//
// TopLevel holds ordinary statements written outside `main`'s own
// declaration (weave_spec.md §15.3, §17: gotype/gofunc declarations,
// prototype object definitions, ...), in source order, regardless of
// where `main = fn(args) {...}` itself appears among them. Main is
// always exactly one such declaration — parser.go's parseFile
// recognizes it (a top-level `*ast.AssignStmt` named "main" whose value
// is directly a `*ast.FuncLit`), extracts it out of TopLevel into this
// dedicated field, and rejects a second one appearing in the same file.
//
// codegen.Generate and sema.Check both simply process TopLevel and
// Main.Body in order against the *same* scope/funcGen — Weave has no
// other top-level function to isolate TopLevel's bindings from (every
// other Weave "function" is a `fn(a) {...}` closure, independently
// compiled and only ever reachable through a value, never by name at
// the top level), so there is currently no need for AMIVM's GVAR
// (global variable) mechanism: TopLevel's statements behave exactly as
// if they were the first statements written inside main() itself (see
// CLAUDE.md's 後半 Step 5 "確定した設計判断" for the full reasoning,
// including what would have to change if Weave ever grew a second
// top-level function).
type File struct {
	TopLevel []Stmt
	Main     *FuncDecl
}

// FuncDecl is Weave's entry point, written as `main = fn(args) { ... }`
// (weave_spec.md §12) — an ordinary-looking top-level assignment whose
// RHS is a function literal, but recognized and extracted specially
// because the name is exactly "main" (mirroring how `gotype`/`gofunc`/
// `package(...)` are likewise ordinary-shaped assignments that carry
// special, compile-time-only meaning — see internal/sema's goAssetReservedName
// and internal/modloader's package doc comment for the same pattern
// applied elsewhere). Param is the closure's own parameter name (not
// fixed to any particular spelling — weave_spec.md §11's `at`-indexable
// list of command-line arguments, including the program's own name at
// position 0, is bound to it at the call site — see codegen.go's
// Generate). There used to be a dedicated `func` keyword for this
// declaration; it was retired once `main` became just another
// (specially recognized) assignment, leaving `fn(a) {...}` the only
// function-literal syntax in the language (§5).
type FuncDecl struct {
	Name  string
	Param string
	Body  []Stmt
	Line  int
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
//
// A top-level `name = package("<path>")` is a special case internal/
// modloader recognizes and fully resolves away before sema.Check/
// codegen.Generate ever run (weave_spec.md §17.2) — there is no
// dedicated AST node for it, exactly like gotype/gofunc (see
// modloader's package doc comment and CLAUDE.md's 確定した設計判断 on
// why no new syntax is needed).
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

// PropAssignStmt is `obj.name = value` (weave_spec.md §4.1). A property
// write always applies to Obj itself, never a prototype (§4.2) — that
// distinction only matters once Step 7 adds prototype-chain reads.
type PropAssignStmt struct {
	Obj   Expr
	Prop  string
	Value Expr
	Line  int
}

// ForStmt is `for k, v in obj {...}` (weave_spec.md §7): enumerates
// obj's own properties — never a prototype's, matching `has`/`remove`'s
// own-only scope (§7's own comment: "プロトタイプは辿らない").
type ForStmt struct {
	Key   string
	Value string
	Obj   Expr
	Body  []Stmt
	Line  int
}

func (*ExprStmt) stmtNode()       {}
func (*ReturnStmt) stmtNode()     {}
func (*AssignStmt) stmtNode()     {}
func (*IfStmt) stmtNode()         {}
func (*WhileStmt) stmtNode()      {}
func (*BreakStmt) stmtNode()      {}
func (*ContinueStmt) stmtNode()   {}
func (*ForStmt) stmtNode()        {}
func (*PropAssignStmt) stmtNode() {}

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

// FuncLit is a function literal `fn(param) { body }` (weave_spec.md
// §5). Every Weave function takes exactly one argument — Param is
// always a single name. The multi-argument sugar `fn(a, b) {...}` and
// the chained-literal sugar `fn(a) fn(b) {...}` are both folded into
// nested single-Param FuncLits by the parser (see parser.go's
// parseFuncLit), so this is the only shape codegen/sema ever see:
// `fn(a, b) { body }` arrives as
// `FuncLit{Param: "a", Body: [ReturnStmt{Value: FuncLit{Param: "b", Body: body}}]}`.
type FuncLit struct {
	Param string
	Body  []Stmt
	Line  int
}

// ObjectField is one `key: value` pair in an object literal.
type ObjectField struct {
	Name  string
	Value Expr
}

// ObjectLit is `{ }` or `{ x: 1, y: 2 }` (weave_spec.md §3, §4.1). A
// prototype-carrying literal (`{ __proto__: base, x: 1 }`, §4.2)
// arrives in Step 7 — through Step 6 every field is an ordinary
// property.
type ObjectLit struct {
	Fields []ObjectField
	Line   int
}

// PropExpr is a property read `obj.name` (weave_spec.md §4.1).
type PropExpr struct {
	Obj  Expr
	Prop string
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
func (*FuncLit) exprNode()    {}
func (*ObjectLit) exprNode()  {}
func (*PropExpr) exprNode()   {}
