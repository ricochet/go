// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build goexperiment.regabiargs

package abi

const (
	// See abi_generic.go.

	// R0 - R2, passed as wasm parameters and results
	// (GOEXPERIMENT=wasmregabi). Wasmtime passes few multi-value
	// results in native registers, so this is kept small.
	IntArgRegs = 3

	FloatArgRegs = 0

	EffectiveFloatRegSize = 0
)
