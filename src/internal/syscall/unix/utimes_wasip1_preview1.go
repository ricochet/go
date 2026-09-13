// Copyright 2025 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build wasip1 && !wasip3

package unix

import (
	"syscall"
)

// WASI preview 1 imports. GOOS=wasip3 implements these functions on top
// of WASI 0.3 instead.

//go:wasmimport wasi_snapshot_preview1 path_filestat_set_times
//go:noescape
func path_filestat_set_times(fd int32, flags uint32, path *byte, pathLen size, atim uint64, mtim uint64, fstflags uint32) syscall.Errno
