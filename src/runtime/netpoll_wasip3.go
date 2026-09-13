// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build wasip3

package runtime

import "unsafe"

// Network poller for GOOS=wasip3.
//
// There are no file descriptors to poll. Instead, goroutines wait for events
// on Component Model waitables (streams, futures, subtasks), joined to a
// waitable set. When the scheduler finds nothing to run, netpoll waits on
// that set. Timers are subtasks of wasi:clocks/monotonic-clock.wait-for,
// joined to the same set.
//
// A goroutine waits for a waitable in one of two ways:
//
//   - p3Await parks the goroutine until the waitable's event.
//   - Package net arms an operation (see net_wasip3PollArm) with a poll
//     descriptor. The event is recorded in the operation, and makes the poll
//     descriptor ready, waking a goroutine blocked in poll_runtime_pollWait,
//     so that deadlines and closing work as for other network pollers.
//
// An executable has a single waitable set, which netpoll waits on. In a
// library, Go only runs during calls of its exports. Component Model bindings
// (see the wasip3Poll hooks below) run each async export as a task with its
// own waitable set, make that set the current one while the task runs, and
// return to the host to wait for events instead of blocking in netpoll. A
// waitable joins the set that is current when a goroutine starts waiting for
// it, so its event is delivered to the task the goroutine was running in.
//
// This state is only touched from one Component Model thread at a time, and
// never with preemption, so it needs no locks.

// A p3Waiter waits for an event on a waitable.
type p3Waiter struct {
	handle  uint32 // the waitable, or 0 if the slot is free
	set     uint32 // the waitable set it joined
	event   uint32
	payload uint32
	gp      guintptr // for p3Await; 0 once the event is delivered
	op      uintptr  // for an armed operation, a *p3Op
	pd      uintptr  // for an armed operation, a *pollDesc
	mode    int32
	when    int64 // for a timer subtask, when it completes (in nanotime)
	timer   bool
	notify  uint32 // for a task's notification future, the task id
}

// p3Op mirrors package net's p3Op.
type p3Op struct {
	event, payload, done uint32
	handle               uint32
}

var p3Poll struct {
	// set is the current waitable set, or 0 until one is needed.
	set uint32

	// waiters is an array of p3Waiter indexed by waitable handle. It is not
	// in the heap, and its address is a uintptr, so that netpoll, which runs
	// without write barriers, can grow it.
	waiters    uintptr
	numWaiters int

	// timers is the number of pending timer subtasks.
	timers int32
}

func netpollinit() {}

func netpollIsPollDescriptor(fd uintptr) bool {
	return false
}

func netpollopen(fd uintptr, pd *pollDesc) int32 {
	return 0
}

func netpollclose(fd uintptr) int32 {
	for i := range p3Waiters() {
		w := &p3Waiters()[i]
		if w.handle != 0 && w.pd != 0 && (*pollDesc)(unsafe.Pointer(w.pd)).fd == fd {
			p3WaitableJoin(w.handle, 0)
			*w = p3Waiter{}
		}
	}
	return 0
}

func netpollarm(pd *pollDesc, mode int) {
	throw("runtime: unused")
}

func netpollBreak() {}

func p3Waiters() []p3Waiter {
	return unsafe.Slice((*p3Waiter)(unsafe.Pointer(p3Poll.waiters)), p3Poll.numWaiters)
}

func p3PollSet() uint32 {
	if p3Poll.set == 0 {
		p3Poll.set = p3WaitableSetNew()
	}
	return p3Poll.set
}

// p3Join joins waitable h to the current waitable set, recording a waiter
// for it.
func p3Join(h uint32) *p3Waiter {
	return p3JoinSet(h, p3PollSet())
}

