// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build wasip1

package unix

import (
	"syscall"
	"unsafe"
)

// The values of these constants are not part of the WASI API.
const (
	// UTIME_OMIT is the sentinel value to indicate that a time value should not
	// be changed. It is useful for example to indicate for example with UtimesNano
	// to avoid changing AccessTime or ModifiedTime.
	// Its value must match syscall/fs_wasip1.go
	UTIME_OMIT = -0x2

	AT_REMOVEDIR        = 0x200
	AT_SYMLINK_NOFOLLOW = 0x100
)

func Unlinkat(dirfd int, path string, flags int) error {
	if flags&AT_REMOVEDIR == 0 {
		return errnoErr(path_unlink_file(
			int32(dirfd),
			unsafe.StringData(path),
			size(len(path)),
		))
	} else {
		return errnoErr(path_remove_directory(
			int32(dirfd),
			unsafe.StringData(path),
			size(len(path)),
		))
	}
}

func Openat(dirfd int, path string, flags int, perm uint32) (int, error) {
	return syscall.Openat(dirfd, path, flags, perm)
}

func Fstatat(dirfd int, path string, stat *syscall.Stat_t, flags int) error {
	var filestatFlags uint32
	if flags&AT_SYMLINK_NOFOLLOW == 0 {
		filestatFlags |= syscall.LOOKUP_SYMLINK_FOLLOW
	}
	return errnoErr(path_filestat_get(
		int32(dirfd),
		uint32(filestatFlags),
		unsafe.StringData(path),
		size(len(path)),
		unsafe.Pointer(stat),
	))
}

func Readlinkat(dirfd int, path string, buf []byte) (int, error) {
	var nwritten size
	errno := path_readlink(
		int32(dirfd),
		unsafe.StringData(path),
		size(len(path)),
		&buf[0],
		size(len(buf)),
		&nwritten)
	return int(nwritten), errnoErr(errno)

}

type (
	size = uint32
)

func Mkdirat(dirfd int, path string, mode uint32) error {
	if path == "" {
		return syscall.EINVAL
	}
	return errnoErr(path_create_directory(
		int32(dirfd),
		unsafe.StringData(path),
		size(len(path)),
	))
}

func Fchmodat(dirfd int, path string, mode uint32, flags int) error {
	// WASI preview 1 doesn't support changing file modes.
	return syscall.ENOSYS
}

func Fchownat(dirfd int, path string, uid, gid int, flags int) error {
	// WASI preview 1 doesn't support changing file ownership.
	return syscall.ENOSYS
}

func Renameat(olddirfd int, oldpath string, newdirfd int, newpath string) error {
	if oldpath == "" || newpath == "" {
		return syscall.EINVAL
	}
	return errnoErr(path_rename(
		int32(olddirfd),
		unsafe.StringData(oldpath),
		size(len(oldpath)),
		int32(newdirfd),
		unsafe.StringData(newpath),
		size(len(newpath)),
	))
}

func Linkat(olddirfd int, oldpath string, newdirfd int, newpath string, flag int) error {
	if oldpath == "" || newpath == "" {
		return syscall.EINVAL
	}
	return errnoErr(path_link(
		int32(olddirfd),
		0,
		unsafe.StringData(oldpath),
		size(len(oldpath)),
		int32(newdirfd),
		unsafe.StringData(newpath),
		size(len(newpath)),
	))
}

func Symlinkat(oldpath string, newdirfd int, newpath string) error {
	if oldpath == "" || newpath == "" {
		return syscall.EINVAL
	}
	return errnoErr(path_symlink(
		unsafe.StringData(oldpath),
		size(len(oldpath)),
		int32(newdirfd),
		unsafe.StringData(newpath),
		size(len(newpath)),
	))
}

func errnoErr(errno syscall.Errno) error {
	if errno == 0 {
		return nil
	}
	return errno
}
