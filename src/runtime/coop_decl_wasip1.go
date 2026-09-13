// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build wasip1 && !goexperiment.wasmcoopthreads

package runtime

// Declarations for assembly functions that are only assembled with
// GOEXPERIMENT=wasmcoopthreads (see coop_wasip1.go), so that vet's asmdecl
// check, which does not evaluate #ifdef, finds them.

func wasmCoopThreadStart()
func wasmCoopStartMain()
