// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build wasip3

package syscall

import "unsafe"

// GOOS=wasip3 shares the GOOS=wasip1 implementation of this package, which is
// written in terms of WASI preview 1 functions. This file and fs_wasip3.go
// implement those functions on top of WASI 0.3 (see runtime/os_wasip3.go):
// stdin, stdout and stderr are Component Model streams, files use
// wasi:filesystem, and the clocks, random numbers and exiting map directly to
// WASI 0.3 interfaces. Package net implements sockets on wasi:sockets itself,
// so the socket functions here fail.

// Implemented in the runtime, which copies buffers rather than retaining them.

//go:noescape
func wasip3Write(fd int32, p unsafe.Pointer, n int32) (written int32, broken bool)

//go:noescape
func wasip3Read(p unsafe.Pointer, n int32) int32

func wasip3ClockTime(realtime bool) uint64

//go:noescape
func wasip3Random(r []byte)

func wasip3Exit(code int32)

func ProcExit(code int32) {
	wasip3Exit(code)
}

func random_get(buf *byte, bufLen size) Errno {
	wasip3Random(unsafe.Slice(buf, bufLen))
	return 0
}

func clock_time_get(id clockid, precision timestamp, time *timestamp) Errno {
	switch id {
	case clockRealtime:
		*time = wasip3ClockTime(true)
	case clockMonotonic:
		*time = wasip3ClockTime(false)
	default:
		return EINVAL
	}
	return 0
}

func sock_accept(fd int32, flags fdflags, newfd *int32) Errno {
	return ENOSYS
}

func sock_shutdown(fd int32, flags sdflags) Errno {
	return ENOSYS
}
