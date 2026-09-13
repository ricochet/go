// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package wasm

import (
	"bytes"
	"cmd/internal/obj"
	"cmd/internal/objabi"
	"cmd/internal/sys"
	"encoding/binary"
	"fmt"
	"internal/abi"
	"internal/buildcfg"
	"io"
	"math"
	"strings"
)

var Register = map[string]int16{
	"SP":    REG_SP,
	"CTXT":  REG_CTXT,
	"g":     REG_g,
	"RET0":  REG_RET0,
	"RET1":  REG_RET1,
	"RET2":  REG_RET2,
	"RET3":  REG_RET3,
	"PAUSE": REG_PAUSE,

	"R0":  REG_R0,
	"R1":  REG_R1,
	"R2":  REG_R2,
	"R3":  REG_R3,
	"R4":  REG_R4,
	"R5":  REG_R5,
	"R6":  REG_R6,
	"R7":  REG_R7,
	"R8":  REG_R8,
	"R9":  REG_R9,
	"R10": REG_R10,
	"R11": REG_R11,
	"R12": REG_R12,
	"R13": REG_R13,
	"R14": REG_R14,
	"R15": REG_R15,

	"F0":  REG_F0,
	"F1":  REG_F1,
	"F2":  REG_F2,
	"F3":  REG_F3,
	"F4":  REG_F4,
	"F5":  REG_F5,
	"F6":  REG_F6,
	"F7":  REG_F7,
	"F8":  REG_F8,
	"F9":  REG_F9,
	"F10": REG_F10,
	"F11": REG_F11,
	"F12": REG_F12,
	"F13": REG_F13,
	"F14": REG_F14,
	"F15": REG_F15,

	"F16": REG_F16,
	"F17": REG_F17,
	"F18": REG_F18,
	"F19": REG_F19,
	"F20": REG_F20,
	"F21": REG_F21,
	"F22": REG_F22,
	"F23": REG_F23,
	"F24": REG_F24,
	"F25": REG_F25,
	"F26": REG_F26,
	"F27": REG_F27,
	"F28": REG_F28,
	"F29": REG_F29,
	"F30": REG_F30,
	"F31": REG_F31,

	"V0":  REG_V0,
	"V1":  REG_V1,
	"V2":  REG_V2,
	"V3":  REG_V3,
	"V4":  REG_V4,
	"V5":  REG_V5,
	"V6":  REG_V6,
	"V7":  REG_V7,
	"V8":  REG_V8,
	"V9":  REG_V9,
	"V10": REG_V10,
	"V11": REG_V11,
	"V12": REG_V12,
	"V13": REG_V13,
	"V14": REG_V14,
	"V15": REG_V15,

	"FDEPTH": REG_FDEPTH,

	"PC_B": REG_PC_B,
}

var registerNames []string

func init() {
	obj.RegisterRegister(MINREG, MAXREG, rconv)
	obj.RegisterOpcode(obj.ABaseWasm, Anames)

	registerNames = make([]string, MAXREG-MINREG)
	for name, reg := range Register {
		registerNames[reg-MINREG] = name
	}
}

func rconv(r int) string {
	return registerNames[r-MINREG]
}

var unaryDst = map[obj.As]bool{
	ASet:          true,
	ATee:          true,
	ACall:         true,
	ACallIndirect: true,
	ABr:           true,
	ABrIf:         true,
	ABrTable:      true,
	AI32Store:     true,
	AI64Store:     true,
	AF32Store:     true,
	AF64Store:     true,
	AI32Store8:    true,
	AI32Store16:   true,
	AI64Store8:    true,
	AI64Store16:   true,
	AI64Store32:   true,
	ACALLNORESUME: true,
	ACALLRESULT:   true,
}

var Linkwasm = obj.LinkArch{
	Arch:       sys.ArchWasm,
	Init:       instinit,
	Preprocess: preprocess,
	Assemble:   assemble,
	UnaryDst:   unaryDst,
}

var (
	morestack             *obj.LSym
	morestackNoCtxt       *obj.LSym
	sigpanic              *obj.LSym
	wasm_pc_f_loop_export *obj.LSym
	runtimeNotInitialized *obj.LSym
	coopResumePC          *obj.LSym
	coopResumeSP          *obj.LSym
	coopActive            *obj.LSym
	coopExportEnter       *obj.LSym
	coopMainStarted       *obj.LSym
	coopStartMain         *obj.LSym
	goexit                *obj.LSym
)

// stackful reports whether GOEXPERIMENT=wasmstackful is in effect: calls are
// not resume points and goroutine switches do not unwind the wasm stack.
func stackful() bool {
	return (buildcfg.GOOS == "wasip1" || buildcfg.GOOS == "wasip3") && buildcfg.Experiment.WasmCoopThreads && buildcfg.Experiment.WasmStackful
}

// spParam reports whether GOEXPERIMENT=wasmspparam is in effect (together
// with wasmstackful): functions that follow the Go ABI have the wasm type
// (func (param $SP i32) (result i32)), where the result is the callee's final
// SP with the unwind flag in bit 0 (SP is a multiple of 8). SP lives in the
// local that the SP param occupies, and the SP that callers take from the
// result differs from the SP passed in only if the stack was switched or
// moved. PC_B is a local that is 0 on entry: without resumable functions, a
// function is never entered at a resume point, except runtime·goexit, whose
// first instruction is a NOP anyway. (Wasmtime is much faster with few
// parameters and results.) The SP global is written only before
// calls to functions that do not follow the Go ABI and may read it (host
// imports, which may call back into exports, and the write barrier, which may
// call wbBufFlush) and before exceptions thrown by assembly functions; code
// that does not follow the Go ABI passes and takes it explicitly (see
// wasm_pc_f_loop).
func spParam() bool {
	return stackful() && buildcfg.Experiment.WasmSPParam
}

// regABI reports whether GOEXPERIMENT=wasmregabi is in effect (together with
// wasmspparam): the register ABI is enabled, with the integer argument and
// result registers R0-R2 (see internal/abi/abi_wasm.go). Functions that
// follow the Go ABI (ABIInternal or ABI0) have the wasm type
// (func (param $SP i32) (param $R0 i64) (param $R1 i64) (param $R2 i64)
// (result i32 i64 i64 i64)), with results SP|unwind, R0, R1 and R2. On arm64,
// Wasmtime passes at most four parameters and results in native registers.
func regABI() bool {
	return spParam() && buildcfg.Experiment.RegabiArgs
}

// regABIArgs is the number of integer argument registers with
// GOEXPERIMENT=wasmregabi.
const regABIArgs = 3

// spFree lists functions that do not follow the Go ABI and never read the SP
// global, so callers need not write it first (GOEXPERIMENT=wasmspparam).
var spFree = map[string]bool{
	"runtime.wasmDiv":    true,
	"runtime.wasmTruncS": true,
	"runtime.wasmTruncU": true,
	"cmpbody":            true,
	"memeqbody":          true,
	"memcmp":             true,
	"memchr":             true,
}

// coopResumeTag is the index of the exception tag thrown by runtime·gogo
// (GOEXPERIMENT=wasmstackful). The linker defines it as the only tag.
const coopResumeTag = 0

// In assembly, "Throw $coopResumeKeepSP" throws coopResumeTag without writing
// the SP global (GOEXPERIMENT=wasmspparam), for a thread that exits without
// resuming a goroutine, while the SP global belongs to another thread.
const coopResumeKeepSP = 1

// panicStart is (*_panic).start, whose return runtime·recovery resumes when a
// recovered panic would skip a pending Goexit.
const panicStart = "runtime.(*_panic).start"

// isRecoveryCall reports whether a call to sym makes the calling function a
// place where runtime·recovery can resume (GOEXPERIMENT=wasmstackful).
func isRecoveryCall(sym *obj.LSym) bool {
	return sym.Name == "runtime.deferreturn" || sym.Name == panicStart
}

const (
	/* mark flags */
	WasmImport = 1 << 0
	// rethrow marks a Throw that passes on an exception caught in a frame
	// that is not the one being resumed; its SP is stale, so unlike other
	// throws (GOEXPERIMENT=wasmspparam) it must not write the SP global.
	rethrow = 1 << 1
	// dropUnwind marks a call whose caller ignores the unwind result
	// (GOEXPERIMENT=wasmspparam).
	dropUnwind = 1 << 2
)

const (
	// This is a special wasm module name that when used as the module name
	// in //go:wasmimport will cause the generated code to pass the stack pointer
	// directly to the imported function. In other words, any function that
	// uses the gojs module understands the internal Go WASM ABI directly.
	GojsModule = "gojs"
)

func instinit(ctxt *obj.Link) {
	morestack = ctxt.Lookup("runtime.morestack")
	morestackNoCtxt = ctxt.Lookup("runtime.morestack_noctxt")
	sigpanic = ctxt.LookupABI("runtime.sigpanic", obj.ABIInternal)
	wasm_pc_f_loop_export = ctxt.Lookup("wasm_pc_f_loop_export")
	runtimeNotInitialized = ctxt.Lookup("runtime.notInitialized")
	if stackful() {
		coopResumePC = ctxt.Lookup("runtime.coopResumePC")
		coopResumeSP = ctxt.Lookup("runtime.coopResumeSP")
		coopActive = ctxt.Lookup("runtime.coopActive")
		coopExportEnter = ctxt.LookupABI("runtime.wasmCoopExportEnter", obj.ABIInternal)
		coopMainStarted = ctxt.Lookup("runtime.coopMainStarted")
		coopStartMain = ctxt.Lookup("runtime.wasmCoopStartMain")
		goexit = ctxt.LookupABI("runtime.goexit", obj.ABI0)
	}
}

