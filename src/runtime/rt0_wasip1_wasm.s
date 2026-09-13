// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

#include "go_asm.h"
#include "textflag.h"

#ifdef GOEXPERIMENT_wasmcoopthreads
#ifdef GOEXPERIMENT_wasmstackful
#ifdef GOEXPERIMENT_wasmspparam
// Functions that follow the Go ABI take SP instead of PC_B (see asm_wasm.s).
#define WASM_SP_PARAM
#endif
#endif
#endif

// GOOS=wasip1 executables export cabi_realloc with
// GOEXPERIMENT=wasmcoopthreads (see cabi_realloc_wasip1.go). GOOS=wasip3
// modules export a Go function instead (see cabi_wasip3.go).
#ifdef GOEXPERIMENT_wasmcoopthreads
#ifndef GOOS_wasip3
#define WASM_CABI_REALLOC
#endif
#endif

TEXT _rt0_wasm_wasip1(SB),NOSPLIT,$0
#ifdef WASM_CABI_REALLOC
	// Mark the cabi_realloc export as alive.
	I64Const $runtime·wasmCoopCabiRealloc(SB)
	Drop
#endif
#ifdef GOEXPERIMENT_wasmcoopthreads
#ifdef GOEXPERIMENT_wasmstackful
	// Go is running; exports called from here on are callbacks.
	MOVD $1, runtime·coopActive(SB)
#endif
#endif
	MOVD $runtime·wasmStack+(m0Stack__size-16)(SB), SP

#ifndef WASM_SP_PARAM
	I32Const $0 // entry PC_B
#endif
	Call runtime·rt0_go(SB)
	Drop
	Call wasm_pc_f_loop(SB)

	Return

TEXT _rt0_wasm_wasip1_lib(SB),NOSPLIT,$0
#ifdef GOEXPERIMENT_wasmcoopthreads
#ifdef GOEXPERIMENT_wasmstackful
	// _initialize runs at component instantiation, where Component Model
	// threads cannot switch, so only initialize the runtime here; the first
	// export call runs runtime.main (see wasmCoopStartMain).
	MOVD $1, runtime·coopActive(SB)
	MOVD $runtime·wasmStack+(m0Stack__size-16)(SB), SP
#ifndef WASM_SP_PARAM
	I32Const $0 // entry PC_B
#endif
	Call runtime·rt0_go_init(SB)
	Drop
	MOVD $0, runtime·coopActive(SB)
	Return
#endif
#endif
	Call _rt0_wasm_wasip1(SB)
	Return

#ifdef GOEXPERIMENT_wasmcoopthreads
// wasmCoopThreadStart is the start function of every Component Model thread
// created by wasmCoopPrepare. It has the wasm type (func (param i32)), as
// required by thread.new-indirect; the parameter is unused. The thread that
// created it stored the gobuf of the goroutine to run in runtime·coopBuf and
// switched here. Like gogo, load that goroutine's state and let
// wasm_pc_f_loop run it. The loop only returns if PAUSE is set, which never
// happens for GOOS=wasip1 executables.
TEXT runtime·wasmCoopThreadStart(SB),NOSPLIT,$0
	MOVD $runtime·coopBuf(SB), R1
	MOVD 0(R1), R1
	MOVD gobuf_g(R1), R2
	MOVD R2, g
	MOVD gobuf_sp(R1), SP

	// Put target PC at -8(SP), wasm_pc_f_loop will pick it up
	Get SP
	I32Const $8
	I32Sub
	I64Load gobuf_pc(R1)
	I64Store $0

	MOVD gobuf_ctxt(R1), CTXT
	// clear to help garbage collector
	MOVD $0, gobuf_sp(R1)
	MOVD $0, gobuf_ctxt(R1)

#ifdef GOEXPERIMENT_wasmstackful
	Loop
		Block
			TryTable $0
				Call wasm_pc_f_loop(SB)
				UNDEF
			End
		End
		// A PC of 0 means that this thread was asked to exit
		// (coopRetireThread).
		MOVD $runtime·coopResumePC(SB), R1
		I64Load 0(R1)
		I64Eqz
		If
			Return
		End
		// Otherwise gogo threw to resume a goroutine that no frame on this
		// thread's stack is waiting for, discarding all of them. That is
		// only valid for a goroutine starting at its entry, on a thread
		// recycled from an exited goroutine. gogo already set g, SP, CTXT
		// and the PC at -8(SP) for wasm_pc_f_loop.
		MOVD $runtime·coopResumePC(SB), R1
		I64Load 0(R1)
		I64Const $0xffff
		I64And
		I64Eqz
		Not
		If
			CALLNORESUME runtime·coopResumeLost(SB)
			UNDEF
		End
		Br $0
	End
