// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build wasip3

package http

func init() {
	// Tests check the behavior of the connections that transports make,
	// rather than of the host's HTTP client, unless they use SetWASIHTTP.
	p3HTTPDisabled = true
}

// SetWASIHTTP sets whether transports send requests with the host's
// wasi:http client, and returns a function that restores the setting.
func SetWASIHTTP(enabled bool) (restore func()) {
	old := p3HTTPDisabled
	p3HTTPDisabled = !enabled
	return func() { p3HTTPDisabled = old }
}