func preprocess(ctxt *obj.Link, s *obj.LSym, newprog obj.ProgAlloc) {
	appendp := func(p *obj.Prog, as obj.As, args ...obj.Addr) *obj.Prog {
		if p.As != obj.ANOP {
			p2 := obj.Appendp(p, newprog)
			p2.Pc = p.Pc
			p = p2
		}
		p.As = as
		switch len(args) {
		case 0:
			p.From = obj.Addr{}
			p.To = obj.Addr{}
		case 1:
			if unaryDst[as] {
				p.From = obj.Addr{}
				p.To = args[0]
			} else {
				p.From = args[0]
				p.To = obj.Addr{}
			}
		case 2:
			p.From = args[0]
			p.To = args[1]
		default:
			panic("bad args")
		}
		return p
	}

	framesize := s.Func().Text.To.Offset
	if framesize < 0 {
		panic("bad framesize")
	}
	s.Func().Args = s.Func().Text.To.Val.(int32)
	s.Func().Locals = int32(framesize)

	// If the function exists just to call out to a wasmimport, then
	// generate the code to translate from our internal Go-stack
	// based call convention to the native webassembly call convention.
	if s.Func().WasmImport != nil {
		genWasmImportWrapper(s, appendp)

		// It should be 0 already, but we'll set it to 0 anyway just to be sure
		// that the code below which adds frame expansion code to the function body
		// isn't run. We don't want the frame expansion code because our function
		// body is just the code to translate and call the imported function.
		framesize = 0
	} else if s.Func().WasmExport != nil {
		genWasmExportWrapper(s, appendp)
	}

	pPrologueEnd := s.Func().Text
	if framesize > 0 && s.Func().WasmExport == nil { // genWasmExportWrapper has its own prologue generation
		p := s.Func().Text
		p = appendp(p, AGet, regAddr(REG_SP))
		p = appendp(p, AI32Const, constAddr(framesize))
		p = appendp(p, AI32Sub)
		p = appendp(p, ASet, regAddr(REG_SP))
		p.Spadj = int32(framesize)
		pPrologueEnd = p
	}

	// With GOEXPERIMENT=wasmstackful, panic recovery resumes a function in
	// the middle, which (unlike the old unwind/rewind scheme) requires that
	// frame to still be on the wasm stack. runtime·recovery gogos either to a
	// function's (first) deferreturn call, or, when the recovery would skip
	// a pending Goexit, back to the return from Goexit's (*_panic).start
	// call. Such a function catches the exception thrown for gogo if the
	// target is one of its recovery PCs at its own depth below stack.hi, and
	// dispatches to that PC.
	hasRecoveryTarget := false
	if stackful() {
		for p := s.Func().Text; p != nil; p = p.Link {
			if p.As == obj.ACALL && p.To.Sym != nil && isRecoveryCall(p.To.Sym) {
				hasRecoveryTarget = true
				break
			}
		}
	}
	if hasRecoveryTarget {
		// FDEPTH = g.stack.hi - SP
		p := pPrologueEnd
		p = appendp(p, AGet, regAddr(REGG))
		p = appendp(p, AI32WrapI64)
		p = appendp(p, AI64Load, constAddr(1*int64(ctxt.Arch.PtrSize))) // G.stack.hi
		p = appendp(p, AGet, regAddr(REG_SP))
		p = appendp(p, AI64ExtendI32U)
		p = appendp(p, AI64Sub)
		p = appendp(p, ASet, regAddr(REG_FDEPTH))
	}

	// If the framesize is 0, then imply nosplit because it's a specially
	// generated function.
	needMoreStack := framesize > 0 && !s.Func().Text.From.Sym.NoSplit()

	// If the maymorestack debug option is enabled, insert the
	// call to maymorestack *before* processing resume points so
	// we can construct a resume point after maymorestack for
	// morestack to resume at.
	var pMorestack = s.Func().Text
	if needMoreStack && ctxt.Flag_maymorestack != "" {
		p := pMorestack

		// Save REGCTXT on the stack.
		const tempFrame = 8
		p = appendp(p, AGet, regAddr(REG_SP))
		p = appendp(p, AI32Const, constAddr(tempFrame))
		p = appendp(p, AI32Sub)
		p = appendp(p, ASet, regAddr(REG_SP))
		p.Spadj = tempFrame
		ctxtp := obj.Addr{
			Type:   obj.TYPE_MEM,
			Reg:    REG_SP,
			Offset: 0,
		}
		p = appendp(p, AMOVD, regAddr(REGCTXT), ctxtp)

		// maymorestack must not itself preempt because we
		// don't have full stack information, so this can be
		// ACALLNORESUME.
		p = appendp(p, ACALLNORESUME, constAddr(0))
		// See ../x86/obj6.go
		sym := ctxt.LookupABI(ctxt.Flag_maymorestack, s.ABI())
		p.To = obj.Addr{Type: obj.TYPE_MEM, Name: obj.NAME_EXTERN, Sym: sym}

		// Restore REGCTXT.
		p = appendp(p, AMOVD, ctxtp, regAddr(REGCTXT))
		p = appendp(p, AGet, regAddr(REG_SP))
		p = appendp(p, AI32Const, constAddr(tempFrame))
		p = appendp(p, AI32Add)
		p = appendp(p, ASet, regAddr(REG_SP))
		p.Spadj = -tempFrame

		// Add an explicit ARESUMEPOINT after maymorestack for
		// morestack to resume at.
		pMorestack = appendp(p, ARESUMEPOINT)
	}

	// Introduce resume points for CALL instructions
	// and collect other explicit resume points.
	numResumePoints := 0
	explicitBlockDepth := 0
	pc := int64(0) // pc is only incremented when necessary, this avoids bloat of the BrTable instruction
	var tableIdxs []uint64
	tablePC := int64(0)
	base := ctxt.PosTable.Pos(s.Func().Text.Pos).Base()
	for p := s.Func().Text; p != nil; p = p.Link {
		prevBase := base
		base = ctxt.PosTable.Pos(p.Pos).Base()
		switch p.As {
		case ABlock, ALoop, AIf, ATryTable:
			explicitBlockDepth++

		case AEnd:
			if explicitBlockDepth == 0 {
				panic("End without block")
			}
			explicitBlockDepth--

		case ARESUMEPOINT:
			if explicitBlockDepth != 0 {
				panic("RESUME can only be used on toplevel")
			}
			p.As = AEnd
			for tablePC <= pc {
				tableIdxs = append(tableIdxs, uint64(numResumePoints))
				tablePC++
			}
			numResumePoints++
			pc++

		case obj.ACALL:
			resume := !stackful() || (p.To.Sym != nil && p.To.Sym.Name == panicStart)
			if explicitBlockDepth != 0 && resume {
				// With GOEXPERIMENT=wasmstackful, calls may be nested in
				// structured control flow (see cmd/compile/internal/wasm).
				panic("CALL can only be used on toplevel, try CALLNORESUME instead")
			}
			if resume {
				// With GOEXPERIMENT=wasmstackful, only the return from
				// (*_panic).start is ever resumed (by recovery).
				appendp(p, ARESUMEPOINT)
			}
		}

		p.Pc = pc

		// Increase pc whenever some pc-value table needs a new entry. Don't increase it
		// more often to avoid bloat of the BrTable instruction.
		// The "base != prevBase" condition detects inlined instructions. They are an
		// implicit call, so entering and leaving this section affects the stack trace.
		// Without resume points after calls (GOEXPERIMENT=wasmstackful), a CALL still
		// needs its own PC so that its return address differs from the call site.
		if p.As == ACALLNORESUME || p.As == ACALLRESULT || (p.As == obj.ACALL && stackful()) || p.As == obj.ANOP || p.As == ANop || p.Spadj != 0 || base != prevBase {
			pc++
			if p.To.Sym == sigpanic {
				// The panic stack trace expects the PC at the call of sigpanic,
				// not the next one. However, runtime.Caller subtracts 1 from the
				// PC. To make both PC and PC-1 work (have the same line number),
				// we advance the PC by 2 at sigpanic.
				pc++
			}
		}
	}
	tableIdxs = append(tableIdxs, uint64(numResumePoints))
	s.Size = pc + 1
	if pc >= 1<<16 {
		ctxt.Diag("function too big: %s exceeds 65536 blocks", s)
	}

	if needMoreStack {
		p := pMorestack

		if framesize <= abi.StackSmall {
			// small stack: SP <= stackguard
			// Get SP
			// Get g
			// I32WrapI64
			// I32Load $stackguard0
			// I32GtU

			p = appendp(p, AGet, regAddr(REG_SP))
			p = appendp(p, AGet, regAddr(REGG))
			p = appendp(p, AI32WrapI64)
			p = appendp(p, AI32Load, constAddr(2*int64(ctxt.Arch.PtrSize))) // G.stackguard0
			p = appendp(p, AI32LeU)
		} else {
			// large stack: SP-framesize <= stackguard-StackSmall
			//              SP <= stackguard+(framesize-StackSmall)
			// Get SP
			// Get g
			// I32WrapI64
			// I32Load $stackguard0
			// I32Const $(framesize-StackSmall)
			// I32Add
			// I32GtU

			p = appendp(p, AGet, regAddr(REG_SP))
			p = appendp(p, AGet, regAddr(REGG))
			p = appendp(p, AI32WrapI64)
			p = appendp(p, AI32Load, constAddr(2*int64(ctxt.Arch.PtrSize))) // G.stackguard0
			p = appendp(p, AI32Const, constAddr(framesize-abi.StackSmall))
			p = appendp(p, AI32Add)
			p = appendp(p, AI32LeU)
		}
		// TODO(neelance): handle wraparound case

		p = appendp(p, AIf)
		// This CALL does *not* have a resume point after it
		// (we already inserted all of the resume points). As
		// a result, morestack will resume at the *previous*
		// resume point (typically, the beginning of the
		// function) and perform the morestack check again.
		// This is why we don't need an explicit loop like
		// other architectures.
		// With GOEXPERIMENT=wasmstackful, morestack returns
		// normally instead, so loop back to redo the check.
		if regABI() {
			// Spill register arguments to their spill slots, where
			// morestack can adjust any pointers into the stack.
			p = spillRegArgs(s, p, newprog, (*obj.FuncInfo).SpillRegisterArgs)
		}
		p = appendp(p, obj.ACALL, constAddr(0))
		if s.Func().Text.From.Sym.NeedCtxt() {
			p.To = obj.Addr{Type: obj.TYPE_MEM, Name: obj.NAME_EXTERN, Sym: morestack}
		} else {
			p.To = obj.Addr{Type: obj.TYPE_MEM, Name: obj.NAME_EXTERN, Sym: morestackNoCtxt}
		}
		if regABI() {
			p = spillRegArgs(s, p, newprog, (*obj.FuncInfo).UnspillRegisterArgs)
		}
		if stackful() {
			p = appendp(p, ABr, constAddr(1)) // to the Loop below
		}
		p = appendp(p, AEnd)
		if stackful() {
			p = appendp(p, AEnd)       // end Loop
			appendp(pMorestack, ALoop) // before the check
		}
	}

	// record the branches targeting the entry loop and the unwind exit,
	// their targets will be filled in later
	var entryPointLoopBranches []*obj.Prog
	var unwindExitBranches []*obj.Prog
	var recoveryPCs []int64 // PC_B values where recovery may resume this function
	sawDeferReturn := false
	currentDepth := 0
	for p := s.Func().Text; p != nil; p = p.Link {
		switch p.As {
		case ABlock, ALoop, AIf, ATryTable:
			currentDepth++
		case AEnd:
			currentDepth--
		}

		switch p.As {
		case obj.AJMP:
			jmp := *p
			p.As = obj.ANOP

			if jmp.To.Type == obj.TYPE_BRANCH {
				// jump to basic block
				p = appendp(p, AI32Const, constAddr(jmp.To.Val.(*obj.Prog).Pc))
				p = appendp(p, ASet, regAddr(REG_PC_B)) // write next basic block to PC_B
				p = appendp(p, ABr)                     // jump to beginning of entryPointLoop
				entryPointLoopBranches = append(entryPointLoopBranches, p)
				break
			}

			// low-level WebAssembly call to function
			switch jmp.To.Type {
			case obj.TYPE_MEM:
				if !notUsePC_B[jmp.To.Sym.Name] && !spParam() {
					// Set PC_B parameter to function entry.
					p = appendp(p, AI32Const, constAddr(0))
				}
				p = appendp(p, ACall, jmp.To)

			case obj.TYPE_NONE:
				// (target PC is on stack)
				p = appendp(p, AI64Const, constAddr(16)) // only needs PC_F bits (16-47), PC_B bits (0-15) are zero
				p = appendp(p, AI64ShrU)
				p = appendp(p, AI32WrapI64)

				// Set PC_B parameter to function entry.
				// We need to push this before pushing the target PC_F,
				// so temporarily pop PC_F, using our REG_PC_B as a
				// scratch register, and push it back after pushing 0.
				p = appendp(p, ASet, regAddr(REG_PC_B))
				if !spParam() {
					p = appendp(p, AI32Const, constAddr(0))
				} else {
					p = appendp(p, AGet, regAddr(REG_SP))
					if regABI() {
						for r := int16(REG_R0); r < REG_R0+regABIArgs; r++ {
							p = appendp(p, AGet, regAddr(r))
						}
					}
				}
				p = appendp(p, AGet, regAddr(REG_PC_B))

				p = appendp(p, ACallIndirect)

			default:
				panic("bad target for JMP")
			}

			p = appendp(p, AReturn)

		case obj.ACALL, ACALLNORESUME, ACALLRESULT:
			call := *p
			p.As = obj.ANOP

			pcAfterCall := call.Link.Pc
			if call.To.Sym == sigpanic {
				pcAfterCall-- // sigpanic expects to be called without advancing the pc
			}
			if hasRecoveryTarget && call.To.Sym != nil {
				switch call.To.Sym.Name {
				case "runtime.deferreturn":
					if !sawDeferReturn {
						// Matches cmd/link/internal/ld.computeDeferReturn,
						// which uses the first deferreturn call.
						recoveryPCs = append(recoveryPCs, pcAfterCall-1)
						sawDeferReturn = true
					}
				case panicStart:
					// _panic.start records its return PC (sys.GetCallerPC).
					recoveryPCs = append(recoveryPCs, pcAfterCall)
				}
			}

			// SP -= 8
			p = appendp(p, AGet, regAddr(REG_SP))
			p = appendp(p, AI32Const, constAddr(8))
			p = appendp(p, AI32Sub)
			p = appendp(p, ASet, regAddr(REG_SP))

			// write return address to Go stack
			p = appendp(p, AGet, regAddr(REG_SP))
			p = appendp(p, AI64Const, obj.Addr{
				Type:   obj.TYPE_ADDR,
				Name:   obj.NAME_EXTERN,
				Sym:    s,           // PC_F
				Offset: pcAfterCall, // PC_B
			})
			p = appendp(p, AI64Store, constAddr(0))

			// low-level WebAssembly call to function
			switch call.To.Type {
			case obj.TYPE_MEM:
				if !notUsePC_B[call.To.Sym.Name] && !spParam() {
					// Set PC_B parameter to function entry.
					p = appendp(p, AI32Const, constAddr(0))
				}
				p = appendp(p, ACall, call.To)

			case obj.TYPE_NONE:
				// (target PC is on stack)
				p = appendp(p, AI64Const, constAddr(16)) // only needs PC_F bits (16-47), PC_B bits (0-15) are zero
				p = appendp(p, AI64ShrU)
				p = appendp(p, AI32WrapI64)

				// Set PC_B parameter to function entry.
				// We need to push this before pushing the target PC_F,
				// so temporarily pop PC_F, using our PC_B as a
				// scratch register, and push it back after pushing 0.
				p = appendp(p, ASet, regAddr(REG_PC_B))
				if !spParam() {
					p = appendp(p, AI32Const, constAddr(0))
				} else {
					p = appendp(p, AGet, regAddr(REG_SP))
					if regABI() {
						for r := int16(REG_R0); r < REG_R0+regABIArgs; r++ {
							p = appendp(p, AGet, regAddr(r))
						}
					}
				}
				p = appendp(p, AGet, regAddr(REG_PC_B))

				p = appendp(p, ACallIndirect)

			default:
				panic("bad target for CALL")
			}

			// return value of call is on the top of the stack, indicating whether to unwind the WebAssembly stack
			if call.As == ACALLRESULT {
				// The caller inspects the result.
			} else if stackful() && ctxt.Pkgpath != "runtime" {
				// With GOEXPERIMENT=wasmstackful, only runtime functions
				// unwind: gogo returns 1 through the scheduler frames up
				// to mcall or morestack, which are all in package runtime.
				// Nothing is ever rewound, so there are no resume points.
				if spParam() {
					p.Mark |= dropUnwind // see assemble
				} else {
					p = appendp(p, ADrop)
				}
			} else if call.As == ACALLNORESUME && call.To.Sym != sigpanic { // sigpanic unwinds the stack, but it never resumes
				// trying to unwind WebAssembly stack but call has no resume point, terminate with error
				p = appendp(p, AIf)
				p = appendp(p, obj.AUNDEF)
				p = appendp(p, AEnd)
			} else {
				// unwinding WebAssembly stack to switch goroutine, return 1
				p = appendp(p, ABrIf)
				unwindExitBranches = append(unwindExitBranches, p)
			}

		case obj.ARET, ARETUNWIND:
			ret := *p
			p.As = obj.ANOP

			if framesize > 0 {
				// SP += framesize
				p = appendp(p, AGet, regAddr(REG_SP))
				p = appendp(p, AI32Const, constAddr(framesize))
				p = appendp(p, AI32Add)
				p = appendp(p, ASet, regAddr(REG_SP))
				// TODO(neelance): This should theoretically set Spadj, but it only works without.
				// p.Spadj = int32(-framesize)
			}

			if ret.To.Type == obj.TYPE_MEM {
				// Set PC_B parameter to function entry.
				if !spParam() {
					p = appendp(p, AI32Const, constAddr(0))
				}

				// low-level WebAssembly call to function
				p = appendp(p, ACall, ret.To)
				p = appendp(p, AReturn)
				break
			}

			// SP += 8
			p = appendp(p, AGet, regAddr(REG_SP))
			p = appendp(p, AI32Const, constAddr(8))
			p = appendp(p, AI32Add)
			p = appendp(p, ASet, regAddr(REG_SP))

			if ret.As == ARETUNWIND {
				// function needs to unwind the WebAssembly stack, return 1
				p = appendp(p, AI32Const, constAddr(1))
				p = appendp(p, AReturn)
				break
			}

			// not unwinding the WebAssembly stack, return 0
			p = appendp(p, AI32Const, constAddr(0))
			p = appendp(p, AReturn)
		}
	}

	for p := s.Func().Text; p != nil; p = p.Link {
		switch p.From.Name {
		case obj.NAME_AUTO:
			p.From.Offset += framesize
		case obj.NAME_PARAM:
			p.From.Reg = REG_SP
			p.From.Offset += framesize + 8 // parameters are after the frame and the 8-byte return address
		}

		switch p.To.Name {
		case obj.NAME_AUTO:
			p.To.Offset += framesize
		case obj.NAME_PARAM:
			p.To.Reg = REG_SP
			p.To.Offset += framesize + 8 // parameters are after the frame and the 8-byte return address
		}

		switch p.As {
		case AGet:
			if p.From.Type == obj.TYPE_ADDR {
				get := *p
				p.As = obj.ANOP

				switch get.From.Name {
				case obj.NAME_EXTERN:
					p = appendp(p, AI64Const, get.From)
				case obj.NAME_AUTO, obj.NAME_PARAM:
					p = appendp(p, AGet, regAddr(get.From.Reg))
					if get.From.Reg == REG_SP {
						p = appendp(p, AI64ExtendI32U)
					}
					if get.From.Offset != 0 {
						p = appendp(p, AI64Const, constAddr(get.From.Offset))
						p = appendp(p, AI64Add)
					}
				default:
					panic("bad Get: invalid name")
				}
			}

		case AI32Load, AI64Load, AF32Load, AF64Load, AI32Load8S, AI32Load8U, AI32Load16S, AI32Load16U,
			AI64Load8S, AI64Load8U, AI64Load16S, AI64Load16U, AI64Load32S, AI64Load32U, AV128Load:
			if p.From.Type == obj.TYPE_MEM {
				as := p.As
				from := p.From

				p.As = AGet
				p.From = regAddr(from.Reg)

				if from.Reg != REG_SP {
					p = appendp(p, AI32WrapI64)
				}

				p = appendp(p, as, constAddr(from.Offset))
			}

		case AMOVB, AMOVH, AMOVW, AMOVD:
			mov := *p
			p.As = obj.ANOP

			var loadAs obj.As
			var storeAs obj.As
			switch mov.As {
			case AMOVB:
				loadAs = AI64Load8U
				storeAs = AI64Store8
			case AMOVH:
				loadAs = AI64Load16U
				storeAs = AI64Store16
			case AMOVW:
				loadAs = AI64Load32U
				storeAs = AI64Store32
			case AMOVD:
				loadAs = AI64Load
				storeAs = AI64Store
			}

			appendValue := func() {
				switch mov.From.Type {
				case obj.TYPE_CONST:
					p = appendp(p, AI64Const, constAddr(mov.From.Offset))

				case obj.TYPE_ADDR:
					switch mov.From.Name {
					case obj.NAME_NONE, obj.NAME_PARAM, obj.NAME_AUTO:
						p = appendp(p, AGet, regAddr(mov.From.Reg))
						if mov.From.Reg == REG_SP {
							p = appendp(p, AI64ExtendI32U)
						}
						p = appendp(p, AI64Const, constAddr(mov.From.Offset))
						p = appendp(p, AI64Add)
					case obj.NAME_EXTERN:
						p = appendp(p, AI64Const, mov.From)
					default:
						panic("bad name for MOV")
					}

				case obj.TYPE_REG:
					p = appendp(p, AGet, mov.From)
					if mov.From.Reg == REG_SP {
						p = appendp(p, AI64ExtendI32U)
					}

				case obj.TYPE_MEM:
					p = appendp(p, AGet, regAddr(mov.From.Reg))
					if mov.From.Reg != REG_SP {
						p = appendp(p, AI32WrapI64)
					}
					p = appendp(p, loadAs, constAddr(mov.From.Offset))

				default:
					panic("bad MOV type")
				}
			}

			switch mov.To.Type {
			case obj.TYPE_REG:
				appendValue()
				if mov.To.Reg == REG_SP {
					p = appendp(p, AI32WrapI64)
				}
				p = appendp(p, ASet, mov.To)

			case obj.TYPE_MEM:
				switch mov.To.Name {
				case obj.NAME_NONE, obj.NAME_PARAM:
					p = appendp(p, AGet, regAddr(mov.To.Reg))
					if mov.To.Reg != REG_SP {
						p = appendp(p, AI32WrapI64)
					}
				case obj.NAME_EXTERN:
					p = appendp(p, AI32Const, obj.Addr{Type: obj.TYPE_ADDR, Name: obj.NAME_EXTERN, Sym: mov.To.Sym})
				default:
					panic("bad MOV name")
				}
				appendValue()
				p = appendp(p, storeAs, constAddr(mov.To.Offset))

			default:
				panic("bad MOV type")
			}
		}
	}

	{
		p := s.Func().Text
		var retryLoop *obj.Prog
		if hasRecoveryTarget {
			if len(recoveryPCs) == 0 {
				panic("wasmstackful: recovery call not found")
			}
			retryLoop = appendp(p, ALoop)
			p = appendp(retryLoop, ABlock)                      // caught
			p = appendp(p, ATryTable, constAddr(coopResumeTag)) // catch → caught
		}
		if len(unwindExitBranches) > 0 {
			p = appendp(p, ABlock) // unwindExit, used to return 1 when unwinding the stack
			for _, b := range unwindExitBranches {
				b.To = obj.Addr{Type: obj.TYPE_BRANCH, Val: p}
			}
		}
		if len(entryPointLoopBranches) > 0 {
			p = appendp(p, ALoop) // entryPointLoop, used to jump between basic blocks
			for _, b := range entryPointLoopBranches {
				b.To = obj.Addr{Type: obj.TYPE_BRANCH, Val: p}
			}
		}
		if numResumePoints > 0 {
			// Add Block instructions for resume points and BrTable to jump to selected resume point.
			for i := 0; i < numResumePoints+1; i++ {
				p = appendp(p, ABlock)
			}
			p = appendp(p, AGet, regAddr(REG_PC_B)) // read next basic block from PC_B
			p = appendp(p, ABrTable, obj.Addr{Val: tableIdxs})
			p = appendp(p, AEnd) // end of Block
		}
		for p.Link != nil {
			p = p.Link // function instructions
		}
		if len(entryPointLoopBranches) > 0 {
			p = appendp(p, AEnd) // end of entryPointLoop
		}
		p = appendp(p, obj.AUNDEF)
		if len(unwindExitBranches) > 0 {
			p = appendp(p, AEnd) // end of unwindExit
			p = appendp(p, AI32Const, constAddr(1))
			if hasRecoveryTarget {
				p = appendp(p, AReturn) // not the end of the function body
			}
		}
		if hasRecoveryTarget {
			externAddr := func(sym *obj.LSym, off int64) obj.Addr {
				return obj.Addr{Type: obj.TYPE_ADDR, Name: obj.NAME_EXTERN, Sym: sym, Offset: off}
			}
			loadExtern := func(p *obj.Prog, sym *obj.LSym) *obj.Prog {
				p = appendp(p, AI64Const, externAddr(sym, 0))
				p = appendp(p, AI32WrapI64)
				return appendp(p, AI64Load, constAddr(0))
			}
			throwUnless := func(p *obj.Prog) *obj.Prog {
				p = appendp(p, AI32Eqz)
				p = appendp(p, AIf)
				p = appendp(p, AThrow, constAddr(coopResumeTag))
				p.Mark |= rethrow
				return appendp(p, AEnd)
			}

			p = appendp(p, AEnd) // end of try_table
			p = appendp(p, obj.AUNDEF)
			p = appendp(p, AEnd) // end of caught

			// Only resume here if gogo targets one of this function's
			// recovery PCs...
			for i, pc := range recoveryPCs {
				p = loadExtern(p, coopResumePC)
				p = appendp(p, AI64Const, externAddr(s, pc))
				p = appendp(p, AI64Eq)
				if i > 0 {
					p = appendp(p, AI32Or)
				}
			}
			p = throwUnless(p)
			// ...in this activation: g.stack.hi - gobuf.sp == FDEPTH.
			p = appendp(p, AGet, regAddr(REGG))
			p = appendp(p, AI32WrapI64)
			p = appendp(p, AI64Load, constAddr(1*int64(ctxt.Arch.PtrSize))) // G.stack.hi
			p = loadExtern(p, coopResumeSP)
			p = appendp(p, AI64Sub)
			p = appendp(p, AGet, regAddr(REG_FDEPTH))
			p = appendp(p, AI64Eq)
			p = throwUnless(p)

			// gogo already set g and CTXT. Reload SP and dispatch to the PC.
			p = loadExtern(p, coopResumeSP)
			p = appendp(p, AI32WrapI64)
			p = appendp(p, ASet, regAddr(REG_SP))
			p = loadExtern(p, coopResumePC)
			p = appendp(p, AI32WrapI64)
			p = appendp(p, AI32Const, constAddr(0xffff))
			p = appendp(p, AI32And)
			p = appendp(p, ASet, regAddr(REG_PC_B))
			p = appendp(p, ABr, obj.Addr{Type: obj.TYPE_BRANCH, Val: retryLoop})
			p = appendp(p, AEnd) // end of retryLoop
			p = appendp(p, obj.AUNDEF)
		}
	}

	currentDepth = 0
	blockDepths := make(map[*obj.Prog]int)
	for p := s.Func().Text; p != nil; p = p.Link {
		switch p.As {
		case ABlock, ALoop, AIf, ATryTable:
			currentDepth++
			blockDepths[p] = currentDepth
		case AEnd:
			currentDepth--
		}

		switch p.As {
		case ABr, ABrIf:
			if p.To.Type == obj.TYPE_BRANCH {
				blockDepth, ok := blockDepths[p.To.Val.(*obj.Prog)]
				if !ok {
					panic("label not at block")
				}
				p.To = constAddr(int64(currentDepth - blockDepth))
			}
		}
	}
}

