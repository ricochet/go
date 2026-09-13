// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build wasip3

package runtime

import (
	"structs"
	"unsafe"
)

// GOOS=wasip3 targets the wasi:cli/command@0.3.0 world of the WebAssembly
// Component Model. It implies the wasip1 build tag: the rest of the runtime
// and the syscall package share the GOOS=wasip1 code, while this file (and
// netpoll_wasip3.go, and the *_wasip3.go files in syscall) implement the WASI
// preview 1 functions that code uses on top of WASI 0.3.
//
// The linker exports _rt0_wasm_wasip1 as wasi:cli/run.run, lifted as a
// stackful async function, so the program's thread may block. I/O and sleeps
// don't block other goroutines, though: a goroutine waiting on a stream parks
// (see p3Await), and when no goroutine is runnable the scheduler waits for
// any stream event or timer in netpoll.
//
// This is a prototype covering stdio, files, sockets, clocks, sleeping,
// arguments, the environment, random numbers and exiting. The syscall package
// implements files on wasi:filesystem, waiting for its import calls and
// streams with syscall_wasip3AwaitCall and syscall_wasip3Await. Package net
// implements TCP and UDP on wasi:sockets, waiting in poll descriptors (see
// netpoll_wasip3.go). Other WASI preview 1 functions report ENOSYS.

// Canonical ABI return codes of stream and future operations.
const (
	p3Blocked   = 0xffffffff
	p3Completed = 0
	p3Dropped   = 1
	p3Cancelled = 2
)

// Canonical ABI event codes.
const (
	p3EventNone        = 0
	p3EventSubtask     = 1
	p3EventStreamRead  = 2
	p3EventStreamWrite = 3
	p3EventFutureRead  = 4
	p3EventFutureWrite = 5
)

// Canonical ABI subtask states.
const (
	p3SubtaskStarting = 0
	p3SubtaskStarted  = 1
	p3SubtaskReturned = 2
)

//go:wasmimport $root [waitable-set-new]
func p3WaitableSetNew() uint32

//go:wasmimport $root [waitable-set-wait]
//go:noescape
func p3WaitableSetWait(set uint32, event *[2]uint32) uint32

//go:wasmimport $root [waitable-set-poll]
//go:noescape
func p3WaitableSetPoll(set uint32, event *[2]uint32) uint32

//go:wasmimport $root [waitable-join]
func p3WaitableJoin(waitable, set uint32)

//go:wasmimport $root [subtask-drop]
func p3SubtaskDrop(subtask uint32)

//go:wasmimport wasi:cli/exit@0.3.0 exit
func p3Exit(status uint32)

//go:wasmimport wasi:cli/exit@0.3.0 exit-with-code
func p3ExitWithCode(code uint32)

//go:wasmimport wasi:cli/environment@0.3.0 get-arguments
//go:noescape
func p3GetArguments(list *[2]uint32)

//go:wasmimport wasi:cli/environment@0.3.0 get-environment
//go:noescape
func p3GetEnvironment(list *[2]uint32)

//go:wasmimport wasi:clocks/monotonic-clock@0.3.0 now
func p3MonotonicNow() uint64

//go:wasmimport wasi:clocks/monotonic-clock@0.3.0 [async-lower]wait-for
func p3WaitFor(nanoseconds uint64) uint32

type p3Instant struct {
	_           structs.HostLayout
	seconds     int64
	nanoseconds uint32
}

//go:wasmimport wasi:clocks/system-clock@0.3.0 now
//go:noescape
func p3SystemNow(instant *p3Instant)

//go:wasmimport wasi:random/random@0.3.0 get-random-u64
func p3RandomU64() uint64

//go:wasmimport wasi:cli/stdout@0.3.0 [stream-new-0]write-via-stream
func p3StdoutStreamNew() uint64

//go:wasmimport wasi:cli/stdout@0.3.0 write-via-stream
func p3StdoutWriteViaStream(readable uint32) uint32

//go:wasmimport wasi:cli/stdout@0.3.0 [async-lower][stream-write-0]write-via-stream
func p3StdoutStreamWrite(writable uint32, buf unsafe.Pointer, n uint32) uint32

//go:wasmimport wasi:cli/stderr@0.3.0 [stream-new-0]write-via-stream
func p3StderrStreamNew() uint64

