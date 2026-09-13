// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build wasip3

package runtime

import "unsafe"

// Tasks of Component Model bindings in GOOS=wasip3 libraries.
//
// Component Model bindings run each async export call as a task (see the
// wasip3Poll hooks in netpoll_wasip3.go), and Go only runs while the host calls
// into one of them. The runtime tracks tasks for two reasons:
//
//   - A task must not exit before its export has produced a result, even when
//     nothing is joined to its waitable set, for example while its goroutine
//     waits on a channel that another task's goroutine sends to. So each task
//     has a future that its callback always has a pending read on, and that
//     the runtime writes to notify the task.
//
//   - Built-ins such as task.return act on the task that the host is running,
//     so without GOEXPERIMENT=wasmcoopthreads, a goroutine that belongs to one
//     task must not run while the host runs another (see
//     task_wasip3_affinity.go). With it, goroutines run on Component Model
//     threads, which belong to the task that created them.

//go:wasmimport $root [future-new-unit]go-task
func p3TaskFutureNew() uint64

//go:wasmimport $root [async-lower][future-read-unit]go-task
func p3TaskFutureRead(readable uint32, buf unsafe.Pointer) uint32

//go:wasmimport $root [async-lower][future-write-unit]go-task
func p3TaskFutureWrite(writable uint32, buf unsafe.Pointer) uint32

//go:wasmimport $root [future-cancel-read-unit]go-task
func p3TaskFutureCancelRead(readable uint32) uint32

//go:wasmimport $root [future-drop-readable-unit]go-task
func p3TaskFutureDropReadable(readable uint32)

//go:wasmimport $root [future-drop-writable-unit]go-task
func p3TaskFutureDropWritable(writable uint32)

// A p3Task is a task of Component Model bindings.
type p3Task struct {
	id  uint32
	set uint32

	// deferred holds the task's runnable goroutines that the scheduler
	// picked while another task was running.
	deferred gList

	// The task's callback has a pending read on notifyReadable, in set.
	// notified reports whether notifyWritable was written since.
	notifyReadable uint32
	notifyWritable uint32
	notified       bool

	// returned reports whether the goroutine of an async export returned
	// its result (see export_wasip3.go).
	returned bool

	// cabiArena holds the parameters of the async export call that started
	// the task (see cabi_wasip3.go).
	cabiArena p3Arena
}

var p3Tasks struct {
	tasks  []p3Task
	cur    uint32 // the id of the running task, or 0
	nextID uint32
}

func p3FindTask(id uint32) *p3Task {
	for i := range p3Tasks.tasks {
		if t := &p3Tasks.tasks[i]; t.id == id {
			return t
		}
	}
	return nil
}

// p3TaskNew starts tracking the task with waitable set set.
func p3TaskNew(set uint32) {
	p3Tasks.nextID++
	p3Tasks.tasks = append(p3Tasks.tasks, p3Task{id: p3Tasks.nextID, set: set})
	p3TaskArm(&p3Tasks.tasks[len(p3Tasks.tasks)-1])
}

// p3TaskArm gives t a new notification future with a pending read.
func p3TaskArm(t *p3Task) {
	pair := p3TaskFutureNew()
	t.notifyReadable, t.notifyWritable = uint32(pair), uint32(pair>>32)
	t.notified = false
	if code := p3TaskFutureRead(t.notifyReadable, nil); code != p3Blocked {
		throw("runtime: task notification future is not pending")
	}
	w := p3JoinSet(t.notifyReadable, t.set)
	w.notify = t.id
}

// p3TaskNotify notifies t, unless it was notified since its callback last
// read the notification.
func p3TaskNotify(t *p3Task) {
	if t.notified {
		return
	}
	t.notified = true
	if code := p3TaskFutureWrite(t.notifyWritable, nil); code&0xf != p3Completed {
		throw("runtime: task notification not delivered")
	}
}

// p3TaskSetCurrent records that the task with waitable set set is running,
// and makes its set-aside goroutines runnable.
func p3TaskSetCurrent(set uint32) {
	for i := range p3Tasks.tasks {
		t := &p3Tasks.tasks[i]
		if t.set != set {
			continue
		}
		p3Tasks.cur = t.id
		p3TaskAdopt(getg().m.curg, t.id)
		// They are still runnable.
		pp := getg().m.p.ptr()
		for !t.deferred.empty() {
			runqput(pp, t.deferred.pop(), false)
		}
		return
	}
	p3Tasks.cur = 0
}

// p3TaskNotified handles the notification of task id, after its
// notification future was read, by arming a new notification. The event is
// delivered to the task's callback, which already ran the task's set-aside
// goroutines (see p3TaskSetCurrent).
//
//go:nowritebarrierrec
func p3TaskNotified(id uint32) {
	t := p3FindTask(id)
	if t == nil {
		return
	}
	p3TaskFutureDropReadable(t.notifyReadable)
	p3TaskFutureDropWritable(t.notifyWritable)
	p3TaskArm(t)
}

// p3TaskExit stops tracking the running task.
func p3TaskExit() {
	for i := range p3Tasks.tasks {
		t := &p3Tasks.tasks[i]
		if t.id != p3Tasks.cur {
			continue
		}
		if w := &p3Waiters()[t.notifyReadable]; w.handle == t.notifyReadable {
			p3WaitableJoin(w.handle, 0)
			*w = p3Waiter{}
		}
		// A future's write end can't be dropped before it is written.
		// Writing completes the pending read, whose result the cancel
		// then takes.
		if !t.notified {
			p3TaskFutureWrite(t.notifyWritable, nil)
		}
		p3TaskFutureCancelRead(t.notifyReadable)
		p3TaskFutureDropReadable(t.notifyReadable)
		p3TaskFutureDropWritable(t.notifyWritable)
		t.cabiArena.free()
		// The task's set is dropped once it exits (by the task's callback).
		if p3Poll.set == t.set {
			p3Poll.set = 0
		}
		p3Tasks.tasks = append(p3Tasks.tasks[:i], p3Tasks.tasks[i+1:]...)
		break
	}
	p3Tasks.cur = 0
}

// wasip3PollWake makes the host call back the task with waitable set set, if
// it is not the running task. Bindings call it when the goroutine of a task's
// export finishes, which may happen while another task runs.
//
//go:linkname wasip3PollWake
func wasip3PollWake(set uint32) {
	for i := range p3Tasks.tasks {
		t := &p3Tasks.tasks[i]
		if t.set == set && t.id != p3Tasks.cur {
			p3TaskNotify(t)
		}
	}
}
