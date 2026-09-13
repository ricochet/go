// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build wasip3 && !goexperiment.wasmcoopthreads

package runtime

// Task affinity of goroutines (see task_wasip3.go).
//
// A goroutine belongs to the task of the goroutine that created it; runtime
// goroutines belong to none. When the scheduler picks a goroutine of a task
// other than the running one, it sets the goroutine aside and notifies its
// task, so that the host calls the task back to run the goroutine.

// gCoop holds a goroutine's task.
type gCoop struct {
	task uint32 // the id of the task this goroutine belongs to, or 0
}

func coopNewproc(gp *g) {
	gp.coop.task = 0
	if curg := getg().m.curg; curg != nil && !isSystemGoroutine(gp, false) {
		gp.coop.task = curg.coop.task
	}
}

// p3TaskAdopt makes gp belong to task id.
func p3TaskAdopt(gp *g, id uint32) {
	gp.coop.task = id
}

// p3TaskDefer reports whether gp belongs to a task other than the running
// one, in which case it sets gp aside for that task and notifies the task.
func p3TaskDefer(gp *g) bool {
	id := gp.coop.task
	if id == 0 || p3Tasks.cur == 0 || id == p3Tasks.cur {
		return false
	}
	t := p3FindTask(id)
	if t == nil {
		// The task has exited; its goroutines may run anywhere.
		gp.coop.task = 0
		return false
	}
	t.deferred.push(gp)
	p3TaskNotify(t)
	return true
}
