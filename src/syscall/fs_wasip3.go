// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build wasip3

package syscall

import (
	"internal/byteorder"
	"sync"
	"unsafe"
)

// This file implements the WASI preview 1 file functions used by
// fs_wasip1.go on top of wasi:filesystem@0.3.0.
//
// File descriptors index a table of descriptor resources. The preopened
// directories come first, starting at 3. The functions that may take a while
// are called async-lowered, so a goroutine waiting for one parks instead of
// blocking the program (see runtime.p3Await). Reads and writes go through
// Component Model streams, and a file keeps its last stream open for as long
// as the next read or write continues where it left off.
//
// Memory that the host writes after an import call returns (results of
// async-lowered calls, stream buffers) must stay in place while the calling
// goroutine is parked, so it is always allocated in the heap, never on a
// goroutine stack, which may move.

// Implemented in the runtime.
func wasip3Await(h uint32) (event, payload uint32)
func wasip3AwaitCall(status uint32)
func wasip3CabiHold()
func wasip3CabiRelease()

// Canonical ABI stream and future result codes.
const (
	p3Blocked   = 0xffffffff
	p3Completed = 0
	p3Dropped   = 1
)

//go:wasmimport wasi:filesystem/preopens@0.3.0 get-directories
//go:noescape
func p3GetDirectories(list *[2]uint32)

//go:wasmimport wasi:filesystem/types@0.3.0 [resource-drop]descriptor
func p3DescriptorDrop(self uint32)

//go:wasmimport wasi:filesystem/types@0.3.0 [method]descriptor.read-via-stream
//go:noescape
func p3ReadViaStream(self uint32, offset uint64, result *[2]uint32)

//go:wasmimport wasi:filesystem/types@0.3.0 [method]descriptor.write-via-stream
func p3WriteViaStream(self, data uint32, offset uint64) uint32

//go:wasmimport wasi:filesystem/types@0.3.0 [method]descriptor.append-via-stream
func p3AppendViaStream(self, data uint32) uint32

//go:wasmimport wasi:filesystem/types@0.3.0 [method]descriptor.read-directory
//go:noescape
func p3ReadDirectory(self uint32, result *[2]uint32)

// Stream and future intrinsics. The types are structural, so the ones for
// read-via-stream and write-via-stream work for every stream<u8> and
// future<result<_, error-code>>.

//go:wasmimport wasi:filesystem/types@0.3.0 [stream-new-0][method]descriptor.write-via-stream
func p3BytesNew() uint64

//go:wasmimport wasi:filesystem/types@0.3.0 [async-lower][stream-write-0][method]descriptor.write-via-stream
func p3BytesWrite(writable uint32, buf unsafe.Pointer, n uint32) uint32

//go:wasmimport wasi:filesystem/types@0.3.0 [stream-drop-writable-0][method]descriptor.write-via-stream
func p3BytesDropWritable(writable uint32)

//go:wasmimport wasi:filesystem/types@0.3.0 [async-lower][stream-read-0][method]descriptor.read-via-stream
func p3BytesRead(readable uint32, buf unsafe.Pointer, n uint32) uint32

//go:wasmimport wasi:filesystem/types@0.3.0 [stream-drop-readable-0][method]descriptor.read-via-stream
func p3BytesDropReadable(readable uint32)

//go:wasmimport wasi:filesystem/types@0.3.0 [async-lower][future-read-1][method]descriptor.read-via-stream
func p3ResultRead(readable uint32, result unsafe.Pointer) uint32

//go:wasmimport wasi:filesystem/types@0.3.0 [future-drop-readable-1][method]descriptor.read-via-stream
func p3ResultDropReadable(readable uint32)

//go:wasmimport wasi:filesystem/types@0.3.0 [async-lower][stream-read-0][method]descriptor.read-directory
func p3EntriesRead(readable uint32, buf unsafe.Pointer, n uint32) uint32

//go:wasmimport wasi:filesystem/types@0.3.0 [stream-drop-readable-0][method]descriptor.read-directory
func p3EntriesDropReadable(readable uint32)

// Async-lowered descriptor methods. Each returns the subtask status, and
// writes its result to the given memory once it returns. Methods with more
// than four flattened parameters take them in memory.

//go:wasmimport wasi:filesystem/types@0.3.0 [async-lower][method]descriptor.stat
func p3Stat(self uint32, result unsafe.Pointer) uint32

//go:wasmimport wasi:filesystem/types@0.3.0 [async-lower][method]descriptor.stat-at
func p3StatAt(self, pathFlags uint32, path unsafe.Pointer, pathLen uint32, result unsafe.Pointer) uint32

//go:wasmimport wasi:filesystem/types@0.3.0 [async-lower][method]descriptor.metadata-hash
func p3MetadataHash(self uint32, result unsafe.Pointer) uint32

//go:wasmimport wasi:filesystem/types@0.3.0 [async-lower][method]descriptor.metadata-hash-at
func p3MetadataHashAt(self, pathFlags uint32, path unsafe.Pointer, pathLen uint32, result unsafe.Pointer) uint32

//go:wasmimport wasi:filesystem/types@0.3.0 [async-lower][method]descriptor.get-type
func p3GetType(self uint32, result unsafe.Pointer) uint32

//go:wasmimport wasi:filesystem/types@0.3.0 [async-lower][method]descriptor.set-size
func p3SetSize(self uint32, size uint64, result unsafe.Pointer) uint32

//go:wasmimport wasi:filesystem/types@0.3.0 [async-lower][method]descriptor.sync
func p3Sync(self uint32, result unsafe.Pointer) uint32