// Generate function body for wasmimport wrapper function.
func genWasmImportWrapper(s *obj.LSym, appendp func(p *obj.Prog, as obj.As, args ...obj.Addr) *obj.Prog) {
	wi := s.Func().WasmImport
	wi.CreateAuxSym()
	p := s.Func().Text
	if p.Link != nil {
		panic("wrapper functions for WASM imports should not have a body")
	}
	to := obj.Addr{
		Type: obj.TYPE_MEM,
		Name: obj.NAME_EXTERN,
		Sym:  s,
	}

	// If the module that the import is for is our magic "gojs" module, then this
	// indicates that the called function understands the Go stack-based call convention
	// so we just pass the stack pointer to it, knowing it will read the params directly
	// off the stack and push the results into memory based on the stack pointer.
	if wi.Module == GojsModule {
		// The called function has a signature of 'func(sp int)'. It has access to the memory
		// value somewhere to be able to address the memory based on the "sp" value.

		p = appendp(p, AGet, regAddr(REG_SP))
		p = appendp(p, ACall, to)

		p.Mark = WasmImport
	} else {
		if len(wi.Results) > 1 {
			// TODO(evanphx) implement support for the multi-value proposal:
			// https://github.com/WebAssembly/multi-value/blob/master/proposals/multi-value/Overview.md
			panic("invalid results type") // impossible until multi-value proposal has landed
		}
		for _, f := range wi.Params {
			// Each load instructions will consume the value of sp on the stack, so
			// we need to read sp for each param. WASM appears to not have a stack dup instruction
			// (a strange omission for a stack-based VM), if it did, we'd be using the dup here.
			p = appendp(p, AGet, regAddr(REG_SP))

			// Offset is the location of the param on the Go stack (ie relative to sp).
			// Because of our call convention, the parameters are located an additional 8 bytes
			// from sp because we store the return address as an int64 at the bottom of the stack.
			// Ie the stack looks like [return_addr, param3, param2, param1, etc]

			// Ergo, we add 8 to the true byte offset of the param to skip the return address.
			loadOffset := f.Offset + 8

			// We're reading the value from the Go stack onto the WASM stack and leaving it there
			// for CALL to pick them up.
			switch f.Type {
			case obj.WasmI32:
				p = appendp(p, AI32Load, constAddr(loadOffset))
			case obj.WasmI64:
				p = appendp(p, AI64Load, constAddr(loadOffset))
			case obj.WasmF32:
				p = appendp(p, AF32Load, constAddr(loadOffset))
			case obj.WasmF64:
				p = appendp(p, AF64Load, constAddr(loadOffset))
			case obj.WasmV128:
				p = appendp(p, AV128Load, constAddr(loadOffset))
			case obj.WasmPtr:
				p = appendp(p, AI32Load, constAddr(loadOffset))
			case obj.WasmBool:
				p = appendp(p, AI32Load8U, constAddr(loadOffset))
			default:
				panic("bad param type")
			}
		}

		// The call instruction is marked as being for a wasm import so that a later phase
		// will generate relocation information that allows us to patch this with then
		// offset of the imported function in the wasm imports.
		p = appendp(p, ACall, to)
		p.Mark = WasmImport

		if len(wi.Results) == 1 {
			f := wi.Results[0]

			// Much like with the params, we need to adjust the offset we store the result value
			// to by 8 bytes to account for the return address on the Go stack.
			storeOffset := f.Offset + 8

			// We need to push SP on the Wasm stack for the Store instruction, which needs to
			// be pushed before the value (call result). So we pop the value into a register,
			// push SP, and push the value back.
			// We cannot get the SP onto the stack before the call, as if the host function
			// calls back into Go, the Go stack may have moved.
			switch f.Type {
			case obj.WasmI32:
				p = appendp(p, AI64ExtendI32U) // the register is 64-bit, so we have to extend
				p = appendp(p, ASet, regAddr(REG_R0))
				p = appendp(p, AGet, regAddr(REG_SP))
				p = appendp(p, AGet, regAddr(REG_R0))
				p = appendp(p, AI64Store32, constAddr(storeOffset))
			case obj.WasmI64:
				p = appendp(p, ASet, regAddr(REG_R0))
				p = appendp(p, AGet, regAddr(REG_SP))
				p = appendp(p, AGet, regAddr(REG_R0))
				p = appendp(p, AI64Store, constAddr(storeOffset))
			case obj.WasmF32:
				p = appendp(p, ASet, regAddr(REG_F0))
				p = appendp(p, AGet, regAddr(REG_SP))
				p = appendp(p, AGet, regAddr(REG_F0))
				p = appendp(p, AF32Store, constAddr(storeOffset))
			case obj.WasmF64:
				p = appendp(p, ASet, regAddr(REG_F16))
				p = appendp(p, AGet, regAddr(REG_SP))
				p = appendp(p, AGet, regAddr(REG_F16))
				p = appendp(p, AF64Store, constAddr(storeOffset))
			case obj.WasmV128:
				p = appendp(p, ASet, regAddr(REG_V0))
				p = appendp(p, AGet, regAddr(REG_SP))
				p = appendp(p, AGet, regAddr(REG_V0))
				p = appendp(p, AV128Store, constAddr(storeOffset))
			case obj.WasmPtr:
				p = appendp(p, AI64ExtendI32U)
				p = appendp(p, ASet, regAddr(REG_R0))
				p = appendp(p, AGet, regAddr(REG_SP))
				p = appendp(p, AGet, regAddr(REG_R0))
				p = appendp(p, AI64Store, constAddr(storeOffset))
			case obj.WasmBool:
				p = appendp(p, AI64ExtendI32U)
				p = appendp(p, ASet, regAddr(REG_R0))
				p = appendp(p, AGet, regAddr(REG_SP))
				p = appendp(p, AGet, regAddr(REG_R0))
				p = appendp(p, AI64Store8, constAddr(storeOffset))
			default:
				panic("bad result type")
			}
		}
	}

	p = appendp(p, obj.ARET)
}

