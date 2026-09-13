// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build wasip1

package runtime

// wasmCoopCabiRealloc (rt0_wasip1_wasm.s) is exported as cabi_realloc in
// GOOS=wasip1 executables with GOEXPERIMENT=wasmcoopthreads, where the host
// allocates through it to lower lists. (GOOS=wasip3 modules export
// cabiRealloc instead; see cabi_wasip3.go.) The
// preview1 component adapter allocates its global state through it before it
// calls _start, so it cannot be a //go:wasmexport function (those require an
// initialized runtime). Without it, wit-component synthesizes a realloc that
// grows linear memory behind the Go heap's back, and sbrk would later hand
// out the same pages again. Before osinit it starts the break at the current
// end of memory; see osinit.
func wasmCoopCabiRealloc()

// cabiNext and cabiEnd delimit the unused part of the chunk that
// wasmCoopCabiRealloc allocates from.
var cabiNext, cabiEnd uintptr