//go:wasmimport wasi:filesystem/types@0.3.0 [async-lower][method]descriptor.create-directory-at
func p3CreateDirectoryAt(self uint32, path unsafe.Pointer, pathLen uint32, result unsafe.Pointer) uint32

//go:wasmimport wasi:filesystem/types@0.3.0 [async-lower][method]descriptor.remove-directory-at
func p3RemoveDirectoryAt(self uint32, path unsafe.Pointer, pathLen uint32, result unsafe.Pointer) uint32

//go:wasmimport wasi:filesystem/types@0.3.0 [async-lower][method]descriptor.unlink-file-at
func p3UnlinkFileAt(self uint32, path unsafe.Pointer, pathLen uint32, result unsafe.Pointer) uint32

//go:wasmimport wasi:filesystem/types@0.3.0 [async-lower][method]descriptor.readlink-at
func p3ReadlinkAt(self uint32, path unsafe.Pointer, pathLen uint32, result unsafe.Pointer) uint32

//go:wasmimport wasi:filesystem/types@0.3.0 [async-lower][method]descriptor.open-at
func p3OpenAt(params, result unsafe.Pointer) uint32

//go:wasmimport wasi:filesystem/types@0.3.0 [async-lower][method]descriptor.set-times-at
func p3SetTimesAt(params, result unsafe.Pointer) uint32

//go:wasmimport wasi:filesystem/types@0.3.0 [async-lower][method]descriptor.link-at
func p3LinkAt(params, result unsafe.Pointer) uint32

//go:wasmimport wasi:filesystem/types@0.3.0 [async-lower][method]descriptor.rename-at
func p3RenameAt(params, result unsafe.Pointer) uint32

//go:wasmimport wasi:filesystem/types@0.3.0 [async-lower][method]descriptor.symlink-at
func p3SymlinkAt(params, result unsafe.Pointer) uint32

// Sizes of canonical ABI values, and offsets of the payload in results.
const (
	p3ResultSize     = 20 // result<T, error-code> where T fits in 16 bytes, align 4
	p3ResultPayload  = 4
	p3StatResultSize = 112 // result<descriptor-stat, error-code>
	p3HashResultSize = 24  // result<metadata-hash-value, error-code>
	p3WidePayload    = 8   // payload offset when T has 8-byte alignment
	p3EntrySize      = 24  // directory-entry
)

// p3Errnos maps error-code cases to errors. The last case, other, is EIO.
var p3Errnos = [...]Errno{
	EACCES, EALREADY, EBADF, EBUSY, EDEADLK, EDQUOT, EEXIST, EFBIG, EILSEQ,
	EINPROGRESS, EINTR, EINVAL, EIO, EISDIR, ELOOP, EMLINK, EMSGSIZE,
	ENAMETOOLONG, ENODEV, ENOENT, ENOLCK, ENOMEM, ENOSPC, ENOTDIR, ENOTEMPTY,
	ENOTRECOVERABLE, ENOTSUP, ENOTTY, ENXIO, EOVERFLOW, EPERM, EPIPE, EROFS,
	ESPIPE, ETXTBSY, EXDEV,
}

// p3Errno returns the error of the result<T, error-code> in b, whose payload
// is at offset off.
func p3Errno(b []byte, off int) Errno {
	if b[0] == 0 {
		return 0
	}
	if c := int(b[off]); c < len(p3Errnos) {
		return p3Errnos[c]
	}
	return EIO
}

func p3Filetype(t uint8) filetype {
	switch t {
	case 0:
		return FILETYPE_BLOCK_DEVICE
	case 1:
		return FILETYPE_CHARACTER_DEVICE
	case 2:
		return FILETYPE_DIRECTORY
	case 4:
		return FILETYPE_SYMBOLIC_LINK
	case 5:
		return FILETYPE_REGULAR_FILE
	case 6:
		return FILETYPE_SOCKET_STREAM
	}
	return FILETYPE_UNKNOWN
}

// p3Time returns the option<instant> in b as nanoseconds since the epoch.
func p3Time(b []byte) uint64 {
	if b[0] == 0 {
		return 0
	}
	return byteorder.LEUint64(b[8:])*1e9 + uint64(byteorder.LEUint32(b[16:]))
}

// p3PutTimestamp stores a new-timestamp in b.
func p3PutTimestamp(b []byte, t timestamp, set, now bool) {
	switch {
	case set:
		b[0] = 2
		byteorder.LEPutUint64(b[8:], t/1e9)
		byteorder.LEPutUint32(b[16:], uint32(t%1e9))
	case now:
		b[0] = 1
	}
}

// p3Ptr returns the address of b[0] as a canonical ABI pointer.
func p3Ptr(b []byte) uint32 {
	return uint32(uintptr(unsafe.Pointer(unsafe.SliceData(b))))
}

// A p3Call is heap memory for one import call: its parameters, if passed
// in memory, its results, and copies of its paths. The host may read the
// parameters after the call returns, if the callee starts later, and writes
// the results once it's done, so the memory must not be on the stack. Calls
// reuse it, to not allocate for each.
type p3Call struct {
	params [8]uint64  // 64 bytes, align 8
	res    [14]uint64 // 112 bytes
	hash   [3]uint64  // 24 bytes
	path   []byte
	path2  []byte
}

var p3Calls = sync.Pool{New: func() any { return new(p3Call) }}

func p3NewCall() *p3Call {
	return p3Calls.Get().(*p3Call)
}

