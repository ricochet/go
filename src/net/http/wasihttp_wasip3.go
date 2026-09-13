// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build wasip3

package http

import (
	"net/http/internal/ascii"
	"runtime"
	"strconv"
	"sync"
	"unsafe"
)

// Imports of wasi:http@0.3.0, and helpers for its canonical ABI, for sending
// requests with the host's HTTP client (see roundtrip_wasip3.go).

// Implemented in the runtime (see runtime/os_wasip3.go). wasip3Await parks
// the calling goroutine until an event on waitable h, unless another
// goroutine sets *interrupted and calls wasip3Interrupt(h), and then returns
// p3EventNone.
//
//go:linkname wasip3Await
//go:noescape
func wasip3Await(h uint32, interrupted *bool) (event, payload uint32)

//go:linkname wasip3Interrupt
func wasip3Interrupt(h uint32)

//go:linkname wasip3SubtaskDrop
func wasip3SubtaskDrop(subtask uint32)

//go:linkname wasip3CabiHold
func wasip3CabiHold()

//go:linkname wasip3CabiRelease
func wasip3CabiRelease()

// Canonical ABI codes.
const (
	p3Blocked   = 0xffffffff
	p3Completed = 0
	p3Dropped   = 1
	p3Cancelled = 2

	p3EventNone       = 0
	p3SubtaskReturned = 2
)

//go:wasmimport $root [subtask-cancel]
func p3SubtaskCancel(subtask uint32) uint32

//go:wasmimport wasi:http/client@0.3.0 [async-lower]send
func p3Send(request uint32, result unsafe.Pointer) uint32

//go:wasmimport wasi:http/types@0.3.0 [static]fields.from-list
//go:noescape
func p3FieldsFromList(entries unsafe.Pointer, n uint32, result *[5]uint32)

//go:wasmimport wasi:http/types@0.3.0 [method]fields.copy-all
//go:noescape
func p3FieldsCopyAll(self uint32, result *[2]uint32)

//go:wasmimport wasi:http/types@0.3.0 [resource-drop]fields
func p3FieldsDrop(self uint32)

//go:wasmimport wasi:http/types@0.3.0 [static]request.new
//go:noescape
func p3RequestNew(headers, contentsSome, contents, trailers, optionsSome, options uint32, result *[2]uint32)

//go:wasmimport wasi:http/types@0.3.0 [method]request.set-method
//go:noescape
func p3RequestSetMethod(self, method uint32, other unsafe.Pointer, otherLen uint32) uint32

//go:wasmimport wasi:http/types@0.3.0 [method]request.set-scheme
//go:noescape
func p3RequestSetScheme(self, some, scheme uint32, other unsafe.Pointer, otherLen uint32) uint32

//go:wasmimport wasi:http/types@0.3.0 [method]request.set-authority
//go:noescape
func p3RequestSetAuthority(self, some uint32, authority unsafe.Pointer, n uint32) uint32

//go:wasmimport wasi:http/types@0.3.0 [method]request.set-path-with-query
//go:noescape
func p3RequestSetPathWithQuery(self, some uint32, path unsafe.Pointer, n uint32) uint32

//go:wasmimport wasi:http/types@0.3.0 [resource-drop]request
func p3RequestDrop(self uint32)

//go:wasmimport wasi:http/types@0.3.0 [method]response.get-status-code
func p3ResponseStatusCode(self uint32) uint32

//go:wasmimport wasi:http/types@0.3.0 [method]response.get-headers
func p3ResponseHeaders(self uint32) uint32

//go:wasmimport wasi:http/types@0.3.0 [static]response.consume-body
//go:noescape
func p3ResponseConsumeBody(self, res uint32, result *[2]uint32)

//go:wasmimport wasi:http/types@0.3.0 [resource-drop]response
func p3ResponseDrop(self uint32)

// Stream and future intrinsics, named after request.new, whose contents are
// a stream<u8> (0), trailers a future<result<option<trailers>, error-code>>
// (1), and result a future<result<_, error-code>> (2). The types are
// structural, so they work for the streams and futures of responses too.

//go:wasmimport wasi:http/types@0.3.0 [stream-new-0][static]request.new
func p3BytesNew() uint64

//go:wasmimport wasi:http/types@0.3.0 [async-lower][stream-write-0][static]request.new
func p3BytesWrite(writable uint32, buf unsafe.Pointer, n uint32) uint32

//go:wasmimport wasi:http/types@0.3.0 [stream-cancel-write-0][static]request.new
func p3BytesCancelWrite(writable uint32) uint32

//go:wasmimport wasi:http/types@0.3.0 [stream-drop-writable-0][static]request.new
func p3BytesDropWritable(writable uint32)

