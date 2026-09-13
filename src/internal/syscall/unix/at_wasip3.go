// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build wasip3

package unix

import (
	"syscall"
	"unsafe"
)

// GOOS=wasip3 has no preopened directories (see syscall/wasip3.go), so these
// WASI preview 1 path functions always fail.

func path_unlink_file(fd int32, path *byte, pathLen size) syscall.Errno {
	return syscall.ENOSYS
}

func path_remove_directory(fd int32, path *byte, pathLen size) syscall.Errno {
	return syscall.ENOSYS
}

func path_filestat_get(fd int32, flags uint32, path *byte, pathLen size, buf unsafe.Pointer) syscall.Errno {
	return syscall.ENOSYS
}

func path_readlink(fd int32, path *byte, pathLen size, buf *byte, bufLen size, nwritten *size) syscall.Errno {
	return syscall.ENOSYS
}

func path_create_directory(fd int32, path *byte, pathLen size) syscall.Errno {
	return syscall.ENOSYS
}

func path_rename(oldFd int32, oldPath *byte, oldPathLen size, newFd int32, newPath *byte, newPathLen size) syscall.Errno {
	return syscall.ENOSYS
}

func path_link(oldFd int32, oldFlags uint32, oldPath *byte, oldPathLen size, newFd int32, newPath *byte, newPathLen size) syscall.Errno {
	return syscall.ENOSYS
}

func path_symlink(oldPath *byte, oldPathLen size, fd int32, newPath *byte, newPathLen size) syscall.Errno {
	return syscall.ENOSYS
}

func path_filestat_set_times(fd int32, flags uint32, path *byte, pathLen size, atim uint64, mtim uint64, fstflags uint32) syscall.Errno {
	return syscall.ENOSYS
}
