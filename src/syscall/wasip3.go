// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build wasip3

package syscall

import "unsafe"

// GOOS=wasip3 shares the GOOS=wasip1 implementation of this package, which is
// written in terms of WASI preview 1 functions. This file implements those
// functions on top of WASI 0.3 (see runtime/os_wasip3.go): stdin, stdout and
// stderr are Component Model streams, and the clocks, random numbers and
// exiting map directly to WASI 0.3 interfaces. There are no preopened
// directories, so every other file system and socket function fails.

// Implemented in the runtime.
func wasip3Write(fd int32, p unsafe.Pointer, n int32) (written int32, broken bool)
func wasip3Read(p unsafe.Pointer, n int32) int32
func wasip3ClockTime(realtime bool) uint64
func wasip3Random(r []byte)
func wasip3Exit(code int32)

func ProcExit(code int32) {
	wasip3Exit(code)
}

func fd_write(fd int32, iovs *iovec, iovsLen size, nwritten *size) Errno {
	if fd != Stdout && fd != Stderr {
		return EBADF
	}
	*nwritten = 0
	for _, iov := range unsafe.Slice(iovs, iovsLen) {
		written, broken := wasip3Write(fd, unsafe.Pointer(uintptr(iov.buf)), int32(iov.bufLen))
		*nwritten += size(written)
		if broken {
			return EPIPE
		}
	}
	return 0
}

func fd_read(fd int32, iovs *iovec, iovsLen size, nread *size) Errno {
	if fd != Stdin {
		return EBADF
	}
	*nread = 0
	for _, iov := range unsafe.Slice(iovs, iovsLen) {
		if iov.bufLen == 0 {
			continue
		}
		// Like read(2), return as soon as some bytes are available.
		*nread = size(wasip3Read(unsafe.Pointer(uintptr(iov.buf)), int32(iov.bufLen)))
		break
	}
	return 0
}

func isStdio(fd int32) bool {
	return fd == Stdin || fd == Stdout || fd == Stderr
}

func fd_close(fd int32) Errno {
	if isStdio(fd) {
		return 0
	}
	return EBADF
}

func fd_fdstat_get(fd int32, buf *fdstat) Errno {
	if !isStdio(fd) {
		return EBADF
	}
	*buf = fdstat{filetype: FILETYPE_CHARACTER_DEVICE}
	if fd == Stdin {
		buf.rightsBase = RIGHT_FD_READ
	} else {
		buf.rightsBase = RIGHT_FD_WRITE
	}
	return 0
}

func fd_fdstat_set_flags(fd int32, flags fdflags) Errno {
	if !isStdio(fd) {
		return EBADF
	}
	return ENOSYS
}

func fd_fdstat_set_rights(fd int32, rightsBase rights, rightsInheriting rights) Errno {
	return ENOSYS
}

func fd_seek(fd int32, offset filedelta, whence uint32, newoffset *filesize) Errno {
	if isStdio(fd) {
		return ESPIPE
	}
	return EBADF
}

func fd_prestat_get(fd int32, prestat *prestat) Errno {
	return EBADF // no preopened directories
}

func fd_prestat_dir_name(fd int32, path *byte, pathLen size) Errno {
	return EBADF
}

func fd_filestat_get(fd int32, buf unsafe.Pointer) Errno {
	return ENOSYS
}

func fd_filestat_set_size(fd int32, set_size filesize) Errno {
	return ENOSYS
}

func fd_pread(fd int32, iovs *iovec, iovsLen size, offset filesize, nread *size) Errno {
	return ENOSYS
}

func fd_pwrite(fd int32, iovs *iovec, iovsLen size, offset filesize, nwritten *size) Errno {
	return ENOSYS
}

func fd_readdir(fd int32, buf *byte, bufLen size, cookie dircookie, nwritten *size) Errno {
	return ENOSYS
}

func fd_sync(fd int32) Errno {
	return ENOSYS
}

func path_create_directory(fd int32, path *byte, pathLen size) Errno {
	return ENOSYS
}

func path_filestat_get(fd int32, flags lookupflags, path *byte, pathLen size, buf unsafe.Pointer) Errno {
	return ENOSYS
}

func path_filestat_set_times(fd int32, flags lookupflags, path *byte, pathLen size, atim timestamp, mtim timestamp, fstflags fstflags) Errno {
	return ENOSYS
}

func path_link(oldFd int32, oldFlags lookupflags, oldPath *byte, oldPathLen size, newFd int32, newPath *byte, newPathLen size) Errno {
	return ENOSYS
}

func path_readlink(fd int32, path *byte, pathLen size, buf *byte, bufLen size, nwritten *size) Errno {
	return ENOSYS
}

func path_remove_directory(fd int32, path *byte, pathLen size) Errno {
	return ENOSYS
}

func path_rename(oldFd int32, oldPath *byte, oldPathLen size, newFd int32, newPath *byte, newPathLen size) Errno {
	return ENOSYS
}

func path_symlink(oldPath *byte, oldPathLen size, fd int32, newPath *byte, newPathLen size) Errno {
	return ENOSYS
}

func path_unlink_file(fd int32, path *byte, pathLen size) Errno {
	return ENOSYS
}

func path_open(rootFD int32, dirflags lookupflags, path *byte, pathLen size, oflags oflags, fsRightsBase rights, fsRightsInheriting rights, fsFlags fdflags, fd *int32) Errno {
	return ENOSYS
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
