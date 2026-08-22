package codegen

import (
	"fmt"

	"github.com/amisonnet8/weave/internal/ast"
)

// genForStmt lowers `for k, v in obj {...}` (weave_spec.md §7) into an
// index-counted LOOP, structurally similar to genWhileStmt. AMIVM has
// no native "range" instruction, and — per the same reasoning as every
// other object operation (weavert/object.go's package doc comment) —
// obj is `^any`, so enumerating it has to go through weavert
// (ObjKeys/weavert.Len/weavert.KeyAt) rather than any native construct.
//
// The index is advanced at the very TOP of the loop body, before the
// bounds check, rather than at the bottom (idx starts at -1 to
// compensate) — unlike a plain while-loop, a for-in still has mandatory
// per-iteration work (advancing idx) that must happen even when the
// body `continue`s. Putting that work first, ahead of the check, means
// a native CONTINUE (which re-enters at LOOP's own top) naturally hits
// it every time, with no separate continue-label/GOTO bookkeeping
// needed at all — genWhileStmt's condition-recheck-at-top trick and
// this one are the same idea, just applied to "recheck vs. advance".
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
	fmt.Fprintf(&fg.body, "\tSET\t%s\t-1.0\n", idx)

	keyTok := fg.declare(s.Key, "^any")
	valTok := fg.declare(s.Value, "^any")

	fg.body.WriteString("\tLOOP\n")
	next := fg.newTemp("^any")
	fmt.Fprintf(&fg.body, "\tCALL\t%s\t:\t?weavert.Add\t%s\t1.0\n", next, idx)
	fmt.Fprintf(&fg.body, "\tSET\t%s\t%s\n", idx, next)

	lt := fg.newTemp("^any")
	fmt.Fprintf(&fg.body, "\tCALL\t%s\t:\t?weavert.Lt\t%s\t%s\n", lt, idx, count)
	checked := fg.newTemp("^bool")
	fmt.Fprintf(&fg.body, "\tCALL\t%s\t:\t?weavert.CheckBool\t%s\n", checked, lt)
	genBreakUnless(fg, checked)

	fmt.Fprintf(&fg.body, "\tCALL\t%%%s\t:\t?weavert.KeyAt\t%s\t%s\n", keyTok, keys, idx)
	fmt.Fprintf(&fg.body, "\tCALL\t%%%s\t:\t?weavert.ObjGet\t%s\t%%%s\n", valTok, objVal, keyTok)

	if err := genBlock(fg, s.Body); err != nil {
		return err
	}
	fg.body.WriteString("\tENDLOOP\n")
	return nil
}