//go:wasmimport wasi:cli/stderr@0.3.0 write-via-stream
func p3StderrWriteViaStream(readable uint32) uint32

//go:wasmimport wasi:cli/stderr@0.3.0 [async-lower][stream-write-0]write-via-stream
func p3StderrStreamWrite(writable uint32, buf unsafe.Pointer, n uint32) uint32

//go:wasmimport wasi:cli/stdin@0.3.0 read-via-stream
//go:noescape
func p3StdinReadViaStream(result *[2]uint32)

//go:wasmimport wasi:cli/stdin@0.3.0 [async-lower][stream-read-0]read-via-stream
func p3StdinStreamRead(readable uint32, buf unsafe.Pointer, n uint32) uint32

// sched_yield is used by notetsleepg (lock_wasip1.go).
func sched_yield() errno {
	return 0
}

func exit(code int32) {
	if code == 0 {
		p3Exit(0) // ok
	}
	p3ExitWithCode(uint32(uint8(code)))
}

// A p3OutStream is a stream to stdout or stderr. The bytes being written are
// copied into buf, which stays in place until the host has consumed them.
type p3OutStream struct {
	writable uint32 // 0 until the stream is created
	sema     uint32 // serializes writes (a stream allows one at a time)
	dropped  bool
	buf      [16 << 10]byte
}

// The runtime's own output (print, panics) does not share streams with the
// syscall package, since it cannot wait for another write to finish.
var p3Stdout, p3Stderr, p3RuntimeStdout, p3RuntimeStderr p3OutStream

func init() {
	p3Stdout.sema = 1
	p3Stderr.sema = 1
}

func (s *p3OutStream) start(fd int32) {
	var pair uint64
	if fd == 1 {
		pair = p3StdoutStreamNew()
		p3StdoutWriteViaStream(uint32(pair))
	} else {
		pair = p3StderrStreamNew()
		p3StderrWriteViaStream(uint32(pair))
	}
	s.writable = uint32(pair >> 32)
}

// write writes p to the stream, parking the calling goroutine while the host
// is not ready to take more bytes. It reports whether it wrote all of p.
func (s *p3OutStream) write(fd int32, p unsafe.Pointer, n int32) (int32, bool) {
	written := int32(0)
	for written < n && !s.dropped {
		code := s.writeChunk(fd, add(p, uintptr(written)), n-written)
		if code == p3Blocked {
			_, code = p3Await(s.writable)
		}
		written += s.written(code)
	}
	return written, written == n
}

// writeBlocking is like write, but waits without the scheduler, blocking
// the whole program.
//
//go:nowritebarrierrec
func (s *p3OutStream) writeBlocking(fd int32, p unsafe.Pointer, n int32) int32 {
	written := int32(0)
	for written < n && !s.dropped {
		code := s.writeChunk(fd, add(p, uintptr(written)), n-written)
		if code == p3Blocked {
			code = p3BlockOn(s.writable)
		}
		written += s.written(code)
	}
	return written
}

// writeChunk starts writing a prefix of p and returns the canonical ABI
// result code.
//
//go:nowritebarrierrec
func (s *p3OutStream) writeChunk(fd int32, p unsafe.Pointer, n int32) uint32 {
	if s.writable == 0 {
		s.start(fd)
	}
	chunk := min(n, int32(len(s.buf)))
	memmove(unsafe.Pointer(&s.buf[0]), p, uintptr(chunk))
	if fd == 1 {
		return p3StdoutStreamWrite(s.writable, unsafe.Pointer(&s.buf[0]), uint32(chunk))
	}
	return p3StderrStreamWrite(s.writable, unsafe.Pointer(&s.buf[0]), uint32(chunk))
}

// written returns the number of bytes written according to code.
//
//go:nowritebarrierrec
func (s *p3OutStream) written(code uint32) int32 {
	if code&0xf == p3Dropped {
		s.dropped = true
	}
	return int32(code >> 4)
}

func write1(fd uintptr, p unsafe.Pointer, n int32) int32 {
	var s *p3OutStream
	switch fd {
	case 1:
		s = &p3RuntimeStdout
	case 2:
		s = &p3RuntimeStderr
	default:
		return -1
	}
	return s.writeBlocking(int32(fd), p, n)
}

