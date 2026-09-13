// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build wasip3 && goexperiment.wasmcoopthreads

package runtime

// With GOEXPERIMENT=wasmcoopthreads, goroutines run on Component Model threads,
// which belong to the task that created them, so goroutines need no task
// affinity (see task_wasip3.go).

func p3TaskAdopt(gp *g, id uint32) {}
