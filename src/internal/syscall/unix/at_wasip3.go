// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build wasip3

package unix

import (
	"syscall"
	"unsafe"
)

// The syscall package implements these WASI preview 1 functions on top of
// wasi:filesystem@0.3.0.

//go:linkname path_unlink_file syscall.path_unlink_file
func path_unlink_file(fd int32, path *byte, pathLen size) syscall.Errno

//go:linkname path_remove_directory syscall.path_remove_directory
func path_remove_directory(fd int32, path *byte, pathLen size) syscall.Errno

//go:linkname path_filestat_get syscall.path_filestat_get
func path_filestat_get(fd int32, flags uint32, path *byte, pathLen size, buf unsafe.Pointer) syscall.Errno

//go:linkname path_readlink syscall.path_readlink
func path_readlink(fd int32, path *byte, pathLen size, buf *byte, bufLen size, nwritten *size) syscall.Errno

//go:linkname path_create_directory syscall.path_create_directory
func path_create_directory(fd int32, path *byte, pathLen size) syscall.Errno

//go:linkname path_rename syscall.path_rename
func path_rename(oldFd int32, oldPath *byte, oldPathLen size, newFd int32, newPath *byte, newPathLen size) syscall.Errno

//go:linkname path_link syscall.path_link
func path_link(oldFd int32, oldFlags uint32, oldPath *byte, oldPathLen size, newFd int32, newPath *byte, newPathLen size) syscall.Errno

//go:linkname path_symlink syscall.path_symlink
func path_symlink(oldPath *byte, oldPathLen size, fd int32, newPath *byte, newPathLen size) syscall.Errno

//go:linkname path_filestat_set_times syscall.path_filestat_set_times
func path_filestat_set_times(fd int32, flags uint32, path *byte, pathLen size, atim uint64, mtim uint64, fstflags uint32) syscall.Errno
