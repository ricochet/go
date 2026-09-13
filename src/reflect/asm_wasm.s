// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

#include "textflag.h"
#include "funcdata.h"

#ifdef GOEXPERIMENT_regabiargs
// With GOEXPERIMENT=wasmregabi, the frames of each of the two functions below
// contain two locals, at offsets that are known to the runtime (as on amd64):
// retValid, a bool with a whole pointer-word reserved for it, and an
// abi.RegArgs value, the highest address local, into which the integer
// argument registers R0-R2 are spilled.
#define LOCAL_RETVALID 32
#define LOCAL_REGARGS 40

// The frame size of the functions below is
// 32 (args of callReflect) + 8 (bool + padding) + 56 (abi.RegArgs) = 96.

// makeFuncStub is the code half of the function returned by MakeFunc.
// See the comment on the declaration of makeFuncStub in makefunc.go
// for more details.
// No arg size here; runtime pulls arg map out of the func value.
TEXT ·makeFuncStub(SB),(NOSPLIT|WRAPPER),$96
	NO_LOCAL_POINTERS
	// NO_LOCAL_POINTERS is a lie. The stack map for the two locals in this
	// frame is specially handled in the runtime.
	MOVD R0, LOCAL_REGARGS(SP)
	MOVD R1, (LOCAL_REGARGS+8)(SP)
	MOVD R2, (LOCAL_REGARGS+16)(SP)
	MOVD CTXT, 16(SP) // outside of moveMakeFuncArgPtrs's arg area
	MOVD CTXT, 0(SP)
	MOVD $LOCAL_REGARGS(SP), 8(SP)
	CALL ·moveMakeFuncArgPtrs(SB)
	MOVD 16(SP), R3
	MOVD R3, 0(SP)

	Get SP
	Get SP
	I64ExtendI32U
	I64Const $argframe+0(FP)
	I64Add
	I64Store $8

	MOVB $0, LOCAL_RETVALID(SP)
	MOVD $LOCAL_RETVALID(SP), 16(SP)
	MOVD $LOCAL_REGARGS(SP), 24(SP)

	CALL ·callReflect(SB)
	MOVD LOCAL_REGARGS(SP), R0
	MOVD (LOCAL_REGARGS+8)(SP), R1
	MOVD (LOCAL_REGARGS+16)(SP), R2
	RET

// methodValueCall is the code half of the function returned by makeMethodValue.
// See the comment on the declaration of methodValueCall in makefunc.go
// for more details.
// No arg size here; runtime pulls arg map out of the func value.
TEXT ·methodValueCall(SB),(NOSPLIT|WRAPPER),$96
	NO_LOCAL_POINTERS
	// NO_LOCAL_POINTERS is a lie. The stack map for the two locals in this
	// frame is specially handled in the runtime.
	MOVD R0, LOCAL_REGARGS(SP)
	MOVD R1, (LOCAL_REGARGS+8)(SP)
	MOVD R2, (LOCAL_REGARGS+16)(SP)
	MOVD CTXT, 16(SP) // outside of moveMakeFuncArgPtrs's arg area
	MOVD CTXT, 0(SP)
	MOVD $LOCAL_REGARGS(SP), 8(SP)
	CALL ·moveMakeFuncArgPtrs(SB)
	MOVD 16(SP), R3
	MOVD R3, 0(SP)

	Get SP
	Get SP
	I64ExtendI32U
	I64Const $argframe+0(FP)
	I64Add
	I64Store $8

	MOVB $0, LOCAL_RETVALID(SP)
	MOVD $LOCAL_RETVALID(SP), 16(SP)
	MOVD $LOCAL_REGARGS(SP), 24(SP)

	CALL ·callMethod(SB)
	MOVD LOCAL_REGARGS(SP), R0
	MOVD (LOCAL_REGARGS+8)(SP), R1
	MOVD (LOCAL_REGARGS+16)(SP), R2
	RET
#else
// makeFuncStub is the code half of the function returned by MakeFunc.
// See the comment on the declaration of makeFuncStub in makefunc.go
// for more details.
// No arg size here; runtime pulls arg map out of the func value.
TEXT ·makeFuncStub(SB),(NOSPLIT|WRAPPER),$40
	NO_LOCAL_POINTERS

	MOVD CTXT, 0(SP)

	Get SP
	Get SP
	I64ExtendI32U
	I64Const $argframe+0(FP)
	I64Add
	I64Store $8

	MOVB $0, 32(SP)
	MOVD $32(SP), 16(SP)
	MOVD $0, 24(SP)

	CALL ·callReflect(SB)
	RET

// methodValueCall is the code half of the function returned by makeMethodValue.
// See the comment on the declaration of methodValueCall in makefunc.go
// for more details.
// No arg size here; runtime pulls arg map out of the func value.
TEXT ·methodValueCall(SB),(NOSPLIT|WRAPPER),$40
	NO_LOCAL_POINTERS

	MOVD CTXT, 0(SP)

	Get SP
	Get SP
	I64ExtendI32U
	I64Const $argframe+0(FP)
	I64Add
	I64Store $8

	MOVB $0, 32(SP)
	MOVD $32(SP), 16(SP)
	MOVD $0, 24(SP)

	CALL ·callMethod(SB)
	RET
#endif
