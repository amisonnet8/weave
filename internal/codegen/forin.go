package codegen

import (
	"fmt"

	"github.com/amisonnet8/weave/internal/ast"
)

// genForStmt lowers `for k, v in obj {...}` (weave_spec.md §7) into an
// index-counted loop, structurally similar to genWhileStmt. AMIVM has
// no native "range" instruction, and — per the same reasoning as every
// other object operation (weavert/object.go's package doc comment) —
// obj is `^any`, so enumerating it has to go through weavert
// (ObjKeys/weavert.Len/weavert.KeyAt) rather than any native construct.
//
// continue must still advance the index before looping back, unlike
// genWhileStmt's continue (which just re-checks the condition): a
// continueLabel positioned right before the increment step, rather than
// at the loop start, is what makes that happen — otherwise `continue`
// would re-process the same index forever.
func genForStmt(fg *funcGen, s *ast.ForStmt) error {
	objVal, err := genExpr(fg, s.Obj)
	if err != nil {
		return err
	}
	keys := fg.newTemp("^any")
	fmt.Fprintf(&fg.body, "\tCALL\t%s\t:\t?weavert.ObjKeys\t%s\n", keys, objVal)
	count := fg.newTemp("^any")
	fmt.Fprintf(&fg.body, "\tCALL\t%s\t:\t?weavert.Len\t%s\n", count, keys)
	idx := fg.newTemp("^any")
	fmt.Fprintf(&fg.body, "\tSET\t%s\t0.0\n", idx)

	keyTok := fg.declare(s.Key, "^any")
	valTok := fg.declare(s.Value, "^any")

	startLabel := fg.newLabel("for_start")
	bodyLabel := fg.newLabel("for_body")
	continueLabel := fg.newLabel("for_continue")
	endLabel := fg.newLabel("for_end")

	fmt.Fprintf(&fg.body, "\tLABEL\t#%s\n", startLabel)
	lt := fg.newTemp("^any")
	fmt.Fprintf(&fg.body, "\tCALL\t%s\t:\t?weavert.Lt\t%s\t%s\n", lt, idx, count)
	cond := fg.newTemp("^bool")
	fmt.Fprintf(&fg.body, "\tCALL\t%s\t:\t?weavert.CheckBool\t%s\n", cond, lt)
	fmt.Fprintf(&fg.body, "\tIF\t%s\t#%s\n", cond, bodyLabel)
	fmt.Fprintf(&fg.body, "\tGOTO\t#%s\n", endLabel)
	fmt.Fprintf(&fg.body, "\tLABEL\t#%s\n", bodyLabel)

	fmt.Fprintf(&fg.body, "\tCALL\t%%%s\t:\t?weavert.KeyAt\t%s\t%s\n", keyTok, keys, idx)
	fmt.Fprintf(&fg.body, "\tCALL\t%%%s\t:\t?weavert.ObjGet\t%s\t%%%s\n", valTok, objVal, keyTok)

	fg.breakStack = append(fg.breakStack, endLabel)
	fg.continueStack = append(fg.continueStack, continueLabel)
	err = genBlock(fg, s.Body)
	fg.breakStack = fg.breakStack[:len(fg.breakStack)-1]
	fg.continueStack = fg.continueStack[:len(fg.continueStack)-1]
	if err != nil {
		return err
	}
	// continueLabel must be reachable by an explicit GOTO regardless of
	// whether the body actually uses `continue` — Go only counts a
	// label as "used" via a goto that names it, not by ordinary
	// fallthrough, and a body with no `continue` would otherwise leave
	// it with none (caught by amivm's go/types check while running
	// examples/builtins.weave through the full pipeline — see
	// CLAUDE.md's Step 8 "確定した設計判断").
	fmt.Fprintf(&fg.body, "\tGOTO\t#%s\n", continueLabel)
	fmt.Fprintf(&fg.body, "\tLABEL\t#%s\n", continueLabel)
	next := fg.newTemp("^any")
	fmt.Fprintf(&fg.body, "\tCALL\t%s\t:\t?weavert.Add\t%s\t1.0\n", next, idx)
	fmt.Fprintf(&fg.body, "\tSET\t%s\t%s\n", idx, next)
	fmt.Fprintf(&fg.body, "\tGOTO\t#%s\n", startLabel)
	fmt.Fprintf(&fg.body, "\tLABEL\t#%s\n", endLabel)
	return nil
}
