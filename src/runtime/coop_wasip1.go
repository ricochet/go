// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build wasip1 && goexperiment.wasmcoopthreads

// Goroutines on WebAssembly Component Model cooperative threads.
//
// Without this experiment, every goroutine shares the single wasm stack and
// gogo switches goroutines by unwinding that stack (each Go function returns 1)
// back to wasm_pc_f_loop, which then resumes the target goroutine one frame at
// a time from the return addresses saved on its Go stack.
//
// With this experiment, each goroutine runs on its own Component Model thread
// (created lazily by thread.new-indirect the first time the goroutine is
// executed), and gogo to a goroutine owned by another thread switches with
// thread.suspend-then-resume. The unwinding machinery is still used within a
// thread: when a suspended thread is resumed, gogo returns 1, which discards
// the scheduler frames left on that thread's wasm stack and lets the thread's
// own wasm_pc_f_loop resume the goroutine from its saved gobuf.
//
// A thread never exits. When a goroutine exits, its g (and with it the thread)
// is recycled through the gFree lists, and the next goroutine to reuse that g
// runs on the same thread.
//
// Invariant: at most one Go thread is ever not suspended, so the host never
// runs two Go threads concurrently and the runtime keeps its single-threaded
// assumptions on wasm.

package runtime

import (
	"internal/abi"
	"internal/goexperiment"
	"unsafe"
)

// gCoop is the per-goroutine Component Model thread state.
type gCoop struct {
	thread uint32 // Component Model thread index running this g, or 0 if none yet
	epoch  uint32 // coopCurEpoch of the thread that created thread (see coopNewproc)
}

//go:wasmimport $root [thread-index]
func coopThreadIndex() uint32

//go:wasmimport $root [thread-new-indirect-v0]
func coopThreadNewIndirect(fi uint32, arg uint32) uint32

//go:wasmimport $root [thread-suspend-then-resume]
func coopThreadSuspendThenResume(thread uint32) uint32

//go:wasmimport $root [thread-resume-later]
func coopThreadResumeLater(thread uint32)

// wasmCoopThreadStart is the thread start function (defined in
// rt0_wasip1_wasm.s). Its table index is passed to thread.new-indirect.
func wasmCoopThreadStart()

var (
	// coopCurThread is the index of the thread currently running Go code.
	coopCurThread uint32

	// coopSwitchFrom is the index of the thread that wasmCoopPrepare last
	// switched away from. gogo keeps it across the switch to recognize
	// whether it was resumed to run a goroutine (see coopRetireThread).
	coopSwitchFrom uint32

	// coopBuf is the *gobuf that the next thread to run Go code must apply.
	// It is a uintptr so that writing it needs no write barrier.
	coopBuf uintptr

	// coopResumePC and coopResumeSP are the PC and SP of the gobuf that gogo
	// applied last. With GOEXPERIMENT=wasmstackful, gogo then throws, and the
	// frames that catch the exception (mcall, morestack, functions with
	// deferreturn, wasmCoopThreadStart) compare them to decide whether they
	// are the frame being resumed.
	coopResumePC uintptr
	coopResumeSP uintptr

	// coopBindFirst makes the first goroutine to run (the main goroutine)
	// use the thread that entered the module (_start or _initialize)
	// instead of a new thread, so that in c-shared mode runtime.main's
	// pause returns from _initialize.
	coopBindFirst = true

	// coopActive counts nested entries into Go (GOEXPERIMENT=wasmstackful).
	// It is 1 while _start or _initialize runs, 0 while a c-shared module
	// is idle between export calls, and incremented by each //go:wasmexport
	// call. An export called while it is 0 is a new call from the host, on a
	// new thread; otherwise it is a callback from within an import (such as
	// cabi_realloc), which runs on the current goroutine's stack.
	coopActive uintptr

	// coopEntryG is the goroutine running a top-level export call whose
	// thread index is not known yet, or 0. thread.index cannot be called on
	// export entry, since the entry may be a realloc call, so it is resolved
	// lazily by wasmCoopPrepare.
	coopEntryG guintptr

	// coopMainStarted is set once runtime.main has run (c-shared mode with
	// GOEXPERIMENT=wasmstackful starts it on the first export call).
	coopMainStarted uintptr

	// Task epochs. A Component Model thread belongs to the task of the
	// thread that created it, and task-scoped built-ins such as task.return
	// act on that task. Each top-level export call is a new task and starts
	// a new epoch; each thread records the epoch it was created in (in the
	// g that owns it), and coopCurEpoch is the epoch of the running thread.
	coopCurEpoch   uint32
	coopEpochCount uint32
)

