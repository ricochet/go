// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build wasip1

package runtime

// wasm has no support for threads yet. There is no preemption.
// See proposal: https://github.com/WebAssembly/threads
// Waiting for a mutex or timeout is implemented as a busy loop
// while allowing other goroutines to run.

const (
	mutex_unlocked = 0
	mutex_locked   = 1

	active_spin     = 4
	active_spin_cnt = 30

	mutexMLocksDelta = 16
)

type mWaitList struct{}

func lockVerifyMSize() {}

func mutexContended(l *mutex) bool {
	return false
}

func lock(l *mutex) {
	lockWithRank(l, getLockRank(l))
}

func lock2(l *mutex) {
	if l.key == mutex_locked {
		// wasm is single-threaded so we should never
		// observe this.
		throw("self deadlock")
	}
	gp := getg()
	if gp.m.locks < 0 {
		throw("lock count")
	}
	gp.m.locks += mutexMLocksDelta
	l.key = mutex_locked
}

func unlock(l *mutex) {
	unlockWithRank(l)
}

func unlock2(l *mutex) {
	if l.key == mutex_unlocked {
		throw("unlock of unlocked lock")
	}
	gp := getg()
	gp.m.locks -= mutexMLocksDelta
	if gp.m.locks < 0 {
		throw("lock count")
	}
	l.key = mutex_unlocked
}

// One-time notifications.
func noteclear(n *note) {
	n.key = 0
}

func notewakeup(n *note) {
	if n.key != 0 {
		print("notewakeup - double wakeup (", n.key, ")\n")
		throw("notewakeup - double wakeup")
	}
	n.key = 1
}

func notesleep(n *note) {
	throw("notesleep not supported by wasi")
}

func notetsleep(n *note, ns int64) bool {
	throw("notetsleep not supported by wasi")
	return false
}

// same as runtime·notetsleep, but called on user g (not g0)
func notetsleepg(n *note, ns int64) bool {
	gp := getg()
	if gp == gp.m.g0 {
		throw("notetsleepg on g0")
	}

	deadline := nanotime() + ns
	for {
		if n.key != 0 {
			return true
		}
		if sched_yield() != 0 {
			throw("sched_yield failed")
		}
		Gosched()
		if ns >= 0 && nanotime() >= deadline {
			return false
		}
	}
}

var onIdle = func() bool {
	return false
}

// wasiOnIdle registers a callback that the scheduler calls when no goroutine
// is runnable. If it returns true, the scheduler looks for runnable goroutines
// again. It is used (via linkname) by go.bytecodealliance.org/pkg/wit/async to
// return control to the Component Model host only after all goroutines have
// made as much progress as they can.
//
// From https://github.com/golang/go/pull/76775.
func wasiOnIdle(callback func() bool) {
	onIdle = callback
}

// idleTimer is the result of wasiIdleTimer.
var idleTimer int64 = -1

// wasiIdleTimer reports how many nanoseconds after the most recent point at
// which no goroutine was runnable the earliest pending timer fires, or -1 if
// no timer was pending then. A wasiOnIdle callback that returns control to
// the host must arrange for the host to call back into Go by that time;
// otherwise goroutines waiting on timers (time.Sleep, time.After, context
// deadlines, ...) never resume. It is used (via linkname) by
// go.bytecodealliance.org/pkg/wit/async.
func wasiIdleTimer() int64 {
	return idleTimer
}

func beforeIdle(now int64, pollUntil int64, netWaiters bool) (*g, bool) {
	idleTimer = -1
	if pollUntil != 0 {
		idleTimer = max(pollUntil-now, 0)
	}
	if gp := wasip3BeforeIdle(); gp != nil {
		return gp, false
	}
	// On GOOS=wasip3, goroutines waiting for Component Model events are
	// among netWaiters, and the idle callback of a task returns to the host
	// to wait for their events.
	return nil, (!netWaiters || GOOS == "wasip3") && onIdle()
}

func checkTimeouts() {}