// p3JoinSet joins waitable h to waitable set set, recording a waiter for it.
func p3JoinSet(h, set uint32) *p3Waiter {
	if int(h) >= p3Poll.numWaiters {
		n := max(64, 2*int(h))
		p := persistentalloc(unsafe.Sizeof(p3Waiter{})*uintptr(n), 0, &memstats.other_sys)
		copy(unsafe.Slice((*p3Waiter)(p), n), p3Waiters())
		p3Poll.waiters, p3Poll.numWaiters = uintptr(p), n
	}
	w := &p3Waiters()[h]
	if w.handle != 0 {
		throw("runtime: waitable already has a waiter")
	}
	*w = p3Waiter{handle: h, set: set}
	p3WaitableJoin(h, set)
	return w
}

// p3Await parks the calling goroutine until an event on waitable h, and
// returns the event code and payload. For a subtask, it waits for the
// subtask to return.
func p3Await(h uint32) (event, payload uint32) {
	netpollGenericInit()
	p3Join(h).gp.set(getg())
	netpollAdjustWaiters(1)
	gopark(nil, nil, waitReasonIOWait, traceBlockNet, 1)
	w := &p3Waiters()[h]
	event, payload = w.event, w.payload
	*w = p3Waiter{}
	return event, payload
}

// p3AwaitCall parks the calling goroutine until the async-lowered import
// call that returned status has returned.
func p3AwaitCall(status uint32) {
	if status&0xf == p3SubtaskReturned {
		return
	}
	subtask := status >> 4
	p3Await(subtask)
	p3SubtaskDrop(subtask)
}

// net_wasip3PollArm arranges for the completion of op, an operation on a
// waitable, to be recorded in op and to make pd ready for mode.
//
//go:linkname net_wasip3PollArm net.wasip3PollArm
func net_wasip3PollArm(pd uintptr, op unsafe.Pointer, mode int32) {
	netpollGenericInit()
	w := p3Join((*p3Op)(op).handle)
	w.op, w.pd, w.mode = uintptr(op), pd, mode
}

// net_wasip3PollDisarm undoes net_wasip3PollArm if op has not completed yet,
// and reports whether it had.
//
//go:linkname net_wasip3PollDisarm net.wasip3PollDisarm
func net_wasip3PollDisarm(op unsafe.Pointer) bool {
	o := (*p3Op)(op)
	if int(o.handle) < p3Poll.numWaiters {
		w := &p3Waiters()[o.handle]
		if w.handle != 0 && w.op == uintptr(op) {
			p3WaitableJoin(w.handle, 0)
			*w = p3Waiter{}
		}
	}
	return o.done != 0
}

// net_wasip3PollReady makes pd ready for mode, as if an operation completed.
//
//go:linkname net_wasip3PollReady net.wasip3PollReady
func net_wasip3PollReady(pd uintptr, mode int32) {
	var toRun gList
	delta := netpollready(&toRun, (*pollDesc)(unsafe.Pointer(pd)), mode)
	netpollAdjustWaiters(delta)
	injectglist(&toRun)
}

// p3ArmTimer makes sure that a timer subtask in the current waitable set
// completes within delay nanoseconds. It reports whether the delay has
// already passed.
func p3ArmTimer(delay int64) bool {
	when := nanotime() + delay
	set := p3PollSet()
	for i := range p3Waiters() {
		w := &p3Waiters()[i]
		if w.handle != 0 && w.timer && w.set == set && w.when <= when {
			return false
		}
	}
	status := p3WaitFor(uint64(delay))
	if status&0xf == p3SubtaskReturned {
		return true
	}
	w := p3Join(status >> 4)
	w.timer, w.when = true, when
	p3Poll.timers++
	return false
}

// netpoll delivers events on waitables. If delay < 0, it blocks until an
// event arrives; if delay == 0, it does not block; if delay > 0, it blocks
// for up to delay nanoseconds.
func netpoll(delay int64) (gList, int32) {
	if delay > 0 && p3ArmTimer(delay) {
		delay = 0
	}
	if netpollWaiters.Load() == 0 && p3Poll.timers == 0 {
		return gList{}, 0
	}

	var toRun gList
	delta := int32(0)
	var event [2]uint32
	var code uint32
	if delay == 0 {
		code = p3WaitableSetPoll(p3PollSet(), &event)
	} else {
		code = p3WaitableSetWait(p3PollSet(), &event)
	}
	for code != p3EventNone {
		delta += p3Deliver(&toRun, code, event[0], event[1])
		code = p3WaitableSetPoll(p3PollSet(), &event)
	}
	return toRun, delta
}

