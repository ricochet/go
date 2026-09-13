// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build wasip3

package runtime

// Network poller for GOOS=wasip3.
//
// There are no file descriptors to poll. Instead, goroutines wait for events
// on Component Model waitables (streams, futures, subtasks) with p3Await,
// which joins the waitable to a waitable set and parks the goroutine. When
// the scheduler finds nothing to run, netpoll waits on that set. Timers are
// subtasks of wasi:clocks/monotonic-clock.wait-for, joined to the same set.
//
// This state is only touched from one Component Model thread at a time, and
// never with preemption, so it needs no locks.

// A p3Waiter is a goroutine waiting for an event on a waitable.
type p3Waiter struct {
	handle  uint32 // 0 if the slot is free
	event   uint32
	payload uint32
	gp      guintptr // 0 once the event is delivered
}

var p3Poll struct {
	set uint32

	// waiters has no pointers, so that netpoll, which runs without
	// write barriers, can update it.
	waiters [64]p3Waiter

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
	return _ENOSYS
}

func netpollclose(fd uintptr) int32 {
	return _ENOSYS
}

func netpollarm(pd *pollDesc, mode int) {
	throw("runtime: unused")
}

func netpollBreak() {}

const _ENOSYS = 52

func p3PollSet() uint32 {
	if p3Poll.set == 0 {
		p3Poll.set = p3WaitableSetNew()
	}
	return p3Poll.set
}

// p3Await parks the calling goroutine until an event on waitable h, and
// returns the event code and payload.
func p3Await(h uint32) (event, payload uint32) {
	netpollGenericInit()
	var w *p3Waiter
	for i := range p3Poll.waiters {
		if p3Poll.waiters[i].handle == 0 {
			w = &p3Poll.waiters[i]
			break
		}
	}
	if w == nil {
		throw("runtime: too many goroutines waiting on Component Model waitables")
	}
	*w = p3Waiter{handle: h}
	w.gp.set(getg())
	p3WaitableJoin(h, p3PollSet())
	netpollAdjustWaiters(1)
	gopark(nil, nil, waitReasonIOWait, traceBlockNet, 1)
	event, payload = w.event, w.payload
	*w = p3Waiter{}
	return event, payload
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
		if code == p3EventSubtask {
			if payload == p3SubtaskReturned {
				p3WaitableJoin(h, 0)
				p3SubtaskDrop(h)
				p3Poll.timers--
				if h == p3Poll.timer {
					p3Poll.timer = 0
				}
			}
		} else {
			for i := range p3Poll.waiters {
				w := &p3Poll.waiters[i]
				if w.handle != h || w.gp == 0 {
					continue
				}
				p3WaitableJoin(h, 0)
				w.event, w.payload = code, payload
				toRun.push(w.gp.ptr())
				w.gp = 0
				delta--
				break
			}
		}
		code = p3WaitableSetPoll(p3PollSet(), &event)
	}
	return toRun, delta
}
