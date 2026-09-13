// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !(wasip1 && goexperiment.wasmcoopthreads) && !wasip3

package runtime

// gCoop is the per-goroutine Component Model thread state, which only exists
// on wasip1 with GOEXPERIMENT=wasmcoopthreads (see coop_wasip1.go), or the
// goroutine's task on GOOS=wasip3 (see task_wasip3.go).
type gCoop struct{}

func coopNewproc(*g) {}

// p3TaskDefer sets aside goroutines of other tasks on GOOS=wasip3 (see
// task_wasip3.go).
func p3TaskDefer(*g) bool { return false }
