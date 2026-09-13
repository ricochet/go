// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build wasip3

package runtime

// Async exports of GOOS=wasip3 c-shared modules (reactors).
//
// A //go:wasmexport function named [async]name, or [async]interface#name, is
// an async function of the component's world (a WIT async func). It's an
// ordinary Go function, which may block, and which the host may call again
// while an earlier call blocks. The compiler exports it with the Component
// Model's callback ABI (see cmd/compile/internal/ssagen.GenWasmAsyncExports):
//
//   - [async-lift]name starts a task for the call (wasip3AsyncExportStart),
//     starts a goroutine that calls the function and passes its result to
//     task.return (and then calls wasip3AsyncExportReturned), and runs the
//     task (wasip3AsyncExportCallback).
//
//   - [callback][async-lift]name runs the task after the host delivered an
//     event for it (wasip3AsyncExportCallback).
//
// Tasks have waitable sets, as tasks of Component Model bindings do (see
// task_wasip3.go). Running a task delivers the host's event, runs goroutines
// until none can run, and then returns to the host, which calls the task back
// when an event arrives on its set. Go only runs during these calls, so the
// host may start other calls in between.

//go:wasmimport $root [context-get-0]
func p3ContextGet() uint32

//go:wasmimport $root [context-set-0]
func p3ContextSet(value uint32)

//go:wasmimport $root [waitable-set-drop]
func p3WaitableSetDrop(set uint32)

// Canonical ABI callback codes.
const (
	p3CallbackExit  = 0
	p3CallbackYield = 1
	p3CallbackWait  = 2
)

// p3IdleWaiter is the goroutine running a task, which waits until no
// goroutine can run (see wasip3BeforeIdle).
var p3IdleWaiter guintptr

// wasip3AsyncExportStart starts a task for a call of an async export, and
// returns its waitable set, which identifies it.
func wasip3AsyncExportStart() uint32 {
	set := wasip3PollNewSet()
	// Goroutines belong to the task that is current when they start.
	wasip3PollSetCurrent(set)
	return set
}

// wasip3AsyncExportReturned records that the goroutine of the task with
// waitable set set passed its export's result to task.return.
func wasip3AsyncExportReturned(set uint32) {
	for i := range p3Tasks.tasks {
		if t := &p3Tasks.tasks[i]; t.set == set {
			t.returned = true
		}
	}
	// This may happen while another task runs; make sure the host calls this
	// task back, so that it can exit.
	wasip3PollWake(set)
}

// wasip3AsyncExportCallback runs the task with waitable set set, or if set is
// 0, the task whose callback the host called, after delivering the host's
// event, until no goroutine can run. It returns the callback code for the
// host.
func wasip3AsyncExportCallback(set, event, handle, payload uint32) uint32 {
	if set == 0 {
		set = p3ContextGet()
		p3ContextSet(0)
	}
	wasip3PollSetCurrent(set)
	if event != p3EventNone {
		wasip3PollDeliver(event, handle, payload)
	}

	if p3IdleWaiter != 0 {
		throw("runtime: nested async export call")
	}
	p3IdleWaiter.set(getg())
	gopark(nil, nil, waitReasonIOWait, traceBlockGeneric, 1)

	returned := false
	for i := range p3Tasks.tasks {
		if t := &p3Tasks.tasks[i]; t.set == set {
			returned = t.returned
		}
	}
	switch wasip3PollIdle(returned) {
	case p3IdleYield:
		p3ContextSet(set)
		return p3CallbackYield
	case p3IdleWait:
		p3ContextSet(set)
		return p3CallbackWait | set<<4
	}
	p3WaitableSetDrop(set)
	return p3CallbackExit
}

// wasip3BeforeIdle returns the goroutine running a task, if any, when no
// goroutine can run, to let it return to the host.
func wasip3BeforeIdle() *g {
	gp := p3IdleWaiter.ptr()
	p3IdleWaiter = 0
	return gp
}
