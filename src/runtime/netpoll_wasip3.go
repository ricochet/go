// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build wasip3

package runtime

import "unsafe"

// Network poller for GOOS=wasip3.
//
// There are no file descriptors to poll. Instead, goroutines wait for events
// on Component Model waitables (streams, futures, subtasks), joined to one
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
// This state is only touched from one Component Model thread at a time, and
// never with preemption, so it needs no locks.

// A p3Waiter waits for an event on a waitable.
type p3Waiter struct {
	handle  uint32 // the waitable, or 0 if the slot is free
	event   uint32
	payload uint32
	gp      guintptr // for p3Await; 0 once the event is delivered
	op      uintptr  // for an armed operation, a *p3Op
	pd      uintptr  // for an armed operation, a *pollDesc
	mode    int32
}

// p3Op mirrors package net's p3Op.
type p3Op struct {
	event, payload, done uint32
	handle               uint32
}

var p3Poll struct {
	set uint32

	// waiters is indexed by waitable handle. It has no pointers, so that
	// netpoll, which runs without write barriers, can update it.
	waiters []p3Waiter

	// timer is the pending wait-for subtask that completes the soonest,
	// at timerWhen (in nanotime), or 0.
	timer     uint32
	timerWhen int64
	// timers is the number of wait-for subtasks joined to set.
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
	for i := range p3Poll.waiters {
		w := &p3Poll.waiters[i]
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

func p3PollSet() uint32 {
	if p3Poll.set == 0 {
		p3Poll.set = p3WaitableSetNew()
	}
	return p3Poll.set
}

// p3NewWaiter returns the free waiter slot for waitable h.
func p3NewWaiter(h uint32) *p3Waiter {
	if int(h) >= len(p3Poll.waiters) {
		waiters := make([]p3Waiter, max(64, 2*int(h)))
		copy(waiters, p3Poll.waiters)
		p3Poll.waiters = waiters
	}
	w := &p3Poll.waiters[h]
	if w.handle != 0 {
		throw("runtime: waitable already has a waiter")
	}
	*w = p3Waiter{handle: h}
	return w
}

// p3Await parks the calling goroutine until an event on waitable h, and
// returns the event code and payload.
func p3Await(h uint32) (event, payload uint32) {
	netpollGenericInit()
	w := p3NewWaiter(h)
	w.gp.set(getg())
	p3WaitableJoin(h, p3PollSet())
	netpollAdjustWaiters(1)
	gopark(nil, nil, waitReasonIOWait, traceBlockNet, 1)
	w = &p3Poll.waiters[h]
	event, payload = w.event, w.payload
	*w = p3Waiter{}
	return event, payload
}

// net_wasip3PollArm arranges for the completion of op, an operation on a
// waitable, to be recorded in op and to make pd ready for mode.
//
//go:linkname net_wasip3PollArm net.wasip3PollArm
func net_wasip3PollArm(pd uintptr, op unsafe.Pointer, mode int32) {
	netpollGenericInit()
	o := (*p3Op)(op)
	w := p3NewWaiter(o.handle)
	w.op, w.pd, w.mode = uintptr(op), pd, mode
	p3WaitableJoin(o.handle, p3PollSet())
}

// net_wasip3PollDisarm undoes net_wasip3PollArm if op has not completed yet,
// and reports whether it had.
//
//go:linkname net_wasip3PollDisarm net.wasip3PollDisarm
func net_wasip3PollDisarm(op unsafe.Pointer) bool {
	o := (*p3Op)(op)
	if int(o.handle) < len(p3Poll.waiters) {
		w := &p3Poll.waiters[o.handle]
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

// p3ArmTimer makes sure that a wait-for subtask completes within delay
// nanoseconds. It reports whether the delay has already passed.
func p3ArmTimer(delay int64) bool {
	when := nanotime() + delay
	if p3Poll.timer != 0 && p3Poll.timerWhen <= when {
		return false
	}
	status := p3WaitFor(uint64(delay))
	if status&0xf == p3SubtaskReturned {
		return true
	}
	subtask := status >> 4
	p3WaitableJoin(subtask, p3PollSet())
	p3Poll.timer = subtask
	p3Poll.timerWhen = when
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
		h, payload := event[0], event[1]
		if int(h) < len(p3Poll.waiters) && p3Poll.waiters[h].handle == h {
			delta += p3Deliver(&toRun, &p3Poll.waiters[h], code, payload)
		} else if code == p3EventSubtask && payload == p3SubtaskReturned {
			// Only timers are joined to the set without a waiter.
			p3WaitableJoin(h, 0)
			p3SubtaskDrop(h)
			p3Poll.timers--
			if h == p3Poll.timer {
				p3Poll.timer = 0
			}
		}
		code = p3WaitableSetPoll(p3PollSet(), &event)
	}
	return toRun, delta
}

// p3Deliver hands an event to its waiter, and returns the change in the
// number of goroutines waiting in the poller.
func p3Deliver(toRun *gList, w *p3Waiter, code, payload uint32) int32 {
	if code == p3EventSubtask && payload != p3SubtaskReturned {
		return 0 // wait for the subtask to return
	}
	p3WaitableJoin(w.handle, 0)
	if w.gp != 0 {
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