// coopNewproc gives a new goroutine a thread in the task of the goroutine
// creating it: the one it inherited with its recycled g if that belongs to
// the current task, or a new one.
func coopNewproc(gp *g) {
	if coopBindFirst {
		// The main goroutine runs on the thread that entered the module.
		return
	}
	if t := gp.coop.thread; t != 0 {
		if gp.coop.epoch == coopCurEpoch {
			return
		}
		coopRetireThread(t)
	}
	if coopCurThread == 0 {
		coopCurThread = coopThreadIndex()
	}
	fi := uint32(abi.FuncPCABI0(wasmCoopThreadStart) >> 16)
	gp.coop.thread = coopThreadNewIndirect(fi, 0)
	gp.coop.epoch = coopCurEpoch
	if coopDebug {
		print("coop: new thread ", gp.coop.thread, " epoch ", gp.coop.epoch, " for g", gp.goid, "\n")
	}
}

// coopRetireThread makes the thread of an exited goroutine, which is
// suspended in gogo, exit.
//
// It doesn't switch to the thread, which would have to yield this thread to
// the host so that the host resumes it after the other thread exits. While
// this thread yields, the host may start another export call, but Go is not
// at a point where it can take one. Instead, the thread becomes ready and
// runs whenever the host next runs threads. gogo on that thread then finds
// that no goroutine switch resumed it (coopCurThread is another thread), and
// throws out to wasmCoopThreadStart, which returns.
func coopRetireThread(t uint32) {
	if !goexperiment.WasmStackful {
		throw("coop: retiring threads requires GOEXPERIMENT=wasmstackful")
	}
	if coopDebug {
		print("coop: retire thread ", t, "\n")
	}
	coopThreadResumeLater(t)
}

// wasmCoopStartMain (rt0_wasip1_wasm.s) runs runtime.main until it pauses.
func wasmCoopStartMain()

// wasmCoopExportEnter is called by //go:wasmexport wrappers on a top-level
// call from the host. In c-shared mode such calls run on the main goroutine,
// which runtime.main paused after initialization, bound to the calling thread.
//
//go:nosplit
func wasmCoopExportEnter() {
	if coopDebug {
		print("coop: export enter g", getg().goid, "\n")
	}
	coopCurThread = 0
	coopEpochCount++
	coopCurEpoch = coopEpochCount
	gp := getg()
	gp.coop.thread = 0
	gp.coop.epoch = coopCurEpoch
	coopEntryG.set(gp)
}

// wasmCoopPrepare is called by gogo before it switches to buf. It records buf
// for whichever thread will run it and returns the index of the thread that
// gogo must switch to, or 0 if buf's goroutine runs on the current thread and
// gogo can unwind directly into it.
//
// It must not unwind the wasm stack: gogo calls it with CALLNORESUME.
//
//go:nosplit
func wasmCoopPrepare(buf *gobuf) uint32 {
	if coopCurThread == 0 {
		coopCurThread = coopThreadIndex()
	}
	if entry := coopEntryG.ptr(); entry != nil {
		// The goroutine that entered on this thread is being switched
		// away from (or to); from now on it lives on this thread.
		entry.coop.thread = coopCurThread
		coopEntryG = 0
	}
	coopBuf = uintptr(unsafe.Pointer(buf))

	gp := buf.g.ptr()
	t := gp.coop.thread
	if t == 0 && coopBindFirst {
		coopBindFirst = false
		t = coopCurThread
		gp.coop.thread = t
		gp.coop.epoch = coopCurEpoch
	}
	coopCurEpoch = gp.coop.epoch
	if coopDebug {
		print("coop: gogo g", gp.goid, " thread=", t, " cur=", coopCurThread, " pc=", hex(buf.pc), " sp=", hex(buf.sp), "\n")
	}
	if t == coopCurThread {
		return 0
	}
	if t == 0 {
		// The table index of a function is its PC_F (see cmd/link/internal/wasm).
		fi := uint32(abi.FuncPCABI0(wasmCoopThreadStart) >> 16)
		t = coopThreadNewIndirect(fi, 0)
		gp.coop.thread = t
		if coopDebug {
			print("coop: new thread ", t, " fi=", fi, " for g", gp.goid, "\n")
		}
	}
	coopSwitchFrom = coopCurThread
	coopCurThread = t
	return t
}

// coopDebug enables tracing of goroutine-to-thread switches.
const coopDebug = false

// coopResumeLost is called by wasmCoopThreadStart when a resume exception
// reached it that no frame claimed.
//
//go:nosplit
func coopResumeLost() {
	print("coop: no frame resumed pc=", hex(coopResumePC), " sp=", hex(coopResumeSP), " g", getg().goid, " depth=", hex(getg().stack.hi-coopResumeSP), "\n")
}