// Generate function body for wasmexport wrapper function.
func genWasmExportWrapper(s *obj.LSym, appendp func(p *obj.Prog, as obj.As, args ...obj.Addr) *obj.Prog) {
	we := s.Func().WasmExport
	we.CreateAuxSym()
	p := s.Func().Text
	framesize := p.To.Offset
	for p.Link != nil && p.Link.As == obj.AFUNCDATA {
		p = p.Link
	}
	if p.Link != nil {
		panic("wrapper functions for WASM export should not have a body")
	}

	// Detect and error out if called before runtime initialization
	// SP is 0 if not initialized
	p = appendp(p, AGet, regAddr(REG_SP))
	p = appendp(p, AI32Eqz)
	p = appendp(p, AIf)
	p = appendp(p, ACall, obj.Addr{Type: obj.TYPE_MEM, Name: obj.NAME_EXTERN, Sym: runtimeNotInitialized})
	p = appendp(p, AEnd)

	// cabi_realloc may be called by the host while lowering values into
	// linear memory, where Component Model threads cannot switch; it always
	// runs on the current stack, like a callback from within an import.
	coopEntry := stackful() && !strings.HasPrefix(s.Name, "cabi_realloc") // the wrapper is named after the export
	if coopEntry {
		// A call while Go is idle (runtime.coopActive == 0) is a new call
		// from the host on a new thread; a call while Go is running is a
		// callback from within an import. See runtime/coop_wasip1.go.
		active := obj.Addr{Type: obj.TYPE_ADDR, Name: obj.NAME_EXTERN, Sym: coopActive}
		p = appendp(p, AI64Const, active)
		p = appendp(p, AI32WrapI64)
		p = appendp(p, AI64Load, constAddr(0))
		p = appendp(p, AI64Eqz)
		p = appendp(p, AIf)
		// In c-shared mode, the first such call runs runtime.main
		// (package initialization), which _initialize could not.
		p = appendp(p, AI64Const, obj.Addr{Type: obj.TYPE_ADDR, Name: obj.NAME_EXTERN, Sym: coopMainStarted})
		p = appendp(p, AI32WrapI64)
		p = appendp(p, AI64Load, constAddr(0))
		p = appendp(p, AI64Eqz)
		p = appendp(p, AIf)
		p = appendp(p, ACall, obj.Addr{Type: obj.TYPE_MEM, Name: obj.NAME_EXTERN, Sym: coopStartMain})
		p = appendp(p, AEnd)
		p = appendp(p, ACALLNORESUME, obj.Addr{Type: obj.TYPE_MEM, Name: obj.NAME_EXTERN, Sym: coopExportEnter})
		// Such a call has no Go caller: the frames of runtime.main were
		// discarded when it paused, so the slot above this frame holds
		// no return address. Store one of runtime.goexit, a top frame,
		// so that tracebacks of the goroutine stop at this frame.
		p = appendp(p, AGet, regAddr(REG_SP))
		p = appendp(p, AI64Const, obj.Addr{Type: obj.TYPE_ADDR, Name: obj.NAME_EXTERN, Sym: goexit, Offset: 1})
		p = appendp(p, AI64Store, constAddr(0))
		p = appendp(p, AEnd)
		p = coopAddActive(p, appendp, active, 1)
	}

	// Now that we've checked the SP, generate the prologue
	if framesize > 0 {
		p = appendp(p, AGet, regAddr(REG_SP))
		p = appendp(p, AI32Const, constAddr(framesize))
		p = appendp(p, AI32Sub)
		p = appendp(p, ASet, regAddr(REG_SP))
		p.Spadj = int32(framesize)
	}

	// Store args
	for i, f := range we.Params {
		p = appendp(p, AGet, regAddr(REG_SP))
		p = appendp(p, AGet, regAddr(REG_R0+int16(i)))
		switch f.Type {
		case obj.WasmI32:
			p = appendp(p, AI32Store, constAddr(f.Offset))
		case obj.WasmI64:
			p = appendp(p, AI64Store, constAddr(f.Offset))
		case obj.WasmF32:
			p = appendp(p, AF32Store, constAddr(f.Offset))
		case obj.WasmF64:
			p = appendp(p, AF64Store, constAddr(f.Offset))
		case obj.WasmV128:
			p = appendp(p, AV128Store, constAddr(f.Offset))
		case obj.WasmPtr:
			p = appendp(p, AI64ExtendI32U)
			p = appendp(p, AI64Store, constAddr(f.Offset))
		case obj.WasmBool:
			p = appendp(p, AI32Store8, constAddr(f.Offset))
		default:
			panic("bad param type")
		}
	}

	// Call the Go function.
	// XXX maybe use ACALL and let later phase expand? But we don't use PC_B. Maybe we should?
	// Go calling convention expects we push a return PC before call.
	// SP -= 8
	p = appendp(p, AGet, regAddr(REG_SP))
	p = appendp(p, AI32Const, constAddr(8))
	p = appendp(p, AI32Sub)
	p = appendp(p, ASet, regAddr(REG_SP))
	// write return address to Go stack
	p = appendp(p, AGet, regAddr(REG_SP))
	retAddr := obj.Addr{
		Type:   obj.TYPE_ADDR,
		Name:   obj.NAME_EXTERN,
		Sym:    s, // PC_F
		Offset: 1, // PC_B=1, past the prologue, so we have the right SP delta
	}
	if framesize == 0 {
		// Frameless function, no prologue.
		retAddr.Offset = 0
	} else if coopEntry {
		// The call of coopStartMain above starts another block, so the
		// prologue is in PC_B=2.
		retAddr.Offset = 2
	}
	p = appendp(p, AI64Const, retAddr)
	p = appendp(p, AI64Store, constAddr(0))
	// Set PC_B parameter to function entry
	if !spParam() {
		p = appendp(p, AI32Const, constAddr(0))
	}
	p = appendp(p, ACall, obj.Addr{Type: obj.TYPE_MEM, Name: obj.NAME_EXTERN, Sym: we.WrappedSym})
	// Return value is on the top of the stack, indicating whether to unwind the Wasm stack.
	// In the unwinding case, we call wasm_pc_f_loop_export to handle stack switch and rewinding,
	// until a normal return (non-unwinding) back to this function.
	p = appendp(p, AIf)
	p = appendp(p, AI64Const, retAddr)
	p = appendp(p, AI64Const, constAddr(16))
	p = appendp(p, AI64ShrU)
	p = appendp(p, AI32WrapI64)
	p = appendp(p, ACall, obj.Addr{Type: obj.TYPE_MEM, Name: obj.NAME_EXTERN, Sym: wasm_pc_f_loop_export})
	p = appendp(p, AEnd)

	if coopEntry {
		p = coopAddActive(p, appendp, obj.Addr{Type: obj.TYPE_ADDR, Name: obj.NAME_EXTERN, Sym: coopActive}, -1)
	}

	// Load result
	if len(we.Results) > 1 {
		panic("invalid results type")
	} else if len(we.Results) == 1 {
		p = appendp(p, AGet, regAddr(REG_SP))
		f := we.Results[0]
		switch f.Type {
		case obj.WasmI32:
			p = appendp(p, AI32Load, constAddr(f.Offset))
		case obj.WasmI64:
			p = appendp(p, AI64Load, constAddr(f.Offset))
		case obj.WasmF32:
			p = appendp(p, AF32Load, constAddr(f.Offset))
		case obj.WasmF64:
			p = appendp(p, AF64Load, constAddr(f.Offset))
		case obj.WasmV128:
			p = appendp(p, AV128Load, constAddr(f.Offset))
		case obj.WasmPtr:
			p = appendp(p, AI32Load, constAddr(f.Offset))
		case obj.WasmBool:
			p = appendp(p, AI32Load8U, constAddr(f.Offset))
		default:
			panic("bad result type")
		}
	}

	// Epilogue. Cannot use ARET as we don't follow Go calling convention.
	if framesize > 0 {
		// SP += framesize
		p = appendp(p, AGet, regAddr(REG_SP))
		p = appendp(p, AI32Const, constAddr(framesize))
		p = appendp(p, AI32Add)
		p = appendp(p, ASet, regAddr(REG_SP))
	}
	p = appendp(p, AReturn)
}