#else
	Call wasm_pc_f_loop(SB)
#endif
	Return

#ifdef GOEXPERIMENT_wasmstackful
// wasmCoopStartMain runs runtime.main (package initialization) on the calling
// thread until it pauses, in c-shared mode. //go:wasmexport wrappers call it
// on the first top-level export call. It does not follow the Go ABI.
TEXT runtime·wasmCoopStartMain(SB),NOSPLIT,$0
	MOVD $1, runtime·coopMainStarted(SB)
	// Like rt0_go: mstart on g0's stack schedules the main goroutine, which
	// (as the first goroutine to run) is bound to this thread, so gogo
	// unwinds to here and wasm_pc_f_loop runs it until runtime.main pauses.
#ifndef WASM_SP_PARAM
	I32Const $0 // entry PC_B
#endif
	Call runtime·mstart(SB)
	Drop
	Call wasm_pc_f_loop(SB)
	Return
#endif

#endif

#ifdef WASM_CABI_REALLOC
// wasmCoopCabiRealloc is exported as cabi_realloc (see cabi_realloc_wasip1.go).
// It does not follow the Go ABI; it has the canonical ABI signature
// (param $old i32 $oldSize i32 $align i32 $newSize i32) (result i32),
// with the parameters in R0-R3, and uses R4-R9 as i64 scratch locals. It only
// allocates (the adapter never reallocates, and the host only lowers new
// values): it bumps runtime·cabiNext within a chunk of whole wasm pages,
// which it takes from runtime·bloc exactly like sbrk. It can run before
// osinit, while bloc and blocMax are still zero.
TEXT runtime·wasmCoopCabiRealloc(SB),NOSPLIT,$0
	// R8 = align-1
	Get R2
	I64ExtendI32U
	I64Const $1
	I64Sub
	Set R8

	// R9 = cabiNext rounded up to a multiple of align
	I64Const $runtime·cabiNext(SB)
	I32WrapI64
	I64Load $0
	Get R8
	I64Add
	Get R8
	I64Const $-1
	I64Xor
	I64And
	Set R9

	// R5 = newSize
	Get R3
	I64ExtendI32U
	Set R5

	// If the allocation fits in the current chunk, take it from there.
	Get R9
	Get R5
	I64Add
	I64Const $runtime·cabiEnd(SB)
	I32WrapI64
	I64Load $0
	I64LeU
	If
		I64Const $runtime·cabiNext(SB)
		I32WrapI64
		Get R9
		Get R5
		I64Add
		I64Store $0
		Get R9
		I32WrapI64
		Return
	End

	// Otherwise start a new chunk of at least 64KiB: R5 = newSize rounded up
	// to a nonzero multiple of 64KiB.
	Get R5
	I64Const $65535
	I64Add
	I64Const $-65536
	I64And
	Set R5
	Get R5
	I64Eqz
	If
		I64Const $65536
		Set R5
	End

	MOVD $runtime·bloc(SB), R6
	MOVD $runtime·blocMax(SB), R4

	// Before osinit, start the break at the current end of memory.
	Get R4
	I32WrapI64
	I64Load $0
	I64Eqz
	If
		CurrentMemory
		I64ExtendI32U
		I64Const $16
		I64Shl
		Set R7
		Get R4
		I32WrapI64
		Get R7
		I64Store $0
		Get R6
		I32WrapI64
		Get R7
		I64Store $0
	End

	// R7 = bloc
	Get R6
	I32WrapI64
	I64Load $0
	Set R7

	// if bloc+n > blocMax { grow memory; blocMax = bloc+n }
	Get R7
	Get R5
	I64Add
	Get R4
	I32WrapI64
	I64Load $0
	I64GtU
	If
		Get R7
		Get R5
		I64Add
		Get R4
		I32WrapI64
		I64Load $0
		I64Sub
		I64Const $16
		I64ShrU
		I32WrapI64
		GrowMemory
		I32Const $0
		I32LtS
		If
			I32Const $0
			Return
		End
		Get R4
		I32WrapI64
		Get R7
		Get R5
		I64Add
		I64Store $0
	End

	// bloc += n
	Get R6
	I32WrapI64
	Get R7
	Get R5
	I64Add
	I64Store $0

	// The new chunk is [bloc, bloc+n); allocate newSize bytes from its start.
	I64Const $runtime·cabiEnd(SB)
	I32WrapI64
	Get R7
	Get R5
	I64Add
	I64Store $0
	I64Const $runtime·cabiNext(SB)
	I32WrapI64
	Get R7
	Get R3
	I64ExtendI32U
	I64Add
	I64Store $0
	Get R7
	I32WrapI64
	Return
#endif
