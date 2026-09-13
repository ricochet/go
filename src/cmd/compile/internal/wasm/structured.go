// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package wasm

import (
	"cmp"
	"internal/buildcfg"
	"slices"

	"cmd/compile/internal/base"
	"cmd/compile/internal/ir"
	"cmd/compile/internal/ssa"
	"cmd/compile/internal/ssa/block"
	"cmd/compile/internal/ssagen"
	"cmd/internal/obj"
	"cmd/internal/obj/wasm"
)

/*
   Structured control flow (GOEXPERIMENT=wasmstackful)
   ---------------------------------------------------

   Normally every block of a function is a resume point: the function body is
   a loop around a br_table on PC_B, and every jump between blocks sets PC_B
   and branches back to the br_table (see the comment at the top of ssa.go).
   That is required as long as a goroutine switch can resume a function in
   the middle of its body.

   With GOEXPERIMENT=wasmstackful, goroutines keep their wasm stacks, so
   functions are only ever entered at the top. The exceptions are functions
   that panic recovery may resume in the middle (those containing deferreturn
   or (*_panic).start calls), which keep the dispatch loop.

   For all other functions with a reducible control flow graph, blocks are
   laid out and nested following "Beyond Relooper" (Norman Ramsey, 2022):
   blocks are emitted in dominator-tree order; a loop header opens a wasm
   loop that encloses everything it dominates; and a block that is the
   target of a forward branch (other than a fall-through from the block
   emitted just before it) is preceded by the end of a wasm block that was
   opened by its immediate dominator. Branches then become br/br_if to those
   labels.
*/

// stackful reports whether GOEXPERIMENT=wasmstackful applies.
func stackful() bool {
	return (buildcfg.GOOS == "wasip1" || buildcfg.GOOS == "wasip3") && buildcfg.Experiment.WasmCoopThreads && buildcfg.Experiment.WasmStackful
}

type layoutOpKind uint8

const (
	layoutOpenLoop  layoutOpKind = iota // open a loop headed by b
	layoutOpenBlock                     // open a block that ends right before b
	layoutClose                         // end the innermost open loop or block
	layoutCode                          // b's values and branches
)

type layoutOp struct {
	kind layoutOpKind
	b    *ssa.Block
}

// structuredFunc is the structured layout of a function's blocks.
type structuredFunc struct {
	last *ssa.Block

	// Indexed by block ID.
	pre        [][]layoutOp // layout ops to emit before the block's code
	rpo        []int32      // reverse postorder number
	fallthru   []*ssa.Block // the successor emitted right after this block, or nil
	loopLabel  []*obj.Prog  // the loop headed by this block
	blockLabel []*obj.Prog  // the block ending right before this block

	post []layoutOp // layout ops to emit after the last block
}

func ssaPrepareBlocks(s *ssagen.State, f *ssa.Func) {
	s.ArchData = nil
	if !stackful() {
		return
	}
	if sf := planStructured(s, f); sf != nil {
		s.ArchData = sf
	}
}

func structured(s *ssagen.State) *structuredFunc {
	sf, _ := s.ArchData.(*structuredFunc)
	return sf
}