func (c *p3Call) done() {
	if cap(c.path) > 1024 || cap(c.path2) > 1024 {
		c.path, c.path2 = nil, nil
	}
	p3Calls.Put(c)
}

// paramBuf returns n bytes of zeroed memory for parameters.
func (c *p3Call) paramBuf(n int) []byte {
	b := unsafe.Slice((*byte)(unsafe.Pointer(&c.params)), unsafe.Sizeof(c.params))[:n]
	clear(b)
	return b
}

// resBuf returns n bytes of zeroed memory for a result.
func (c *p3Call) resBuf(n int) []byte {
	b := unsafe.Slice((*byte)(unsafe.Pointer(&c.res)), unsafe.Sizeof(c.res))[:n]
	clear(b)
	return b
}

// hashBuf returns memory for a metadata-hash result.
func (c *p3Call) hashBuf() []byte {
	b := unsafe.Slice((*byte)(unsafe.Pointer(&c.hash)), unsafe.Sizeof(c.hash))
	clear(b)
	return b
}

// p3CopyPath copies a path into buf, reusing its memory, and returns the
// copy, which has room for at least one byte, so that it has an address.
func p3CopyPath(buf []byte, path *byte, pathLen size) []byte {
	if cap(buf) == 0 {
		buf = make([]byte, 0, max(64, pathLen))
	}
	return append(buf[:0], unsafe.Slice(path, pathLen)...)
}

func p3PathPtr(b []byte) unsafe.Pointer {
	return unsafe.Pointer(unsafe.SliceData(b[:cap(b)]))
}

// p3String returns a copy of a string that the host allocated.
func p3String(ptr, n uint32) string {
	return string(unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), n))
}

// p3AwaitResult reads a future<result<_, error-code>>.
func p3AwaitResult(future uint32) Errno {
	c := p3NewCall()
	defer c.done()
	res := c.resBuf(p3ResultSize)
	wasip3CabiHold()
	defer wasip3CabiRelease()
	code := p3ResultRead(future, unsafe.Pointer(&res[0]))
	if code == p3Blocked {
		_, code = wasip3Await(future)
	}
	if code&0xf != p3Completed {
		return 0
	}
	return p3Errno(res, p3ResultPayload)
}

// A p3File is an open descriptor.
type p3File struct {
	mu      sync.Mutex
	handle  uint32
	socket  bool // a socket that package net manages, with no descriptor
	append  bool
	preopen string // the name of a preopened directory
	offset  int64  // the position for fd_read, fd_write and fd_seek

	// A file has at most one open stream, either for reading (rs and rf, the
	// stream and its result) or for writing (ws and wf), with pos the offset
	// of the next byte it reads or writes.
	rs, rf  uint32
	ws, wf  uint32
	pos     int64
	wappend bool

	buf []byte // memory for stream reads and writes, from p3SmallBufs or p3Bufs

	dir *p3Dir // the directory listing for fd_readdir
}

// Stream buffers, reused by files once they're closed.
const (
	p3SmallBufSize = 4 << 10
	p3BufSize      = 64 << 10
)

var (
	p3SmallBufs = sync.Pool{New: func() any { return new([p3SmallBufSize]byte) }}
	p3Bufs      = sync.Pool{New: func() any { return new([p3BufSize]byte) }}
)

// A p3Dir is a directory listing, reused by files once they're closed.
type p3Dir struct {
	entries []p3Entry
	names   []byte // the entries' names, one after another
	buf     [64 * p3EntrySize]byte
}

type p3Entry struct {
	name uint32 // the end of the name in names
	typ  filetype
}

var p3Dirs = sync.Pool{New: func() any { return new(p3Dir) }}

var p3Files struct {
	once  sync.Once
	mu    sync.Mutex
	table []*p3File
}

func p3LoadPreopens() {
	p3Files.table = make([]*p3File, 3) // stdin, stdout, stderr
	var list [2]uint32
	wasip3CabiHold()
	p3GetDirectories(&list)
	for i := range list[1] {
		e := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(list[0]+i*12))), 12)
		p3Files.table = append(p3Files.table, &p3File{
			handle:  byteorder.LEUint32(e),
			preopen: p3String(byteorder.LEUint32(e[4:]), byteorder.LEUint32(e[8:])),
		})
	}
	wasip3CabiRelease()
}

// p3GetEntry returns the table entry of fd, which may be a socket.
func p3GetEntry(fd int32) (*p3File, Errno) {
	p3Files.once.Do(p3LoadPreopens)
	p3Files.mu.Lock()
	defer p3Files.mu.Unlock()
	if fd < 0 || int(fd) >= len(p3Files.table) || p3Files.table[fd] == nil {
		return nil, EBADF
	}
	return p3Files.table[fd], 0
}

// p3GetFile returns the descriptor of fd.
func p3GetFile(fd int32) (*p3File, Errno) {
	f, errno := p3GetEntry(fd)
	if errno == 0 && f.socket {
		return nil, ENOTSUP
	}
	return f, errno
}

// wasip3SocketFD allocates a file descriptor for a socket, for package net.
// Package net keeps the socket's resources; closing the file descriptor only
// frees it.
//
//go:linkname wasip3SocketFD
func wasip3SocketFD() int {
	return int(p3AddFile(&p3File{socket: true}))
}