// noReturn reports whether the call p is immediately followed by an
// unreachable instruction, as the compiler emits after calls that do not
// return with GOEXPERIMENT=wasmstackful. Its results are then left on the
// (polymorphic) operand stack: popping them into locals would make the wasm
// compiler merge them with the locals' values on the other paths, which can
// slow down loops that contain panic checks.
func noReturn(p *obj.Prog) bool {
	for q := p.Link; q != nil; q = q.Link {
		switch q.As {
		case obj.ANOP, obj.APCDATA, obj.AFUNCDATA:
			continue
		case obj.AUNDEF:
			return true
		}
		return false
	}
	return false
}

// spillRegArgs appends the instructions that spill or unspill s's register
// arguments (GOEXPERIMENT=wasmregabi) after p, giving them p's PC.
func spillRegArgs(s *obj.LSym, p *obj.Prog, newprog obj.ProgAlloc, gen func(*obj.FuncInfo, *obj.Prog, obj.ProgAlloc) *obj.Prog) *obj.Prog {
	last := gen(s.Func(), p, newprog)
	for q := p.Link; q != last.Link; q = q.Link {
		q.Pc = p.Pc
		q.Pos = p.Pos
	}
	return last
}

// coopAddActive emits runtime.coopActive += delta (GOEXPERIMENT=wasmstackful).
func coopAddActive(p *obj.Prog, appendp func(p *obj.Prog, as obj.As, args ...obj.Addr) *obj.Prog, active obj.Addr, delta int64) *obj.Prog {
	p = appendp(p, AI64Const, active)
	p = appendp(p, AI32WrapI64)
	p = appendp(p, AI64Const, active)
	p = appendp(p, AI32WrapI64)
	p = appendp(p, AI64Load, constAddr(0))
	p = appendp(p, AI64Const, constAddr(delta))
	p = appendp(p, AI64Add)
	return appendp(p, AI64Store, constAddr(0))
}