// p3In is the stdin stream.
var p3In struct {
	started  bool
	readable uint32
	sema     uint32
	eof      bool
	buf      [16 << 10]byte
}

func init() {
	p3In.sema = 1
}

// syscall_wasip3Write writes to stdout (fd 1) or stderr (fd 2) for the
// syscall package. It reports the number of bytes written and whether the
// reader of the stream (the host) went away.
//
//go:linkname syscall_wasip3Write syscall.wasip3Write
func syscall_wasip3Write(fd int32, p unsafe.Pointer, n int32) (written int32, broken bool) {
	var s *p3OutStream
	switch fd {
	case 1:
		s = &p3Stdout
	case 2:
		s = &p3Stderr
	default:
		return 0, true
	}
	semacquire(&s.sema)
	written, ok := s.write(fd, p, n)
	semrelease(&s.sema)
	return written, !ok
}

// syscall_wasip3Read reads from stdin for the syscall package. It returns 0
// at the end of input.
//
//go:linkname syscall_wasip3Read syscall.wasip3Read
func syscall_wasip3Read(p unsafe.Pointer, n int32) int32 {
	semacquire(&p3In.sema)
	defer semrelease(&p3In.sema)
	if !p3In.started {
		var result [2]uint32
		p3StdinReadViaStream(&result)
		p3In.readable = result[0]
		p3In.started = true
	}
	if p3In.eof || n == 0 {
		return 0
	}
	chunk := min(n, int32(len(p3In.buf)))
	code := p3StdinStreamRead(p3In.readable, unsafe.Pointer(&p3In.buf[0]), uint32(chunk))
	if code == p3Blocked {
		_, code = p3Await(p3In.readable)
	}
	read := int32(code >> 4)
	memmove(p, unsafe.Pointer(&p3In.buf[0]), uintptr(read))
	if code&0xf == p3Dropped && read == 0 {
		p3In.eof = true
	}
	return read
}

// p3BlockSet is a waitable set for waiting without the scheduler.
var p3BlockSet uint32

// p3BlockOn waits for an event on waitable h, blocking the whole program,
// and returns the event's payload.
//
// In a library, this blocks inside a call of a callback-lifted export (see
// netpoll_wasip3.go), during which the host neither starts other export calls
// nor delivers events to other tasks, so Go is not reentered. The same holds
// for other import calls that block, such as socket binds and cancellations.
func p3BlockOn(h uint32) uint32 {
	if p3BlockSet == 0 {
		p3BlockSet = p3WaitableSetNew()
	}
	p3WaitableJoin(h, p3BlockSet)
	var event [2]uint32
	for {
		p3WaitableSetWait(p3BlockSet, &event)
		if event[0] == h {
			p3WaitableJoin(h, 0)
			return event[1]
		}
	}
}

func usleep(usec uint32) {
	status := p3WaitFor(uint64(usec) * 1e3)
	if status&0xf != p3SubtaskReturned {
		subtask := status >> 4
		for p3BlockOn(subtask) != p3SubtaskReturned {
		}
		p3SubtaskDrop(subtask)
	}
}

func readRandom(r []byte) int {
	for i := 0; i < len(r); i += 8 {
		v := p3RandomU64()
		for j := 0; j < 8 && i+j < len(r); j++ {
			r[i+j] = byte(v >> (8 * j))
		}
	}
	return len(r)
}

// p3String returns a copy of the string at ptr, which the host allocated
// with cabi_realloc.
func p3String(ptr, n uint32) string {
	return string(unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), n))
}

func goenvs() {
	var list [2]uint32
	p3GetArguments(&list)
	argslice = make([]string, list[1])
	for i := range argslice {
		elem := (*[2]uint32)(unsafe.Pointer(uintptr(list[0]) + uintptr(i)*8))
		argslice[i] = p3String(elem[0], elem[1])
	}

	p3GetEnvironment(&list)
	envs = make([]string, list[1])
	for i := range envs {
		elem := (*[4]uint32)(unsafe.Pointer(uintptr(list[0]) + uintptr(i)*16))
		envs[i] = p3String(elem[0], elem[1]) + "=" + p3String(elem[2], elem[3])
	}
}

func walltime() (sec int64, nsec int32) {
	return walltime1()
}