// planStructured computes a structured layout for f and reorders f.Blocks
// accordingly. It returns nil if f must use the dispatch loop instead.
func planStructured(s *ssagen.State, f *ssa.Func) *structuredFunc {
	if s.FuncInfo().OpenCodedDeferInfo != nil {
		return nil
	}
	for _, b := range f.Blocks {
		switch b.Kind {
		case block.BlockPlain, block.BlockIf, block.BlockRet, block.BlockRetJmp, block.BlockExit:
		default:
			return nil // including BlockDefer
		}
		for _, v := range b.Values {
			if call, ok := v.Aux.(*ssa.AuxCall); ok && call.Fn != nil {
				if call.Fn == ir.Syms.Deferreturn || call.Fn.Name == "runtime.(*_panic).start" {
					return nil // recovery resumes this function in the middle
				}
			}
		}
	}

	po := f.Postorder()
	if len(po) != len(f.Blocks) {
		return nil // unreachable blocks
	}
	n := f.NumBlocks()
	sf := &structuredFunc{
		pre:        make([][]layoutOp, n),
		rpo:        make([]int32, n),
		fallthru:   make([]*ssa.Block, n),
		loopLabel:  make([]*obj.Prog, n),
		blockLabel: make([]*obj.Prog, n),
	}
	for i, b := range po {
		sf.rpo[b.ID] = int32(len(po) - 1 - i)
	}
	idom := f.Idom()
	sdom := f.Sdom()

	isHeader := make([]bool, n)
	forwardPreds := make([]int32, n)
	for _, b := range f.Blocks {
		for _, e := range b.Succs {
			c := e.Block()
			if sf.rpo[c.ID] <= sf.rpo[b.ID] {
				if !sdom.IsAncestorEq(c, b) {
					return nil // irreducible
				}
				isHeader[c.ID] = true
			} else {
				forwardPreds[c.ID]++
			}
		}
	}

	// Each block may fall through to one successor that it immediately
	// dominates and that has no other forward predecessors.
	for _, b := range f.Blocks {
		for _, e := range b.Succs {
			c := e.Block()
			if idom[c.ID] == b && forwardPreds[c.ID] == 1 {
				sf.fallthru[b.ID] = c
				break
			}
		}
	}

	// Every other forward branch target gets a label, opened by its
	// immediate dominator.
	children := make([][]*ssa.Block, n)
	for _, b := range f.Blocks {
		if b == f.Entry {
			continue
		}
		if d := idom[b.ID]; sf.fallthru[d.ID] != b {
			children[d.ID] = append(children[d.ID], b)
		}
	}
	for _, ys := range children {
		// Outermost (highest reverse postorder number) first.
		slices.SortFunc(ys, func(a, b *ssa.Block) int { return cmp.Compare(sf.rpo[b.ID], sf.rpo[a.ID]) })
	}

	var ops []layoutOp
	var doTree func(x *ssa.Block)
	doTree = func(x *ssa.Block) {
		if isHeader[x.ID] {
			ops = append(ops, layoutOp{layoutOpenLoop, x})
		}
		ys := children[x.ID]
		for _, y := range ys {
			ops = append(ops, layoutOp{layoutOpenBlock, y})
		}
		ops = append(ops, layoutOp{layoutCode, x})
		if c := sf.fallthru[x.ID]; c != nil {
			doTree(c)
		}
		for i := len(ys) - 1; i >= 0; i-- {
			ops = append(ops, layoutOp{layoutClose, ys[i]})
			doTree(ys[i])
		}
		if isHeader[x.ID] {
			ops = append(ops, layoutOp{layoutClose, x})
		}
	}
	doTree(f.Entry)

	order := make([]*ssa.Block, 0, len(f.Blocks))
	var pending []layoutOp
	for _, op := range ops {
		if op.kind == layoutCode {
			sf.pre[op.b.ID] = pending
			pending = nil
			order = append(order, op.b)
			continue
		}
		pending = append(pending, op)
	}
	if len(order) != len(f.Blocks) {
		base.Fatalf("wasm structured layout of %s: laid out %d of %d blocks", f.Name, len(order), len(f.Blocks))
	}
	sf.post = pending
	sf.last = order[len(order)-1]
	copy(f.Blocks, order)
	return sf
}

func (sf *structuredFunc) emitLayout(s *ssagen.State, ops []layoutOp) {
	for _, op := range ops {
		switch op.kind {
		case layoutOpenLoop:
			sf.loopLabel[op.b.ID] = s.Prog(wasm.ALoop)
		case layoutOpenBlock:
			sf.blockLabel[op.b.ID] = s.Prog(wasm.ABlock)
		case layoutClose:
			s.Prog(wasm.AEnd)
		}
	}
}

// branch emits a br (or br_if, which consumes a condition) from b to c.
func (sf *structuredFunc) branch(s *ssagen.State, as obj.As, b, c *ssa.Block) {
	label := sf.blockLabel[c.ID]
	if sf.rpo[c.ID] <= sf.rpo[b.ID] {
		label = sf.loopLabel[c.ID]
	}
	if label == nil {
		base.Fatalf("wasm structured layout: no label for b%d -> b%d", b.ID, c.ID)
	}
	p := s.Prog(as)
	p.To = obj.Addr{Type: obj.TYPE_BRANCH, Val: label}
}

// genBlock emits the control flow at the end of b.
func (sf *structuredFunc) genBlock(s *ssagen.State, b *ssa.Block) {
	switch b.Kind {
	case block.BlockPlain:
		if c := b.Succs[0].Block(); sf.fallthru[b.ID] != c {
			sf.branch(s, wasm.ABr, b, c)
		}

	case block.BlockIf:
		c0, c1 := b.Succs[0].Block(), b.Succs[1].Block()
		getValue32(s, b.Controls[0])
		switch ft := sf.fallthru[b.ID]; {
		case c0 == c1:
			s.Prog(wasm.ADrop)
			if ft != c0 {
				sf.branch(s, wasm.ABr, b, c0)
			}
		case ft == c1:
			// if true, branch to c0
			sf.branch(s, wasm.ABrIf, b, c0)
		case ft == c0:
			// if false, branch to c1
			s.Prog(wasm.AI32Eqz)
			sf.branch(s, wasm.ABrIf, b, c1)
		default:
			sf.branch(s, wasm.ABrIf, b, c0)
			sf.branch(s, wasm.ABr, b, c1)
		}

	case block.BlockRet:
		s.Prog(obj.ARET)

	case block.BlockExit:
		// The block ends in a call that does not return (e.g. a panic).
		// Saying so lets the wasm compiler ignore the call's results.
		s.Prog(obj.AUNDEF)

	case block.BlockRetJmp:

	default:
		base.FatalfAt(b.Pos, "unexpected block b%d, kind=%v", b.ID, b.Kind)
	}

	if b == sf.last {
		sf.emitLayout(s, sf.post)
	}
}