func constAddr(value int64) obj.Addr {
	return obj.Addr{Type: obj.TYPE_CONST, Offset: value}
}

func regAddr(reg int16) obj.Addr {
	return obj.Addr{Type: obj.TYPE_REG, Reg: reg}
}

// Most of the Go functions have a single parameter (PC_B) in the
// Wasm ABI. This is a list of exceptions.
var notUsePC_B = map[string]bool{
	"_rt0_wasm_js":                true,
	"_rt0_wasm_wasip1":            true,
	"_rt0_wasm_wasip1_lib":        true,
	"wasm_export_run":             true,
	"wasm_export_resume":          true,
	"wasm_export_getsp":           true,
	"wasm_pc_f_loop":              true,
	"wasm_pc_f_loop_export":       true,
	"gcWriteBarrier":              true,
	"runtime.gcWriteBarrier1":     true,
	"runtime.gcWriteBarrier2":     true,
	"runtime.gcWriteBarrier3":     true,
	"runtime.gcWriteBarrier4":     true,
	"runtime.gcWriteBarrier5":     true,
	"runtime.gcWriteBarrier6":     true,
	"runtime.gcWriteBarrier7":     true,
	"runtime.gcWriteBarrier8":     true,
	"runtime.notInitialized":      true,
	"runtime.wasmCoopStartMain":   true,
	"runtime.wasmCoopThreadStart": true,
	"runtime.wasmCoopCabiRealloc": true,
	"runtime.wasmDiv":             true,
	"runtime.wasmTruncS":          true,
	"runtime.wasmTruncU":          true,
	"cmpbody":                     true,
	"memeqbody":                   true,
	"memcmp":                      true,
	"memchr":                      true,
}