//go:wasmimport wasi:http/types@0.3.0 [async-lower][stream-read-0][static]request.new
func p3BytesRead(readable uint32, buf unsafe.Pointer, n uint32) uint32

//go:wasmimport wasi:http/types@0.3.0 [stream-cancel-read-0][static]request.new
func p3BytesCancelRead(readable uint32) uint32

//go:wasmimport wasi:http/types@0.3.0 [stream-drop-readable-0][static]request.new
func p3BytesDropReadable(readable uint32)

//go:wasmimport wasi:http/types@0.3.0 [future-new-1][static]request.new
func p3TrailersNew() uint64

//go:wasmimport wasi:http/types@0.3.0 [async-lower][future-write-1][static]request.new
func p3TrailersWrite(writable uint32, value unsafe.Pointer) uint32

//go:wasmimport wasi:http/types@0.3.0 [future-drop-writable-1][static]request.new
func p3TrailersDropWritable(writable uint32)

//go:wasmimport wasi:http/types@0.3.0 [async-lower][future-read-1][static]request.new
func p3TrailersRead(readable uint32, value unsafe.Pointer) uint32

//go:wasmimport wasi:http/types@0.3.0 [future-cancel-read-1][static]request.new
func p3TrailersCancelRead(readable uint32) uint32

//go:wasmimport wasi:http/types@0.3.0 [future-drop-readable-1][static]request.new
func p3TrailersDropReadable(readable uint32)

//go:wasmimport wasi:http/types@0.3.0 [future-new-2][static]request.new
func p3ResultNew() uint64

//go:wasmimport wasi:http/types@0.3.0 [async-lower][future-write-2][static]request.new
func p3ResultWrite(writable uint32, value unsafe.Pointer) uint32

//go:wasmimport wasi:http/types@0.3.0 [future-drop-writable-2][static]request.new
func p3ResultDropWritable(writable uint32)

//go:wasmimport wasi:http/types@0.3.0 [future-drop-readable-2][static]request.new
func p3ResultDropReadable(readable uint32)

// A p3Value is memory for a result<T, error-code>, where T fits in 8 bytes:
// the case in the first byte, and the payload at offset 8. The error-code has
// its case in its first byte too, and its payload at offset 8.
type p3Value [5]uint64

func (v *p3Value) u8(off uintptr) *uint8 {
	return (*uint8)(unsafe.Add(unsafe.Pointer(v), off))
}

func (v *p3Value) u32(off uintptr) *uint32 {
	return (*uint32)(unsafe.Add(unsafe.Pointer(v), off))
}

// Cases of error-code.
const (
	p3DNSTimeout             = 0
	p3DNSError               = 1
	p3ConnectionTimeout      = 8
	p3ConnectionReadTimeout  = 9
	p3ConnectionWriteTimeout = 10
	p3TLSAlertReceived       = 14
	p3ResponseTransferCoding = 31
	p3ResponseContentCoding  = 32
	p3HTTPResponseTimeout    = 33
	p3InternalError          = 38
)

var p3ErrorCodes = [...]string{
	"DNS timeout", "DNS error", "destination not found",
	"destination unavailable", "destination IP prohibited",
	"destination IP unroutable", "connection refused",
	"connection terminated", "connection timeout", "connection read timeout",
	"connection write timeout", "connection limit reached",
	"TLS protocol error", "TLS certificate error", "TLS alert received",
	"HTTP request denied", "HTTP request length required",
	"HTTP request body size", "HTTP request method invalid",
	"HTTP request URI invalid", "HTTP request URI too long",
	"HTTP request header section size", "HTTP request header size",
	"HTTP request trailer section size", "HTTP request trailer size",
	"HTTP response incomplete", "HTTP response header section size",
	"HTTP response header size", "HTTP response body size",
	"HTTP response trailer section size", "HTTP response trailer size",
	"HTTP response transfer coding", "HTTP response content coding",
	"HTTP response timeout", "HTTP upgrade failed", "HTTP protocol error",
	"loop detected", "configuration error", "internal error",
}

// A p3Error is an error-code from the host's HTTP client.
type p3Error struct {
	code   uint8
	detail string
}

func (e *p3Error) Error() string {
	s := "wasi:http: "
	if int(e.code) < len(p3ErrorCodes) {
		s += p3ErrorCodes[e.code]
	} else {
		s += "error " + strconv.Itoa(int(e.code))
	}
	if e.detail != "" {
		s += ": " + e.detail
	}
	return s
}

func (e *p3Error) Timeout() bool {
	switch e.code {
	case p3DNSTimeout, p3ConnectionTimeout, p3ConnectionReadTimeout,
		p3ConnectionWriteTimeout, p3HTTPResponseTimeout:
		return true
	}
	return false
}