// p3AddFile allocates the lowest free file descriptor for f.
func p3AddFile(f *p3File) int32 {
	p3Files.once.Do(p3LoadPreopens)
	p3Files.mu.Lock()
	defer p3Files.mu.Unlock()
	for fd := 3; fd < len(p3Files.table); fd++ {
		if p3Files.table[fd] == nil {
			p3Files.table[fd] = f
			return int32(fd)
		}
	}
	p3Files.table = append(p3Files.table, f)
	return int32(len(p3Files.table) - 1)
}

func isStdio(fd int32) bool {
	return fd == Stdin || fd == Stdout || fd == Stderr
}

// ioBuf returns heap memory for up to n bytes of stream I/O.
func (f *p3File) ioBuf(n int) []byte {
	n = min(n, p3BufSize)
	if len(f.buf) < n {
		f.freeBuf()
		if n <= p3SmallBufSize {
			f.buf = p3SmallBufs.Get().(*[p3SmallBufSize]byte)[:]
		} else {
			f.buf = p3Bufs.Get().(*[p3BufSize]byte)[:]
		}
	}
	return f.buf[:n]
}

// freeBuf returns the file's stream buffer for reuse. No stream operation
// may be pending.
func (f *p3File) freeBuf() {
	switch len(f.buf) {
	case p3SmallBufSize:
		p3SmallBufs.Put((*[p3SmallBufSize]byte)(f.buf))
	case p3BufSize:
		p3Bufs.Put((*[p3BufSize]byte)(f.buf))
	}
	f.buf = nil
}

// freeDir returns the file's directory listing for reuse, unless it's large.
func (f *p3File) freeDir() {
	if d := f.dir; d != nil && cap(d.names) <= 1<<20 {
		p3Dirs.Put(d)
	}
	f.dir = nil
}

// closeStream drops the open stream, if any.
func (f *p3File) closeStream() {
	if f.rs != 0 {
		p3BytesDropReadable(f.rs)
		p3ResultDropReadable(f.rf)
		f.rs, f.rf = 0, 0
	}
	if f.ws != 0 {
		p3BytesDropWritable(f.ws)
		p3ResultDropReadable(f.wf)
		f.ws, f.wf = 0, 0
	}
}

// readAt reads up to len(p) bytes at offset off. It returns 0 at the end of
// the file. f.mu must be held.
func (f *p3File) readAt(p []byte, off int64) (int, Errno) {
	if len(p) == 0 {
		return 0, 0
	}
	if f.ws != 0 || (f.rs != 0 && f.pos != off) {
		f.closeStream()
	}
	if f.rs == 0 {
		var pair [2]uint32
		p3ReadViaStream(f.handle, uint64(off), &pair)
		f.rs, f.rf, f.pos = pair[0], pair[1], off
	}
	buf := f.ioBuf(len(p))
	code := p3BytesRead(f.rs, unsafe.Pointer(&buf[0]), uint32(len(buf)))
	if code == p3Blocked {
		_, code = wasip3Await(f.rs)
	}
	n := copy(p, buf[:code>>4])
	f.pos += int64(n)
	if code&0xf == p3Dropped {
		// The end of the file, or an error. Don't keep the stream, so that
		// a later read sees data appended meanwhile.
		errno := p3AwaitResult(f.rf)
		f.closeStream()
		if n == 0 {
			return 0, errno
		}
	}
	return n, 0
}

// writeAt writes p at offset off, or at the end of the file if appending.
// f.mu must be held.
func (f *p3File) writeAt(p []byte, off int64, appending bool) (int, Errno) {
	if len(p) == 0 {
		return 0, 0
	}
	if f.rs != 0 || (f.ws != 0 && (f.wappend != appending || !appending && f.pos != off)) {
		f.closeStream()
	}
	if f.ws == 0 {
		pair := p3BytesNew()
		readable, writable := uint32(pair), uint32(pair>>32)
		if appending {
			f.wf = p3AppendViaStream(f.handle, readable)
		} else {
			f.wf = p3WriteViaStream(f.handle, readable, uint64(off))
		}
		f.ws, f.pos, f.wappend = writable, off, appending
	}
	written := 0
	for written < len(p) {
		buf := f.ioBuf(len(p) - written)
		copy(buf, p[written:])
		code := p3BytesWrite(f.ws, unsafe.Pointer(&buf[0]), uint32(len(buf)))
		if code == p3Blocked {
			_, code = wasip3Await(f.ws)
		}
		// The host only reports a write complete once the bytes are in the
		// file.
		written += int(code >> 4)
		f.pos += int64(code >> 4)
		if code&0xf == p3Dropped {
			errno := p3AwaitResult(f.wf)
			f.closeStream()
			if errno == 0 {
				errno = EIO
			}
			return written, errno
		}
	}
	return written, 0
}

// p3IOVecs converts iovs to slices. Their addresses must be converted before
// the goroutine parks, since they may point into its stack.
func p3IOVecs(iovs *iovec, iovsLen size, bufs *[16][]byte) [][]byte {
	n := 0
	for _, iov := range unsafe.Slice(iovs, iovsLen) {
		if iov.bufLen == 0 {
			continue
		}
		if n == len(bufs) {
			break
		}
		bufs[n] = unsafe.Slice((*byte)(unsafe.Pointer(uintptr(iov.buf))), iov.bufLen)
		n++
	}
	return bufs[:n]
}