func assemble(ctxt *obj.Link, s *obj.LSym, newprog obj.ProgAlloc) {
	type regVar struct {
		global bool
		index  uint64
	}

	type varDecl struct {
		count uint64
		typ   valueType
	}

	hasLocalSP := false
	regVars := [MAXREG - MINREG]*regVar{
		REG_SP - MINREG:    {true, 0},
		REG_CTXT - MINREG:  {true, 1},
		REG_g - MINREG:     {true, 2},
		REG_RET0 - MINREG:  {true, 3},
		REG_RET1 - MINREG:  {true, 4},
		REG_RET2 - MINREG:  {true, 5},
		REG_RET3 - MINREG:  {true, 6},
		REG_PAUSE - MINREG: {true, 7},
	}
	var varDecls []*varDecl
	useAssemblyRegMap := func() {
		for i := int16(0); i < 16; i++ {
			regVars[REG_R0+i-MINREG] = &regVar{false, uint64(i)}
		}
	}

	// Function starts with declaration of locals: numbers and types.
	// Some functions use a special calling convention.
	switch s.Name {
	case "_rt0_wasm_js", "_rt0_wasm_wasip1", "_rt0_wasm_wasip1_lib",
		"wasm_export_run", "wasm_export_resume", "wasm_export_getsp",
		"wasm_pc_f_loop", "runtime.wasmDiv", "runtime.wasmTruncS", "runtime.wasmTruncU", "memeqbody":
		varDecls = []*varDecl{}
		useAssemblyRegMap()
	case "wasm_pc_f_loop_export":
		varDecls = []*varDecl{{count: 2, typ: i32}}
		useAssemblyRegMap()
	case "runtime.wasmCoopThreadStart":
		// (param i32) is local 0 (R0); R1 and R2 are i64 scratch locals.
		varDecls = []*varDecl{{count: 2, typ: i64}}
		useAssemblyRegMap()
	case "runtime.wasmCoopStartMain":
		varDecls = []*varDecl{}
		useAssemblyRegMap()
	case "runtime.wasmCoopCabiRealloc":
		// Four i32 params are locals 0-3 (R0-R3); R4-R9 are i64 scratch locals.
		varDecls = []*varDecl{{count: 6, typ: i64}}
		useAssemblyRegMap()
	case "memchr", "memcmp":
		varDecls = []*varDecl{{count: 2, typ: i32}}
		useAssemblyRegMap()
	case "cmpbody":
		varDecls = []*varDecl{{count: 2, typ: i64}}
		useAssemblyRegMap()
	case "gcWriteBarrier":
		varDecls = []*varDecl{{count: 5, typ: i64}}
		useAssemblyRegMap()
	case "runtime.gcWriteBarrier1",
		"runtime.gcWriteBarrier2",
		"runtime.gcWriteBarrier3",
		"runtime.gcWriteBarrier4",
		"runtime.gcWriteBarrier5",
		"runtime.gcWriteBarrier6",
		"runtime.gcWriteBarrier7",
		"runtime.gcWriteBarrier8",
		"runtime.notInitialized":
		// no locals
		useAssemblyRegMap()
	default:
		if s.Func().WasmExport != nil {
			// no local SP, not following Go calling convention
			useAssemblyRegMap()
			break
		}

		hasLocalSP = true

		var regUsed [MAXREG - MINREG]bool
		for p := s.Func().Text; p != nil; p = p.Link {
			if p.From.Reg != 0 {
				regUsed[p.From.Reg-MINREG] = true
			}
			if p.To.Reg != 0 {
				regUsed[p.To.Reg-MINREG] = true
			}
		}

		if spParam() {
			// See spParam and regABI: the parameters are SP (local 0)
			// and, with GOEXPERIMENT=wasmregabi, R0-R2. PC_B is the
			// first local, 0 on entry; the other registers follow.
			index := uint64(1)
			if regABI() {
				for reg := int16(REG_R0); reg < REG_R0+regABIArgs; reg++ {
					regVars[reg-MINREG] = &regVar{false, index}
					index++
				}
			}
			regVars[REG_PC_B-MINREG] = &regVar{false, index}
			index++
			varDecls = append(varDecls, &varDecl{count: 1, typ: i32})
			var lastDecl *varDecl
			for reg := int16(REG_R0); reg <= REG_FDEPTH; reg++ {
				if !regUsed[reg-MINREG] || regVars[reg-MINREG] != nil {
					continue
				}
				t := regType(reg)
				if lastDecl == nil || lastDecl.typ != t {
					lastDecl = &varDecl{count: 0, typ: t}
					varDecls = append(varDecls, lastDecl)
				}
				lastDecl.count++
				regVars[reg-MINREG] = &regVar{false, index}
				index++
			}
			break
		}

		// Normal calling convention: PC_B as WebAssembly parameter. First local variable is local SP cache.
		regVars[REG_PC_B-MINREG] = &regVar{false, 0}

		regs := []int16{REG_SP}
		for reg := int16(REG_R0); reg <= REG_FDEPTH; reg++ {
			if regUsed[reg-MINREG] {
				regs = append(regs, reg)
			}
		}

		var lastDecl *varDecl
		for i, reg := range regs {
			t := regType(reg)
			if lastDecl == nil || lastDecl.typ != t {
				lastDecl = &varDecl{
					count: 0,
					typ:   t,
				}
				varDecls = append(varDecls, lastDecl)
			}
			lastDecl.count++
			if reg != REG_SP {
				regVars[reg-MINREG] = &regVar{false, 1 + uint64(i)}
			}
		}
	}

	w := new(bytes.Buffer)

	writeUleb128(w, uint64(len(varDecls)))
	for _, decl := range varDecls {
		writeUleb128(w, decl.count)
		w.WriteByte(byte(decl.typ))
	}

	if hasLocalSP && !spParam() {
		// Copy SP from its global variable into a local variable. Accessing a local variable is more efficient.
		updateLocalSP(w)
	}

	// With GOEXPERIMENT=wasmspparam, emitSP pushes SP, using the local SP in
	// functions that follow the Go ABI and the SP global otherwise.
	getSP := AGlobalGet
	setSP := AGlobalSet
	spIndex := uint64(0)
	localSP := uint64(1) // the local SP cache, or with wasmspparam the SP parameter
	if spParam() {
		localSP = 0
	}
	if hasLocalSP {
		getSP, setSP, spIndex = ALocalGet, ALocalSet, localSP
	}
	emitSP := func() {
		writeOpcode(w, getSP)
		writeUleb128(w, spIndex)
	}
	// takeSP handles the result of a call to a function that follows the Go
	// ABI, SP|unwind: it stores SP and, unless the call is marked dropUnwind,
	// leaves the unwind flag.
	takeSP := func(p *obj.Prog) {
		if p.Mark&dropUnwind != 0 {
			writeOpcode(w, AI32Const)
			writeSleb128(w, -2)
			writeOpcode(w, AI32And)
			writeOpcode(w, setSP)
			writeUleb128(w, spIndex)
			return
		}
		writeOpcode(w, setSP)
		writeUleb128(w, spIndex)
		emitSP()
		writeOpcode(w, AI32Const)
		writeSleb128(w, 1)
		writeOpcode(w, AI32And)
		emitSP()
		writeOpcode(w, AI32Const)
		writeSleb128(w, -2)
		writeOpcode(w, AI32And)
		writeOpcode(w, setSP)
		writeUleb128(w, spIndex)
	}
	// orSP combines an unwind flag with SP for the result of a function that
	// follows the Go ABI.
	orSP := func() {
		emitSP()
		writeOpcode(w, AI32Or)
	}
	// With GOEXPERIMENT=wasmregabi, pushRegs pushes the argument or result
	// registers R0-R2 (locals 1-3) of a function that follows the Go ABI,
	// and zeros otherwise, and popRegs pops them (after a call).
	pushRegs := func() {
		if !regABI() {
			return
		}
		for i := range uint64(regABIArgs) {
			if hasLocalSP {
				writeOpcode(w, ALocalGet)
				writeUleb128(w, 1+i)
			} else {
				writeOpcode(w, AI64Const)
				writeSleb128(w, 0)
			}
		}
	}
	popRegs := func() {
		if !regABI() {
			return
		}
		for i := uint64(regABIArgs); i > 0; i-- {
			if hasLocalSP {
				writeOpcode(w, ALocalSet)
				writeUleb128(w, i)
			} else {
				writeOpcode(w, ADrop)
			}
		}
	}
	syncGlobalSP := func() {
		if hasLocalSP {
			writeOpcode(w, ALocalGet)
			writeUleb128(w, localSP)
			writeOpcode(w, AGlobalSet)
			writeUleb128(w, 0)
		}
	}

	for p := s.Func().Text; p != nil; p = p.Link {
		if spParam() {
			switch p.As {
			case ACall:
				if p.To.Type != obj.TYPE_MEM {
					break
				}
				if p.Mark&WasmImport == 0 && !notUsePC_B[p.To.Sym.Name] {
					// The callee follows the Go ABI: pass SP, take its SP.
					emitSP()
					pushRegs()
					writeOpcode(w, ACall)
					s.AddRel(ctxt, obj.Reloc{
						Type: objabi.R_CALL,
						Off:  int32(w.Len()),
						Siz:  1, // actually variable sized
						Sym:  p.To.Sym,
					})
					if !noReturn(p) {
						popRegs()
						takeSP(p)
					}
					continue
				}
				if !spFree[p.To.Sym.Name] {
					syncGlobalSP()
				}
				// The callee restores SP (if it changes it at all), and a
				// host import may let other threads run Go code, which uses
				// the SP global for their own stacks, so keep the local SP.
				writeOpcode(w, ACall)
				typ := objabi.R_CALL
				if p.Mark&WasmImport != 0 {
					typ = objabi.R_WASMIMPORT
				}
				s.AddRel(ctxt, obj.Reloc{
					Type: typ,
					Off:  int32(w.Len()),
					Siz:  1, // actually variable sized
					Sym:  p.To.Sym,
				})
				continue

			case ACallIndirect:
				// Callers push SP (and, with GOEXPERIMENT=wasmregabi, the
				// register arguments) below the function index.
				writeOpcode(w, ACallIndirect)
				writeUleb128(w, uint64(p.To.Offset))
				w.WriteByte(0x00) // reserved value
				if !noReturn(p) {
					popRegs()
					takeSP(p)
				}
				continue

			case AReturn:
				if hasLocalSP {
					orSP()
					pushRegs()
				}

			case AThrow:
				if p.Mark&rethrow == 0 && p.From.Offset != coopResumeKeepSP {
					syncGlobalSP()
				}
			}
		}

		switch p.As {
		case AGet:
			if p.From.Type != obj.TYPE_REG {
				panic("bad Get: argument is not a register")
			}
			reg := p.From.Reg
			v := regVars[reg-MINREG]
			if v == nil {
				panic("bad Get: invalid register")
			}
			if reg == REG_SP && hasLocalSP {
				writeOpcode(w, ALocalGet)
				writeUleb128(w, localSP)
				continue
			}
			if v.global {
				writeOpcode(w, AGlobalGet)
			} else {
				writeOpcode(w, ALocalGet)
			}
			writeUleb128(w, v.index)
			continue

		case ASet:
			if p.To.Type != obj.TYPE_REG {
				panic("bad Set: argument is not a register")
			}
			reg := p.To.Reg
			v := regVars[reg-MINREG]
			if v == nil {
				panic("bad Set: invalid register")
			}
			if reg == REG_SP && hasLocalSP {
				if spParam() {
					writeOpcode(w, ALocalSet)
					writeUleb128(w, localSP) // see spParam
					continue
				}
				writeOpcode(w, ALocalTee)
				writeUleb128(w, localSP)
			}
			if v.global {
				writeOpcode(w, AGlobalSet)
			} else {
				if p.Link.As == AGet && p.Link.From.Reg == reg {
					writeOpcode(w, ALocalTee)
					p = p.Link
				} else {
					writeOpcode(w, ALocalSet)
				}
			}
			writeUleb128(w, v.index)
			continue

		case ATee:
			if p.To.Type != obj.TYPE_REG {
				panic("bad Tee: argument is not a register")
			}
			reg := p.To.Reg
			v := regVars[reg-MINREG]
			if v == nil {
				panic("bad Tee: invalid register")
			}
			writeOpcode(w, ALocalTee)
			writeUleb128(w, v.index)
			continue

		case ANot:
			writeOpcode(w, AI32Eqz)
			continue

		case obj.AUNDEF:
			writeOpcode(w, AUnreachable)
			continue

		case ATryTable:
			w.WriteByte(0x1F) // try_table
			w.WriteByte(0x40) // empty block type
			writeUleb128(w, 1)
			w.WriteByte(0x00) // catch
			writeUleb128(w, uint64(p.From.Offset))
			writeUleb128(w, 0) // label: the block enclosing the try_table
			continue

		case AThrow:
			w.WriteByte(0x08) // throw
			tag := uint64(p.From.Offset)
			if tag == coopResumeKeepSP {
				tag = coopResumeTag
			}
			writeUleb128(w, tag)
			continue

		case obj.ANOP, obj.ATEXT, obj.AFUNCDATA, obj.APCDATA:
			// ignore
			continue

		case AV128Const:
			writeOpcode(w, AV128Const)
			// Despite what the spec implies, this is the format of a 128-bit constant.
			writeLE64(w, p.From.Offset)
			writeLE64(w, p.To.Offset)
			continue
		}

		writeOpcode(w, p.As)

		switch p.As {
		case ABlock, ALoop, AIf:
			if p.From.Offset != 0 {
				// block type, rarely used, e.g. for code compiled with emscripten
				w.WriteByte(0x80 - byte(p.From.Offset))
				continue
			}
			w.WriteByte(0x40)

		case ABr, ABrIf:
			if p.To.Type != obj.TYPE_CONST {
				panic("bad Br/BrIf")
			}
			writeUleb128(w, uint64(p.To.Offset))

		case ABrTable:
			idxs := p.To.Val.([]uint64)
			writeUleb128(w, uint64(len(idxs)-1))
			for _, idx := range idxs {
				writeUleb128(w, idx)
			}

		case ACall:
			switch p.To.Type {
			case obj.TYPE_CONST:
				writeUleb128(w, uint64(p.To.Offset))

			case obj.TYPE_MEM:
				if p.To.Name != obj.NAME_EXTERN && p.To.Name != obj.NAME_STATIC {
					fmt.Println(p.To)
					panic("bad name for Call")
				}
				typ := objabi.R_CALL
				if p.Mark&WasmImport != 0 {
					typ = objabi.R_WASMIMPORT
				}
				s.AddRel(ctxt, obj.Reloc{
					Type: typ,
					Off:  int32(w.Len()),
					Siz:  1, // actually variable sized
					Sym:  p.To.Sym,
				})
				if hasLocalSP {
					// The stack may have moved, which changes SP. Update the local SP variable.
					updateLocalSP(w)
				}

			default:
				panic("bad type for Call")
			}

		case AI8x16ExtractLaneS,
			AI8x16ExtractLaneU,
			AI8x16ReplaceLane,
			AI16x8ExtractLaneS,
			AI16x8ExtractLaneU,
			AI16x8ReplaceLane,
			AI32x4ExtractLane,
			AI32x4ReplaceLane,
			AI64x2ExtractLane,
			AI64x2ReplaceLane,
			AF32x4ExtractLane,
			AF32x4ReplaceLane,
			AF64x2ExtractLane,
			AF64x2ReplaceLane:
			writeUleb128(w, uint64(p.To.Offset))

		case ACallIndirect:
			writeUleb128(w, uint64(p.To.Offset))
			w.WriteByte(0x00) // reserved value
			if hasLocalSP {
				// The stack may have moved, which changes SP. Update the local SP variable.
				updateLocalSP(w)
			}

		case AI32Const, AI64Const:
			if p.From.Name == obj.NAME_EXTERN {
				s.AddRel(ctxt, obj.Reloc{
					Type: objabi.R_ADDR,
					Off:  int32(w.Len()),
					Siz:  1, // actually variable sized
					Sym:  p.From.Sym,
					Add:  p.From.Offset,
				})
				break
			}
			writeSleb128(w, p.From.Offset)

		case AF32Const:
			b := make([]byte, 4)
			binary.LittleEndian.PutUint32(b, math.Float32bits(float32(p.From.Val.(float64))))
			w.Write(b)

		case AF64Const:
			b := make([]byte, 8)
			binary.LittleEndian.PutUint64(b, math.Float64bits(p.From.Val.(float64)))
			w.Write(b)

		case AI32Load, AI64Load, AF32Load, AF64Load, AI32Load8S, AI32Load8U, AI32Load16S, AI32Load16U,
			AI64Load8S, AI64Load8U, AI64Load16S, AI64Load16U, AI64Load32S, AI64Load32U, AV128Load:
			if p.From.Offset < 0 {
				panic("negative offset for *Load")
			}
			if p.From.Type != obj.TYPE_CONST {
				panic(fmt.Errorf("bad type for *Load, wanted TYPE_CONST, got %v", p.From.Type))
			}
			if p.From.Offset > math.MaxUint32 {
				ctxt.Diag("bad offset in %v", p)
			}
			writeUleb128(w, align(p.As))
			writeUleb128(w, uint64(p.From.Offset))

		case AI32Store, AI64Store, AF32Store, AF64Store, AI32Store8, AI32Store16, AI64Store8, AI64Store16, AI64Store32, AV128Store:
			if p.To.Offset < 0 {
				panic("negative offset")
			}
			if p.To.Offset > math.MaxUint32 {
				ctxt.Diag("bad offset in %v", p)
			}
			writeUleb128(w, align(p.As))
			writeUleb128(w, uint64(p.To.Offset))

		case ACurrentMemory, AGrowMemory, AMemoryFill:
			w.WriteByte(0x00)

		case AMemoryCopy:
			w.WriteByte(0x00)
			w.WriteByte(0x00)

		}
	}

	if hasLocalSP && spParam() {
		// The function body leaves the unwind flag; combine it with SP.
		// (If the end is unreachable, the operand stack is polymorphic.)
		orSP()
		pushRegs()
	}
	w.WriteByte(0x0b) // end

	s.P = w.Bytes()
}