// p3ErrorCode returns the error-code at offset off of v. Its strings are in
// memory from cabi_realloc, which the caller holds.
func p3ErrorCode(v *p3Value, off uintptr) error {
	e := &p3Error{code: *v.u8(off)}
	p := off + 8
	switch e.code {
	case p3DNSError: // rcode: option<string>, info-code: option<u16>
		if *v.u8(p) != 0 {
			e.detail = "rcode " + p3String(*v.u32(p + 4), *v.u32(p + 8))
		}
	case p3TLSAlertReceived: // alert-id: option<u8>, alert-message: option<string>
		if *v.u8(p + 4) != 0 {
			e.detail = p3String(*v.u32(p + 8), *v.u32(p + 12))
		}
	case p3ResponseTransferCoding, p3ResponseContentCoding, p3InternalError: // option<string>
		if *v.u8(p) != 0 {
			e.detail = p3String(*v.u32(p + 4), *v.u32(p + 8))
		}
	}
	return e
}

// p3String returns a copy of a string that the host allocated.
func p3String(ptr, n uint32) string {
	return string(unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), n))
}

func p3StringPtr(s string) unsafe.Pointer {
	return unsafe.Pointer(unsafe.StringData(s))
}

// p3Forbidden reports whether hosts don't let requests set header key,
// since they manage connections themselves (these are the headers that
// Wasmtime forbids).
func p3Forbidden(key string) bool {
	for _, k := range [...]string{
		"Connection", "Keep-Alive", "Proxy-Authenticate",
		"Proxy-Authorization", "Proxy-Connection", "Transfer-Encoding",
		"Upgrade", "Host", "Http2-Settings",
	} {
		if ascii.EqualFold(key, k) {
			return true
		}
	}
	return false
}

var p3HeaderErrors = [...]string{"invalid syntax", "forbidden", "immutable", "size exceeded"}

// p3NewFields returns a new fields resource with the given names and values,
// which alternate.
func p3NewFields(fields []string) (uint32, error) {
	entries := make([]uint32, 2*len(fields))
	for i, s := range fields {
		entries[2*i] = uint32(uintptr(p3StringPtr(s)))
		entries[2*i+1] = uint32(len(s))
	}
	var res [5]uint32
	wasip3CabiHold()
	p3FieldsFromList(unsafe.Pointer(unsafe.SliceData(entries)), uint32(len(fields)/2), &res)
	wasip3CabiRelease()
	runtime.KeepAlive(fields)
	if uint8(res[0]) != 0 {
		msg := "other error"
		if c := uint8(res[1]); int(c) < len(p3HeaderErrors) {
			msg = p3HeaderErrors[c]
		}
		return 0, &p3Error{code: p3InternalError, detail: "invalid header: " + msg}
	}
	return res[1], nil
}

// p3Header returns the entries of a fields resource, and drops it.
func p3Header(fields uint32) Header {
	var list [2]uint32
	wasip3CabiHold()
	defer wasip3CabiRelease()
	p3FieldsCopyAll(fields, &list)
	p3FieldsDrop(fields)
	h := make(Header, list[1])
	for i := range list[1] {
		e := (*[4]uint32)(unsafe.Pointer(uintptr(list[0] + 16*i)))
		key := CanonicalHeaderKey(p3String(e[0], e[1]))
		h[key] = append(h[key], p3String(e[2], e[3]))
	}
	return h
}

// A p3Cancel lets goroutines interrupt waits for the waitables of a round
// trip, when its context is done or its response body is closed.
type p3Cancel struct {
	mu          sync.Mutex
	interrupted bool  // read by the runtime
	err         error // the reason for the interruption
	waiting     []uint32
}

// wait waits for an event on waitable h, and returns its payload, or false
// if the wait was interrupted.
func (c *p3Cancel) wait(h uint32) (payload uint32, ok bool) {
	c.mu.Lock()
	c.waiting = append(c.waiting, h)
	c.mu.Unlock()
	event, payload := wasip3Await(h, &c.interrupted)
	c.mu.Lock()
	for i, w := range c.waiting {
		if w == h {
			c.waiting = append(c.waiting[:i], c.waiting[i+1:]...)
			break
		}
	}
	c.mu.Unlock()
	return payload, event != p3EventNone
}

// interrupt interrupts the current and future waits, for reason err.
func (c *p3Cancel) interrupt(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err == nil {
		c.err = err
	}
	c.interrupted = true
	for _, h := range c.waiting {
		wasip3Interrupt(h)
	}
}

func (c *p3Cancel) error() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// p3Await waits for an event on waitable h, and returns its payload.
func p3Await(h uint32) uint32 {
	var never bool
	_, payload := wasip3Await(h, &never)
	return payload
}

// Stream buffers.
const p3BufSize = 64 << 10

var p3Bufs = sync.Pool{New: func() any { return new([p3BufSize]byte) }}
