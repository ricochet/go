// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build wasip1 && !wasip3

package unix

import (
	"syscall"
	"unsafe"
)

// WASI preview 1 imports. GOOS=wasip3 implements these functions on top
// of WASI 0.3 instead.

//go:wasmimport wasi_snapshot_preview1 path_unlink_file
//go:noescape
func path_unlink_file(fd int32, path *byte, pathLen size) syscall.Errno

//go:wasmimport wasi_snapshot_preview1 path_remove_directory
//go:noescape
func path_remove_directory(fd int32, path *byte, pathLen size) syscall.Errno

//go:wasmimport wasi_snapshot_preview1 path_filestat_get
//go:noescape
func path_filestat_get(fd int32, flags uint32, path *byte, pathLen size, buf unsafe.Pointer) syscall.Errno

//go:wasmimport wasi_snapshot_preview1 path_readlink
//go:noescape
func path_readlink(fd int32, path *byte, pathLen size, buf *byte, bufLen size, nwritten *size) syscall.Errno

//go:wasmimport wasi_snapshot_preview1 path_create_directory
//go:noescape
func path_create_directory(fd int32, path *byte, pathLen size) syscall.Errno

//go:wasmimport wasi_snapshot_preview1 path_rename
//go:noescape
func path_rename(oldFd int32, oldPath *byte, oldPathLen size, newFd int32, newPath *byte, newPathLen size) syscall.Errno

//go:wasmimport wasi_snapshot_preview1 path_link
//go:noescape
func path_link(oldFd int32, oldFlags uint32, oldPath *byte, oldPathLen size, newFd int32, newPath *byte, newPathLen size) syscall.Errno

//go:wasmimport wasi_snapshot_preview1 path_symlink
//go:noescape
func path_symlink(oldPath *byte, oldPathLen size, fd int32, newPath *byte, newPathLen size) syscall.Errno