func fd_write(fd int32, iovs *iovec, iovsLen size, nwritten *size) Errno {
	var bufs [16][]byte
	vecs := p3IOVecs(iovs, iovsLen, &bufs)
	*nwritten = 0
	if isStdio(fd) {
		if fd == Stdin {
			return EBADF
		}
		for _, b := range vecs {
			written, broken := wasip3Write(fd, unsafe.Pointer(&b[0]), int32(len(b)))
			*nwritten += size(written)
			if broken {
				return EPIPE
			}
		}
		return 0
	}
	f, errno := p3GetFile(fd)
	if errno != 0 {
		return errno
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, b := range vecs {
		n, errno := f.writeAt(b, f.offset, f.append)
		f.offset += int64(n)
		*nwritten += size(n)
		if errno != 0 {
			return errno
		}
	}
	return 0
}

func fd_pwrite(fd int32, iovs *iovec, iovsLen size, offset filesize, nwritten *size) Errno {
	var bufs [16][]byte
	vecs := p3IOVecs(iovs, iovsLen, &bufs)
	*nwritten = 0
	if isStdio(fd) {
		return ESPIPE
	}
	f, errno := p3GetFile(fd)
	if errno != 0 {
		return errno
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, b := range vecs {
		n, errno := f.writeAt(b, int64(offset)+int64(*nwritten), false)
		*nwritten += size(n)
		if errno != 0 {
			return errno
		}
	}
	return 0
}

func fd_read(fd int32, iovs *iovec, iovsLen size, nread *size) Errno {
	var bufs [16][]byte
	vecs := p3IOVecs(iovs, iovsLen, &bufs)
	*nread = 0
	if len(vecs) == 0 {
		return 0
	}
	// Like read(2), return as soon as some bytes are available.
	b := vecs[0]
	if isStdio(fd) {
		if fd != Stdin {
			return EBADF
		}
		*nread = size(wasip3Read(unsafe.Pointer(&b[0]), int32(len(b))))
		return 0
	}
	f, errno := p3GetFile(fd)
	if errno != 0 {
		return errno
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	n, errno := f.readAt(b, f.offset)
	f.offset += int64(n)
	*nread = size(n)
	return errno
}

func fd_pread(fd int32, iovs *iovec, iovsLen size, offset filesize, nread *size) Errno {
	var bufs [16][]byte
	vecs := p3IOVecs(iovs, iovsLen, &bufs)
	*nread = 0
	if isStdio(fd) {
		return ESPIPE
	}
	f, errno := p3GetFile(fd)
	if errno != 0 {
		return errno
	}
	if len(vecs) == 0 {
		return 0
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	n, errno := f.readAt(vecs[0], int64(offset))
	*nread = size(n)
	return errno
}

func fd_seek(fd int32, offset filedelta, whence uint32, newoffset *filesize) Errno {
	if isStdio(fd) {
		return ESPIPE
	}
	f, errno := p3GetFile(fd)
	if errno != 0 {
		return errno
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var base int64
	switch whence {
	case WHENCE_SET:
	case WHENCE_CUR:
		base = f.offset
	case WHENCE_END:
		var st Stat_t
		if errno := p3StatInto(&st, f.handle, false, 0, nil, 0); errno != 0 {
			return errno
		}
		base = int64(st.Size)
	default:
		return EINVAL
	}
	if base+offset < 0 {
		return EINVAL
	}
	f.offset = base + offset
	*newoffset = filesize(f.offset)
	return 0
}

func fd_close(fd int32) Errno {
	if isStdio(fd) {
		return 0
	}
	f, errno := p3GetEntry(fd)
	if errno != 0 {
		return errno
	}
	p3Files.mu.Lock()
	p3Files.table[fd] = nil
	p3Files.mu.Unlock()
	if f.socket {
		return 0
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closeStream()
	p3DescriptorDrop(f.handle)
	f.freeBuf()
	f.freeDir()
	return 0
}

func fd_fdstat_get(fd int32, buf *fdstat) Errno {
	*buf = fdstat{}
	if isStdio(fd) {
		buf.filetype = FILETYPE_CHARACTER_DEVICE
		if fd == Stdin {
			buf.rightsBase = RIGHT_FD_READ
		} else {
			buf.rightsBase = RIGHT_FD_WRITE
		}
		return 0
	}
	f, errno := p3GetEntry(fd)
	if errno != 0 {
		return errno
	}
	if f.socket {
		buf.filetype = FILETYPE_SOCKET_STREAM
		buf.rightsBase = fullRights
		return 0
	}
	c := p3NewCall()
	defer c.done()
	res := c.resBuf(p3ResultSize)
	wasip3CabiHold()
	defer wasip3CabiRelease()
	wasip3AwaitCall(p3GetType(f.handle, unsafe.Pointer(&res[0])))
	if errno := p3Errno(res, p3ResultPayload); errno != 0 {
		return errno
	}
	buf.filetype = p3Filetype(res[p3ResultPayload])
	if f.append {
		buf.fdflags = FDFLAG_APPEND
	}
	buf.rightsBase = fullRights
	buf.rightsInheriting = fullRights
	return 0
}

func fd_fdstat_set_flags(fd int32, flags fdflags) Errno {
	if _, errno := p3GetFile(fd); errno != 0 && !isStdio(fd) {
		return errno
	}
	return ENOSYS
}

func fd_fdstat_set_rights(fd int32, rightsBase rights, rightsInheriting rights) Errno {
	return ENOSYS
}

func fd_prestat_get(fd int32, prestat *prestat) Errno {
	f, errno := p3GetEntry(fd)
	if errno != 0 {
		return errno
	}
	if f.preopen == "" {
		return ENOTDIR
	}
	prestat.typ = preopentypeDir
	prestat.dir.prNameLen = size(len(f.preopen))
	return 0
}

func fd_prestat_dir_name(fd int32, path *byte, pathLen size) Errno {
	f, errno := p3GetFile(fd)
	if errno != 0 {
		return errno
	}
	if f.preopen == "" {
		return ENOTDIR
	}
	copy(unsafe.Slice(path, pathLen), f.preopen)
	return 0
}

// p3StatInto stats a descriptor, or a path relative to it if at is set, into
// st.
func p3StatInto(st *Stat_t, self uint32, at bool, flags lookupflags, path *byte, pathLen size) Errno {
	c := p3NewCall()
	defer c.done()
	res := c.resBuf(p3StatResultSize)
	hash := c.hashBuf()
	wasip3CabiHold()
	defer wasip3CabiRelease()
	// Start both calls before waiting for either.
	var s1, s2 uint32
	if at {
		c.path = p3CopyPath(c.path, path, pathLen)
		s1 = p3StatAt(self, flags, p3PathPtr(c.path), pathLen, unsafe.Pointer(&res[0]))
		s2 = p3MetadataHashAt(self, flags, p3PathPtr(c.path), pathLen, unsafe.Pointer(&hash[0]))
	} else {
		s1 = p3Stat(self, unsafe.Pointer(&res[0]))
		s2 = p3MetadataHash(self, unsafe.Pointer(&hash[0]))
	}
	wasip3AwaitCall(s1)
	wasip3AwaitCall(s2)
	if errno := p3Errno(res, p3WidePayload); errno != 0 {
		return errno
	}
	*st = Stat_t{
		Filetype: p3Filetype(res[8]),
		Nlink:    byteorder.LEUint64(res[24:]),
		Size:     byteorder.LEUint64(res[32:]),
		Atime:    p3Time(res[40:]),
		Mtime:    p3Time(res[64:]),
		Ctime:    p3Time(res[88:]),
	}
	if p3Errno(hash, p3WidePayload) == 0 {
		st.Ino = byteorder.LEUint64(hash[8:])
		st.Dev = byteorder.LEUint64(hash[16:])
	}
	return 0
}

func fd_filestat_get(fd int32, buf unsafe.Pointer) Errno {
	st := (*Stat_t)(buf)
	if isStdio(fd) {
		*st = Stat_t{Filetype: FILETYPE_CHARACTER_DEVICE}
		return 0
	}
	f, errno := p3GetEntry(fd)
	if errno != 0 {
		return errno
	}
	if f.socket {
		*st = Stat_t{Filetype: FILETYPE_SOCKET_STREAM}
		return 0
	}
	return p3StatInto(st, f.handle, false, 0, nil, 0)
}

//go:linkname path_filestat_get
func path_filestat_get(fd int32, flags lookupflags, path *byte, pathLen size, buf unsafe.Pointer) Errno {
	f, errno := p3GetFile(fd)
	if errno != 0 {
		return errno
	}
	return p3StatInto((*Stat_t)(buf), f.handle, true, flags, path, pathLen)
}

func fd_filestat_set_size(fd int32, set_size filesize) Errno {
	if isStdio(fd) {
		return EINVAL
	}
	f, errno := p3GetFile(fd)
	if errno != 0 {
		return errno
	}
	c := p3NewCall()
	defer c.done()
	res := c.resBuf(p3ResultSize)
	wasip3CabiHold()
	defer wasip3CabiRelease()
	wasip3AwaitCall(p3SetSize(f.handle, uint64(set_size), unsafe.Pointer(&res[0])))
	return p3Errno(res, p3ResultPayload)
}

func fd_sync(fd int32) Errno {
	if isStdio(fd) {
		return EINVAL
	}
	f, errno := p3GetFile(fd)
	if errno != 0 {
		return errno
	}
	c := p3NewCall()
	defer c.done()
	res := c.resBuf(p3ResultSize)
	wasip3CabiHold()
	defer wasip3CabiRelease()
	wasip3AwaitCall(p3Sync(f.handle, unsafe.Pointer(&res[0])))
	return p3Errno(res, p3ResultPayload)
}

func fd_readdir(fd int32, buf *byte, bufLen size, cookie dircookie, nwritten *size) Errno {
	*nwritten = 0
	if isStdio(fd) {
		return ENOTDIR
	}
	f, errno := p3GetFile(fd)
	if errno != 0 {
		return errno
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if cookie == 0 || f.dir == nil {
		if f.dir == nil {
			f.dir = p3Dirs.Get().(*p3Dir)
		}
		if errno := f.dir.read(f.handle); errno != 0 {
			f.freeDir()
			return errno
		}
	}
	// Write as many entries as fit, truncating the last one like
	// preview 1 implementations do.
	d := f.dir
	out := unsafe.Slice(buf, bufLen)
	n := 0
	for i := int(cookie); i < len(d.entries) && n < len(out); i++ {
		e := &d.entries[i]
		start := uint32(0)
		if i > 0 {
			start = d.entries[i-1].name
		}
		name := d.names[start:e.name]
		var hdr [unsafe.Sizeof(Dirent{}) - unsafe.Sizeof(Dirent{}.Name)]byte
		byteorder.LEPutUint64(hdr[unsafe.Offsetof(Dirent{}.Next):], uint64(i+1))
		byteorder.LEPutUint32(hdr[unsafe.Offsetof(Dirent{}.Namlen):], uint32(len(name)))
		hdr[unsafe.Offsetof(Dirent{}.Type)] = e.typ
		n += copy(out[n:], hdr[:])
		n += copy(out[n:], name)
	}
	*nwritten = size(n)
	return 0
}

// read lists a directory into d, replacing its previous listing.
func (d *p3Dir) read(self uint32) Errno {
	d.entries, d.names = d.entries[:0], d.names[:0]
	var pair [2]uint32
	p3ReadDirectory(self, &pair)
	stream, future := pair[0], pair[1]
	defer p3EntriesDropReadable(stream)
	defer p3ResultDropReadable(future)

	wasip3CabiHold()
	defer wasip3CabiRelease()
	for {
		code := p3EntriesRead(stream, unsafe.Pointer(&d.buf[0]), uint32(len(d.buf)/p3EntrySize))
		if code == p3Blocked {
			_, code = wasip3Await(stream)
		}
		for i := range int(code >> 4) {
			e := d.buf[i*p3EntrySize:]
			ptr, n := byteorder.LEUint32(e[16:]), byteorder.LEUint32(e[20:])
			d.names = append(d.names, unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), n)...)
			d.entries = append(d.entries, p3Entry{
				name: uint32(len(d.names)),
				typ:  p3Filetype(e[0]),
			})
		}
		if code&0xf == p3Dropped {
			break
		}
	}
	return p3AwaitResult(future)
}

//go:linkname path_open
func path_open(rootFD int32, dirflags lookupflags, path *byte, pathLen size, oflags oflags, fsRightsBase rights, fsRightsInheriting rights, fsFlags fdflags, fd *int32) Errno {
	dir, errno := p3GetFile(rootFD)
	if errno != 0 {
		return errno
	}
	var flags uint8
	if fsRightsBase&(RIGHT_FD_READ|RIGHT_FD_READDIR) != 0 {
		flags |= 1 << 0 // read
	}
	if fsRightsBase&RIGHT_FD_WRITE != 0 {
		flags |= 1 << 1 // write
	}
	c := p3NewCall()
	defer c.done()
	c.path = p3CopyPath(c.path, path, pathLen)
	params := c.paramBuf(20)
	byteorder.LEPutUint32(params[0:], dir.handle)
	params[4] = uint8(dirflags)
	byteorder.LEPutUint32(params[8:], p3Ptr(c.path[:cap(c.path)]))
	byteorder.LEPutUint32(params[12:], pathLen)
	params[16] = uint8(oflags) // the open-flags bits match preview 1
	params[17] = flags
	res := c.resBuf(p3ResultSize)
	wasip3CabiHold()
	defer wasip3CabiRelease()
	wasip3AwaitCall(p3OpenAt(unsafe.Pointer(&params[0]), unsafe.Pointer(&res[0])))
	if errno := p3Errno(res, p3ResultPayload); errno != 0 {
		return errno
	}
	*fd = p3AddFile(&p3File{
		handle: byteorder.LEUint32(res[p3ResultPayload:]),
		append: fsFlags&FDFLAG_APPEND != 0,
	})
	return 0
}

// p3PathCall calls a descriptor method that takes a path and returns
// result<_, error-code>.
func p3PathCall(fd int32, path *byte, pathLen size, method int) Errno {
	dir, errno := p3GetFile(fd)
	if errno != 0 {
		return errno
	}
	c := p3NewCall()
	defer c.done()
	c.path = p3CopyPath(c.path, path, pathLen)
	p := p3PathPtr(c.path)
	res := c.resBuf(p3ResultSize)
	wasip3CabiHold()
	defer wasip3CabiRelease()
	var status uint32
	switch method {
	case p3CreateDirectory:
		status = p3CreateDirectoryAt(dir.handle, p, pathLen, unsafe.Pointer(&res[0]))
	case p3RemoveDirectory:
		status = p3RemoveDirectoryAt(dir.handle, p, pathLen, unsafe.Pointer(&res[0]))
	case p3UnlinkFile:
		status = p3UnlinkFileAt(dir.handle, p, pathLen, unsafe.Pointer(&res[0]))
	}
	wasip3AwaitCall(status)
	return p3Errno(res, p3ResultPayload)
}

const (
	p3CreateDirectory = iota
	p3RemoveDirectory
	p3UnlinkFile
)

//go:linkname path_create_directory
func path_create_directory(fd int32, path *byte, pathLen size) Errno {
	return p3PathCall(fd, path, pathLen, p3CreateDirectory)
}

//go:linkname path_remove_directory
func path_remove_directory(fd int32, path *byte, pathLen size) Errno {
	return p3PathCall(fd, path, pathLen, p3RemoveDirectory)
}

//go:linkname path_unlink_file
func path_unlink_file(fd int32, path *byte, pathLen size) Errno {
	return p3PathCall(fd, path, pathLen, p3UnlinkFile)
}

//go:linkname path_readlink
func path_readlink(fd int32, path *byte, pathLen size, buf *byte, bufLen size, nwritten *size) Errno {
	*nwritten = 0
	dir, errno := p3GetFile(fd)
	if errno != 0 {
		return errno
	}
	c := p3NewCall()
	defer c.done()
	c.path = p3CopyPath(c.path, path, pathLen)
	res := c.resBuf(p3ResultSize)
	wasip3CabiHold()
	defer wasip3CabiRelease()
	wasip3AwaitCall(p3ReadlinkAt(dir.handle, p3PathPtr(c.path), pathLen, unsafe.Pointer(&res[0])))
	if errno := p3Errno(res, p3ResultPayload); errno != 0 {
		return errno
	}
	link := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(byteorder.LEUint32(res[4:])))), byteorder.LEUint32(res[8:]))
	// A result as long as buf tells os.Readlink to try a larger buffer.
	*nwritten = size(copy(unsafe.Slice(buf, bufLen), link))
	return 0
}

//go:linkname path_filestat_set_times
func path_filestat_set_times(fd int32, flags lookupflags, path *byte, pathLen size, atim timestamp, mtim timestamp, fstflags fstflags) Errno {
	dir, errno := p3GetFile(fd)
	if errno != 0 {
		return errno
	}
	c := p3NewCall()
	defer c.done()
	c.path = p3CopyPath(c.path, path, pathLen)
	params := c.paramBuf(64)
	byteorder.LEPutUint32(params[0:], dir.handle)
	params[4] = uint8(flags)
	byteorder.LEPutUint32(params[8:], p3Ptr(c.path[:cap(c.path)]))
	byteorder.LEPutUint32(params[12:], pathLen)
	p3PutTimestamp(params[16:], atim, fstflags&FILESTAT_SET_ATIM != 0, fstflags&FILESTAT_SET_ATIM_NOW != 0)
	p3PutTimestamp(params[40:], mtim, fstflags&FILESTAT_SET_MTIM != 0, fstflags&FILESTAT_SET_MTIM_NOW != 0)
	res := c.resBuf(p3ResultSize)
	wasip3CabiHold()
	defer wasip3CabiRelease()
	wasip3AwaitCall(p3SetTimesAt(unsafe.Pointer(&params[0]), unsafe.Pointer(&res[0])))
	return p3Errno(res, p3ResultPayload)
}

//go:linkname path_link
func path_link(oldFd int32, oldFlags lookupflags, oldPath *byte, oldPathLen size, newFd int32, newPath *byte, newPathLen size) Errno {
	oldDir, errno := p3GetFile(oldFd)
	if errno != 0 {
		return errno
	}
	newDir, errno := p3GetFile(newFd)
	if errno != 0 {
		return errno
	}
	c := p3NewCall()
	defer c.done()
	c.path = p3CopyPath(c.path, oldPath, oldPathLen)
	c.path2 = p3CopyPath(c.path2, newPath, newPathLen)
	params := c.paramBuf(28)
	byteorder.LEPutUint32(params[0:], oldDir.handle)
	params[4] = uint8(oldFlags)
	byteorder.LEPutUint32(params[8:], p3Ptr(c.path[:cap(c.path)]))
	byteorder.LEPutUint32(params[12:], oldPathLen)
	byteorder.LEPutUint32(params[16:], newDir.handle)
	byteorder.LEPutUint32(params[20:], p3Ptr(c.path2[:cap(c.path2)]))
	byteorder.LEPutUint32(params[24:], newPathLen)
	res := c.resBuf(p3ResultSize)
	wasip3CabiHold()
	defer wasip3CabiRelease()
	wasip3AwaitCall(p3LinkAt(unsafe.Pointer(&params[0]), unsafe.Pointer(&res[0])))
	return p3Errno(res, p3ResultPayload)
}

//go:linkname path_rename
func path_rename(oldFd int32, oldPath *byte, oldPathLen size, newFd int32, newPath *byte, newPathLen size) Errno {
	oldDir, errno := p3GetFile(oldFd)
	if errno != 0 {
		return errno
	}
	newDir, errno := p3GetFile(newFd)
	if errno != 0 {
		return errno
	}
	c := p3NewCall()
	defer c.done()
	c.path = p3CopyPath(c.path, oldPath, oldPathLen)
	c.path2 = p3CopyPath(c.path2, newPath, newPathLen)
	params := c.paramBuf(24)
	byteorder.LEPutUint32(params[0:], oldDir.handle)
	byteorder.LEPutUint32(params[4:], p3Ptr(c.path[:cap(c.path)]))
	byteorder.LEPutUint32(params[8:], oldPathLen)
	byteorder.LEPutUint32(params[12:], newDir.handle)
	byteorder.LEPutUint32(params[16:], p3Ptr(c.path2[:cap(c.path2)]))
	byteorder.LEPutUint32(params[20:], newPathLen)
	res := c.resBuf(p3ResultSize)
	wasip3CabiHold()
	defer wasip3CabiRelease()
	wasip3AwaitCall(p3RenameAt(unsafe.Pointer(&params[0]), unsafe.Pointer(&res[0])))
	return p3Errno(res, p3ResultPayload)
}

//go:linkname path_symlink
func path_symlink(oldPath *byte, oldPathLen size, fd int32, newPath *byte, newPathLen size) Errno {
	dir, errno := p3GetFile(fd)
	if errno != 0 {
		return errno
	}
	c := p3NewCall()
	defer c.done()
	c.path = p3CopyPath(c.path, oldPath, oldPathLen)
	c.path2 = p3CopyPath(c.path2, newPath, newPathLen)
	params := c.paramBuf(20)
	byteorder.LEPutUint32(params[0:], dir.handle)
	byteorder.LEPutUint32(params[4:], p3Ptr(c.path[:cap(c.path)]))
	byteorder.LEPutUint32(params[8:], oldPathLen)
	byteorder.LEPutUint32(params[12:], p3Ptr(c.path2[:cap(c.path2)]))
	byteorder.LEPutUint32(params[16:], newPathLen)
	res := c.resBuf(p3ResultSize)
	wasip3CabiHold()
	defer wasip3CabiRelease()
	wasip3AwaitCall(p3SymlinkAt(unsafe.Pointer(&params[0]), unsafe.Pointer(&res[0])))
	return p3Errno(res, p3ResultPayload)
}