// While p3Clocks.paused is set, the clocks report the last time they read,
// without calling imports.
var p3Clocks struct {
	paused bool
	mono   int64
	sec    int64
	nsec   int32
}

func walltime1() (sec int64, nsec int32) {
	if !p3Clocks.paused {
		var now p3Instant
		p3SystemNow(&now)
		p3Clocks.sec, p3Clocks.nsec = now.seconds, int32(now.nanoseconds)
	}
	return p3Clocks.sec, p3Clocks.nsec
}

func nanotime1() int64 {
	if !p3Clocks.paused {
		p3Clocks.mono = int64(p3MonotonicNow())
	}
	return p3Clocks.mono
}

// wasip3PauseClocks sets whether the clocks are paused. The host doesn't let
// a component call imports from cabi_realloc, so Component Model bindings
// whose cabi_realloc allocates Go memory pause the clocks, which the garbage
// collector reads, while it runs.
//
//go:linkname wasip3PauseClocks
func wasip3PauseClocks(paused bool) {
	p3Clocks.paused = paused
}

// syscall_wasip3ClockTime implements clock_time_get for the syscall package.
//
//go:linkname syscall_wasip3ClockTime syscall.wasip3ClockTime
func syscall_wasip3ClockTime(realtime bool) uint64 {
	if realtime {
		sec, nsec := walltime1()
		return uint64(sec)*1e9 + uint64(nsec)
	}
	return uint64(nanotime1())
}

// syscall_wasip3Random implements random_get for the syscall package.
//
//go:linkname syscall_wasip3Random syscall.wasip3Random
func syscall_wasip3Random(r []byte) {
	readRandom(r)
}

// syscall_wasip3Exit implements proc_exit for the syscall package.
//
//go:linkname syscall_wasip3Exit syscall.wasip3Exit
func syscall_wasip3Exit(code int32) {
	exit(code)
}

// syscall_wasip3Await parks the calling goroutine until an event on waitable h
// (a stream or future end), and returns the event code and payload.
//
//go:linkname syscall_wasip3Await syscall.wasip3Await
func syscall_wasip3Await(h uint32) (event, payload uint32) {
	return p3Await(h)
}

// syscall_wasip3AwaitCall parks the calling goroutine until the async-lowered
// import call that returned status has returned.
//
//go:linkname syscall_wasip3AwaitCall syscall.wasip3AwaitCall
func syscall_wasip3AwaitCall(status uint32) {
	p3AwaitCall(status)
}

// Memory that the host allocates with cabi_realloc to pass strings and lists
// to the syscall package is only needed until the syscall package has copied
// them out. The syscall package holds the allocator around every import call
// that may allocate, and when no call holds it any longer, the allocator
// rewinds to where it was when the first of them started.
//
// This relies on nothing else keeping memory from cabi_realloc, which holds for
// executables, where the runtime is the only user.
var p3Cabi struct {
	holds     int32
	next, end uintptr
}

//go:linkname syscall_wasip3CabiHold syscall.wasip3CabiHold
//go:nosplit
func syscall_wasip3CabiHold() {
	if p3Cabi.holds == 0 {
		p3Cabi.next, p3Cabi.end = cabiNext, cabiEnd
	}
	p3Cabi.holds++
}

//go:linkname net_wasip3CabiHold net.wasip3CabiHold
//go:nosplit
func net_wasip3CabiHold() {
	syscall_wasip3CabiHold()
}

//go:linkname net_wasip3CabiRelease net.wasip3CabiRelease
//go:nosplit
func net_wasip3CabiRelease() {
	syscall_wasip3CabiRelease()
}

//go:linkname net_wasip3Await net.wasip3Await
func net_wasip3Await(h uint32) (event, payload uint32) {
	return p3Await(h)
}

//go:linkname net_wasip3SubtaskDrop net.wasip3SubtaskDrop
func net_wasip3SubtaskDrop(subtask uint32) {
	p3SubtaskDrop(subtask)
}

//go:linkname syscall_wasip3CabiRelease syscall.wasip3CabiRelease
//go:nosplit
func syscall_wasip3CabiRelease() {
	p3Cabi.holds--
	if p3Cabi.holds == 0 {
		// If the allocator moved on to a new chunk meanwhile, the new chunk
		// is abandoned.
		cabiNext, cabiEnd = p3Cabi.next, p3Cabi.end
	}
}