func updateLocalSP(w *bytes.Buffer) {
	writeOpcode(w, AGlobalGet)
	writeUleb128(w, 0) // global SP
	writeOpcode(w, ALocalSet)
	writeUleb128(w, 1) // local SP
}

func writeOpcode(w *bytes.Buffer, as obj.As) {
	switch {
	case as < AUnreachable:
		panic(fmt.Sprintf("unexpected assembler op: %s", as))
	case as < AEnd:
		w.WriteByte(byte(as - AUnreachable + 0x00))
	case as < ADrop:
		w.WriteByte(byte(as - AEnd + 0x0B))
	case as < ALocalGet:
		w.WriteByte(byte(as - ADrop + 0x1A))
	case as < AI32Load:
		w.WriteByte(byte(as - ALocalGet + 0x20))
	case as < AI32TruncSatF32S:
		w.WriteByte(byte(as - AI32Load + 0x28))
	case as < AV128Load:
		w.WriteByte(0xFC)
		w.WriteByte(byte(as - AI32TruncSatF32S + 0x00))
	case as < AI16x8Abs: // [AV128Load, AI16x8Abs)
		w.WriteByte(0xFD)
		writeUleb128(w, uint64(as-AV128Load+0x00))
	case as < AI8x16RelaxedSwizzle: // [AI16x8Abs, AI8x16RelaxedSwizzle)
		w.WriteByte(0xFD)
		writeUleb128(w, uint64(as-AI16x8Abs+0x80))
		w.WriteByte(0x01)
	case as <= AI32x4RelaxedDotI8x16I7x16AddS: // [AI8x16RelaxedSwizzle, AI32x4RelaxedDotI8x16I7x16AddS]
		w.WriteByte(0xFD)
		writeUleb128(w, uint64(as-AI8x16RelaxedSwizzle+0x80))
		w.WriteByte(0x02)

	default:
		panic(fmt.Sprintf("unexpected assembler op: %s", as))
	}
}

type valueType byte

const (
	i32  valueType = 0x7F
	i64  valueType = 0x7E
	f32  valueType = 0x7D
	f64  valueType = 0x7C
	v128 valueType = 0x7b
)

func regType(reg int16) valueType {
	switch {
	case reg == REG_SP:
		return i32
	case reg >= REG_R0 && reg <= REG_R15:
		return i64
	case reg >= REG_F0 && reg <= REG_F15:
		return f32
	case reg >= REG_F16 && reg <= REG_F31:
		return f64
	case reg >= REG_V0 && reg <= REG_V15:
		return v128
	case reg == REG_FDEPTH:
		return i64
	default:
		panic("invalid register")
	}
}

func align(as obj.As) uint64 {
	switch as {
	case AI32Load8S, AI32Load8U, AI64Load8S, AI64Load8U, AI32Store8, AI64Store8:
		return 0
	case AI32Load16S, AI32Load16U, AI64Load16S, AI64Load16U, AI32Store16, AI64Store16:
		return 1
	case AI32Load, AF32Load, AI64Load32S, AI64Load32U, AI32Store, AF32Store, AI64Store32:
		return 2
	case AI64Load, AF64Load, AI64Store, AF64Store:
		return 3
	case AV128Load, AV128Store:
		return 0 // TODO do we want more alignment
	default:
		panic("align: bad op")
	}
}

func writeUleb128(w io.ByteWriter, v uint64) {
	if v < 128 {
		w.WriteByte(uint8(v))
		return
	}
	more := true
	for more {
		c := uint8(v & 0x7f)
		v >>= 7
		more = v != 0
		if more {
			c |= 0x80
		}
		w.WriteByte(c)
	}
}

func writeSleb128(w io.ByteWriter, v int64) {
	more := true
	for more {
		c := uint8(v & 0x7f)
		s := uint8(v & 0x40)
		v >>= 7
		more = !((v == 0 && s == 0) || (v == -1 && s != 0))
		if more {
			c |= 0x80
		}
		w.WriteByte(c)
	}
}

func writeLE64(w io.ByteWriter, v int64) {
	for i := 0; i < 8; i++ {
		w.WriteByte(uint8(v & 0xff))
		v >>= 8
	}
}
