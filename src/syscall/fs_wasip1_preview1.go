// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build wasip1 && !wasip3

package syscall

import (
	"unsafe"
)

// WASI preview 1 imports. GOOS=wasip3 implements these functions on top
// of WASI 0.3 instead.

// https://github.com/WebAssembly/WASI/blob/a2b96e81c0586125cc4dc79a5be0b78d9a059925/legacy/preview1/docs.md#-fd_closefd-fd---result-errno
//
//go:wasmimport wasi_snapshot_preview1 fd_close
//go:noescape
func fd_close(fd int32) Errno

// https://github.com/WebAssembly/WASI/blob/a2b96e81c0586125cc4dc79a5be0b78d9a059925/legacy/preview1/docs.md#-fd_filestat_set_sizefd-fd-size-filesize---result-errno
//
//go:wasmimport wasi_snapshot_preview1 fd_filestat_set_size
//go:noescape
func fd_filestat_set_size(fd int32, set_size filesize) Errno

// https://github.com/WebAssembly/WASI/blob/a2b96e81c0586125cc4dc79a5be0b78d9a059925/legacy/preview1/docs.md#-fd_preadfd-fd-iovs-iovec_array-offset-filesize---resultsize-errno
//
//go:wasmimport wasi_snapshot_preview1 fd_pread
//go:noescape
func fd_pread(fd int32, iovs *iovec, iovsLen size, offset filesize, nread *size) Errno

//go:wasmimport wasi_snapshot_preview1 fd_pwrite
//go:noescape
func fd_pwrite(fd int32, iovs *iovec, iovsLen size, offset filesize, nwritten *size) Errno

//go:wasmimport wasi_snapshot_preview1 fd_read
//go:noescape
func fd_read(fd int32, iovs *iovec, iovsLen size, nread *size) Errno

//go:wasmimport wasi_snapshot_preview1 fd_readdir
//go:noescape
func fd_readdir(fd int32, buf *byte, bufLen size, cookie dircookie, nwritten *size) Errno

//go:wasmimport wasi_snapshot_preview1 fd_seek
//go:noescape
func fd_seek(fd int32, offset filedelta, whence uint32, newoffset *filesize) Errno

// https://github.com/WebAssembly/WASI/blob/a2b96e81c0586125cc4dc79a5be0b78d9a059925/legacy/preview1/docs.md#-fd_fdstat_set_rightsfd-fd-fs_rights_base-rights-fs_rights_inheriting-rights---result-errno
//
//go:wasmimport wasi_snapshot_preview1 fd_fdstat_set_rights
//go:noescape
func fd_fdstat_set_rights(fd int32, rightsBase rights, rightsInheriting rights) Errno

//go:wasmimport wasi_snapshot_preview1 fd_filestat_get
//go:noescape
func fd_filestat_get(fd int32, buf unsafe.Pointer) Errno

//go:wasmimport wasi_snapshot_preview1 fd_write
//go:noescape
func fd_write(fd int32, iovs *iovec, iovsLen size, nwritten *size) Errno

//go:wasmimport wasi_snapshot_preview1 fd_sync
//go:noescape
func fd_sync(fd int32) Errno

//go:wasmimport wasi_snapshot_preview1 path_create_directory
//go:noescape
func path_create_directory(fd int32, path *byte, pathLen size) Errno

//go:wasmimport wasi_snapshot_preview1 path_filestat_get
//go:noescape
func path_filestat_get(fd int32, flags lookupflags, path *byte, pathLen size, buf unsafe.Pointer) Errno

//go:wasmimport wasi_snapshot_preview1 path_filestat_set_times
//go:noescape
func path_filestat_set_times(fd int32, flags lookupflags, path *byte, pathLen size, atim timestamp, mtim timestamp, fstflags fstflags) Errno

//go:wasmimport wasi_snapshot_preview1 path_link
//go:noescape
func path_link(oldFd int32, oldFlags lookupflags, oldPath *byte, oldPathLen size, newFd int32, newPath *byte, newPathLen size) Errno

//go:wasmimport wasi_snapshot_preview1 path_readlink
//go:noescape
func path_readlink(fd int32, path *byte, pathLen size, buf *byte, bufLen size, nwritten *size) Errno

//go:wasmimport wasi_snapshot_preview1 path_remove_directory
//go:noescape
func path_remove_directory(fd int32, path *byte, pathLen size) Errno

//go:wasmimport wasi_snapshot_preview1 path_rename
//go:noescape
func path_rename(oldFd int32, oldPath *byte, oldPathLen size, newFd int32, newPath *byte, newPathLen size) Errno

//go:wasmimport wasi_snapshot_preview1 path_symlink
//go:noescape
func path_symlink(oldPath *byte, oldPathLen size, fd int32, newPath *byte, newPathLen size) Errno

//go:wasmimport wasi_snapshot_preview1 path_unlink_file
//go:noescape
func path_unlink_file(fd int32, path *byte, pathLen size) Errno

//go:wasmimport wasi_snapshot_preview1 path_open
//go:noescape
func path_open(rootFD int32, dirflags lookupflags, path *byte, pathLen size, oflags oflags, fsRightsBase rights, fsRightsInheriting rights, fsFlags fdflags, fd *int32) Errno

//go:wasmimport wasi_snapshot_preview1 random_get
//go:noescape
func random_get(buf *byte, bufLen size) Errno

//go:wasmimport wasi_snapshot_preview1 fd_fdstat_get
//go:noescape
func fd_fdstat_get(fd int32, buf *fdstat) Errno

//go:wasmimport wasi_snapshot_preview1 fd_fdstat_set_flags
//go:noescape
func fd_fdstat_set_flags(fd int32, flags fdflags) Errno

//go:wasmimport wasi_snapshot_preview1 fd_prestat_get
//go:noescape
func fd_prestat_get(fd int32, prestat *prestat) Errno

//go:wasmimport wasi_snapshot_preview1 fd_prestat_dir_name
//go:noescape
func fd_prestat_dir_name(fd int32, path *byte, pathLen size) Errno
