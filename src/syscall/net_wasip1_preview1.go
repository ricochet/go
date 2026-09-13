// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build wasip1 && !wasip3

package syscall

// WASI preview 1 imports. GOOS=wasip3 implements these functions on top
// of WASI 0.3 instead.

//go:wasmimport wasi_snapshot_preview1 sock_accept
//go:noescape
func sock_accept(fd int32, flags fdflags, newfd *int32) Errno

//go:wasmimport wasi_snapshot_preview1 sock_shutdown
//go:noescape
func sock_shutdown(fd int32, flags sdflags) Errno