// p3Deliver hands an event on waitable h to its waiter, and returns the
// change in the number of goroutines waiting in the poller.
func p3Deliver(toRun *gList, code, h, payload uint32) int32 {
	if int(h) >= p3Poll.numWaiters || p3Waiters()[h].handle != h {
		return 0
	}
	w := &p3Waiters()[h]
	if code == p3EventSubtask && payload != p3SubtaskReturned {
		return 0 // wait for the subtask to return
	}
	p3WaitableJoin(h, 0)
	switch {
	case w.notify != 0:
		id := w.notify
		*w = p3Waiter{}
		p3TaskNotified(id)
		return 0
	case w.timer:
		p3SubtaskDrop(h)
		*w = p3Waiter{}
		p3Poll.timers--
		return 0
	case w.gp != 0:
		w.event, w.payload = code, payload
		toRun.push(w.gp.ptr())
		w.gp = 0
		return -1
	}
	o := (*p3Op)(unsafe.Pointer(w.op))
	o.event, o.payload, o.done = code, payload, 1
	pd := (*pollDesc)(unsafe.Pointer(w.pd))
	mode := w.mode
	*w = p3Waiter{}
	return netpollready(toRun, pd, mode)
}

// Hooks for Component Model bindings, such as the async support of
// go.bytecodealliance.org/pkg/wit, which call them with linkname.

// wasip3Await parks the calling goroutine until an event on waitable h, a
// stream or future end, and returns the event code and payload.
//
//go:linkname wasip3Await
func wasip3Await(h uint32) (event, payload uint32) {
	return p3Await(h)
}

// wasip3AwaitCall parks the calling goroutine until the async-lowered import
// call that returned status has returned, and drops its subtask.
//
//go:linkname wasip3AwaitCall
func wasip3AwaitCall(status uint32) {
	p3AwaitCall(status)
}

// wasip3PollNewSet returns a new waitable set, for a new task.
//
//go:linkname wasip3PollNewSet
func wasip3PollNewSet() uint32 {
	set := p3WaitableSetNew()
	p3TaskNew(set)
	return set
}

// wasip3PollSetCurrent makes set, the waitable set of the task whose callback
// is running, the current waitable set, which waitables join when goroutines
// start waiting for them.
//
//go:linkname wasip3PollSetCurrent
func wasip3PollSetCurrent(set uint32) {
	p3Poll.set = set
	p3TaskSetCurrent(set)
}

// wasip3PollDeliver delivers an event that the host passed to a task's
// callback.
//
//go:linkname wasip3PollDeliver
func wasip3PollDeliver(code, h, payload uint32) {
	var toRun gList
	netpollAdjustWaiters(p3Deliver(&toRun, code, h, payload))
	injectglist(&toRun)
}

// Results of wasip3PollIdle.
const (
	p3IdleDone  = 0 // the task is done
	p3IdleWait  = 1 // wait for an event in the set
	p3IdleYield = 2 // a timer is already due
)

// wasip3PollIdle is called by a task when no goroutine can run, with whether
// the task's export has produced its result. It makes sure a timer subtask in
// the current waitable set completes when the next goroutine timer is due,
// and reports what the task should do. The task is done once its export has
// produced its result and nothing else is waiting in the set; until then, its
// notification (see task_wasip3.go) lets the runtime wake it up.
//
//go:linkname wasip3PollIdle
func wasip3PollIdle(returned bool) int {
	if idleTimer >= 0 && p3ArmTimer(idleTimer) {
		return p3IdleYield
	}
	if !returned {
		return p3IdleWait
	}
	set := p3PollSet()
	for i := range p3Waiters() {
		if w := &p3Waiters()[i]; w.handle != 0 && w.set == set && w.notify == 0 {
			return p3IdleWait
		}
	}
	p3TaskExit()
	return p3IdleDone
}
