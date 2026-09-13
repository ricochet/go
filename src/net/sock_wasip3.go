// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build wasip3

package net

import (
	"internal/byteorder"
	"syscall"
	"unsafe"
)

// Imports of wasi:sockets@0.3.0, and helpers for its canonical ABI.

// Implemented in the runtime (see runtime/netpoll_wasip3.go).
func wasip3PollArm(pd uintptr, op unsafe.Pointer, mode int32)
func wasip3PollDisarm(op unsafe.Pointer) bool
func wasip3PollReady(pd uintptr, mode int32)
func wasip3Await(h uint32) (event, payload uint32)
func wasip3SubtaskDrop(subtask uint32)
func wasip3CabiHold()
func wasip3CabiRelease()

// Implemented in package syscall.
//
//go:linkname wasip3SocketFD syscall.wasip3SocketFD
func wasip3SocketFD() int

// Canonical ABI codes.
const (
	p3Blocked   = 0xffffffff
	p3Completed = 0
	p3Dropped   = 1

	p3SubtaskReturned = 2
)

//go:wasmimport $root [subtask-cancel]
func p3SubtaskCancel(subtask uint32) uint32

//go:wasmimport wasi:sockets/types@0.3.0 [static]tcp-socket.create
//go:noescape
func p3TCPCreate(family uint32, result *p3Result)

//go:wasmimport wasi:sockets/types@0.3.0 [method]tcp-socket.bind
//go:noescape
func p3TCPBind(self, disc, a1, a2, a3, a4, a5, a6, a7, a8, a9, a10, a11 uint32, result *p3Result)

//go:wasmimport wasi:sockets/types@0.3.0 [async-lower][method]tcp-socket.connect
func p3TCPConnect(params, result unsafe.Pointer) uint32

//go:wasmimport wasi:sockets/types@0.3.0 [method]tcp-socket.listen
//go:noescape
func p3TCPListen(self uint32, result *p3Result)

//go:wasmimport wasi:sockets/types@0.3.0 [method]tcp-socket.send
func p3TCPSend(self, data uint32) uint32

//go:wasmimport wasi:sockets/types@0.3.0 [method]tcp-socket.receive
//go:noescape
func p3TCPReceive(self uint32, result *[2]uint32)

//go:wasmimport wasi:sockets/types@0.3.0 [method]tcp-socket.get-local-address
//go:noescape
func p3TCPLocalAddress(self uint32, result *p3AddrBuf)

//go:wasmimport wasi:sockets/types@0.3.0 [method]tcp-socket.get-remote-address
//go:noescape
func p3TCPRemoteAddress(self uint32, result *p3AddrBuf)

//go:wasmimport wasi:sockets/types@0.3.0 [method]tcp-socket.set-listen-backlog-size
//go:noescape
func p3TCPSetListenBacklogSize(self uint32, value uint64, result *p3Result)

//go:wasmimport wasi:sockets/types@0.3.0 [method]tcp-socket.set-keep-alive-enabled
//go:noescape
func p3TCPSetKeepAliveEnabled(self uint32, value bool, result *p3Result)

//go:wasmimport wasi:sockets/types@0.3.0 [method]tcp-socket.set-keep-alive-idle-time
//go:noescape
func p3TCPSetKeepAliveIdleTime(self uint32, value uint64, result *p3Result)

//go:wasmimport wasi:sockets/types@0.3.0 [method]tcp-socket.set-keep-alive-interval
//go:noescape
func p3TCPSetKeepAliveInterval(self uint32, value uint64, result *p3Result)

//go:wasmimport wasi:sockets/types@0.3.0 [method]tcp-socket.set-keep-alive-count
//go:noescape
func p3TCPSetKeepAliveCount(self, value uint32, result *p3Result)

//go:wasmimport wasi:sockets/types@0.3.0 [method]tcp-socket.set-receive-buffer-size
//go:noescape
func p3TCPSetReceiveBufferSize(self uint32, value uint64, result *p3Result)

//go:wasmimport wasi:sockets/types@0.3.0 [method]tcp-socket.set-send-buffer-size
//go:noescape
func p3TCPSetSendBufferSize(self uint32, value uint64, result *p3Result)

//go:wasmimport wasi:sockets/types@0.3.0 [resource-drop]tcp-socket
func p3TCPDrop(self uint32)

//go:wasmimport wasi:sockets/types@0.3.0 [stream-new-0][method]tcp-socket.send
func p3BytesNew() uint64

//go:wasmimport wasi:sockets/types@0.3.0 [async-lower][stream-write-0][method]tcp-socket.send
func p3BytesWrite(writable uint32, buf unsafe.Pointer, n uint32) uint32

//go:wasmimport wasi:sockets/types@0.3.0 [stream-cancel-write-0][method]tcp-socket.send
func p3BytesCancelWrite(writable uint32) uint32

//go:wasmimport wasi:sockets/types@0.3.0 [stream-drop-writable-0][method]tcp-socket.send
func p3BytesDropWritable(writable uint32)

//go:wasmimport wasi:sockets/types@0.3.0 [async-lower][stream-read-0][method]tcp-socket.receive
func p3BytesRead(readable uint32, buf unsafe.Pointer, n uint32) uint32

//go:wasmimport wasi:sockets/types@0.3.0 [stream-cancel-read-0][method]tcp-socket.receive
func p3BytesCancelRead(readable uint32) uint32

//go:wasmimport wasi:sockets/types@0.3.0 [stream-drop-readable-0][method]tcp-socket.receive
func p3BytesDropReadable(readable uint32)

//go:wasmimport wasi:sockets/types@0.3.0 [async-lower][future-read-1][method]tcp-socket.send
func p3ResultRead(readable uint32, result unsafe.Pointer) uint32

//go:wasmimport wasi:sockets/types@0.3.0 [future-drop-readable-1][method]tcp-socket.send
func p3ResultDropReadable(readable uint32)

//go:wasmimport wasi:sockets/types@0.3.0 [async-lower][stream-read-0][method]tcp-socket.listen
func p3SocketsRead(readable uint32, buf unsafe.Pointer, n uint32) uint32

//go:wasmimport wasi:sockets/types@0.3.0 [stream-cancel-read-0][method]tcp-socket.listen
func p3SocketsCancelRead(readable uint32) uint32

//go:wasmimport wasi:sockets/types@0.3.0 [stream-drop-readable-0][method]tcp-socket.listen
func p3SocketsDropReadable(readable uint32)

//go:wasmimport wasi:sockets/types@0.3.0 [static]udp-socket.create
//go:noescape
func p3UDPCreate(family uint32, result *p3Result)

//go:wasmimport wasi:sockets/types@0.3.0 [method]udp-socket.bind
//go:noescape
func p3UDPBind(self, disc, a1, a2, a3, a4, a5, a6, a7, a8, a9, a10, a11 uint32, result *p3Result)

//go:wasmimport wasi:sockets/types@0.3.0 [method]udp-socket.connect
//go:noescape
func p3UDPConnect(self, disc, a1, a2, a3, a4, a5, a6, a7, a8, a9, a10, a11 uint32, result *p3Result)

//go:wasmimport wasi:sockets/types@0.3.0 [async-lower][method]udp-socket.send
func p3UDPSend(params, result unsafe.Pointer) uint32

//go:wasmimport wasi:sockets/types@0.3.0 [async-lower][method]udp-socket.receive
func p3UDPReceive(self uint32, result unsafe.Pointer) uint32

//go:wasmimport wasi:sockets/types@0.3.0 [method]udp-socket.get-local-address
//go:noescape
func p3UDPLocalAddress(self uint32, result *p3AddrBuf)

//go:wasmimport wasi:sockets/types@0.3.0 [method]udp-socket.get-remote-address
//go:noescape
func p3UDPRemoteAddress(self uint32, result *p3AddrBuf)

//go:wasmimport wasi:sockets/types@0.3.0 [method]udp-socket.set-receive-buffer-size
//go:noescape
func p3UDPSetReceiveBufferSize(self uint32, value uint64, result *p3Result)

//go:wasmimport wasi:sockets/types@0.3.0 [method]udp-socket.set-send-buffer-size
//go:noescape
func p3UDPSetSendBufferSize(self uint32, value uint64, result *p3Result)

//go:wasmimport wasi:sockets/types@0.3.0 [resource-drop]udp-socket
func p3UDPDrop(self uint32)

//go:wasmimport wasi:sockets/ip-name-lookup@0.3.0 [async-lower]resolve-addresses
func p3ResolveAddresses(name unsafe.Pointer, nameLen uint32, result unsafe.Pointer) uint32

// Sizes of canonical ABI values.
const (
	p3ResultSize     = 20 // result<T, error-code> where T fits in 16 bytes
	p3AddrSize       = 32 // ip-socket-address
	p3AddrResultSize = 4 + p3AddrSize
	p3IPAddrSize     = 18 // ip-address
)

// p3Errnos maps error-code cases to errors. The last case, other, is EIO.
var p3Errnos = [...]syscall.Errno{
	syscall.EACCES,        // access-denied
	syscall.EOPNOTSUPP,    // not-supported
	syscall.EINVAL,        // invalid-argument
	syscall.ENOMEM,        // out-of-memory
	syscall.ETIMEDOUT,     // timeout
	syscall.EINVAL,        // invalid-state
	syscall.EADDRNOTAVAIL, // address-not-bindable
	syscall.EADDRINUSE,    // address-in-use
	syscall.EHOSTUNREACH,  // remote-unreachable
	syscall.ECONNREFUSED,  // connection-refused
	syscall.EPIPE,         // connection-broken
	syscall.ECONNRESET,    // connection-reset
	syscall.ECONNABORTED,  // connection-aborted
	syscall.EMSGSIZE,      // datagram-too-large
}

// p3Error returns the error of the result<T, error-code> in b, whose payload
// is at offset 4.
func p3Error(b []byte) error {
	if b[0] == 0 {
		return nil
	}
	if c := int(b[4]); c < len(p3Errnos) {
		return p3Errnos[c]
	}
	return syscall.EIO
}

// p3AwaitResult reads a future<result<_, error-code>>.
func p3AwaitResult(future uint32) error {
	res := make([]byte, p3ResultSize)
	code := p3ResultRead(future, unsafe.Pointer(&res[0]))
	if code == p3Blocked {
		_, code = wasip3Await(future)
	}
	if code&0xf != p3Completed {
		return nil
	}
	return p3Error(res)
}

// A p3Addr is an ip-socket-address.
type p3Addr [p3AddrSize]byte

func p3AddrFromSockaddr(sa syscall.Sockaddr) (a p3Addr, err error) {
	switch sa := sa.(type) {
	case *syscall.SockaddrInet4:
		if sa.Port < 0 || sa.Port > 0xffff {
			return a, syscall.EINVAL
		}
		a[0] = 0
		byteorder.LEPutUint16(a[4:], uint16(sa.Port))
		copy(a[6:10], sa.Addr[:])
	case *syscall.SockaddrInet6:
		if sa.Port < 0 || sa.Port > 0xffff {
			return a, syscall.EINVAL
		}
		a[0] = 1
		byteorder.LEPutUint16(a[4:], uint16(sa.Port))
		for i := range 8 {
			byteorder.LEPutUint16(a[12+2*i:], byteorder.BEUint16(sa.Addr[2*i:]))
		}
		byteorder.LEPutUint32(a[28:], sa.ZoneId)
	default:
		return a, syscall.EAFNOSUPPORT
	}
	return a, nil
}

// flat returns a's flattened canonical ABI values, after the discriminant.
func (a *p3Addr) flat() (disc uint32, v [11]uint32) {
	v[0] = uint32(byteorder.LEUint16(a[4:]))
	if a[0] == 0 {
		for i := range 4 {
			v[1+i] = uint32(a[6+i])
		}
		return 0, v
	}
	v[1] = byteorder.LEUint32(a[8:])
	for i := range 8 {
		v[2+i] = uint32(byteorder.LEUint16(a[12+2*i:]))
	}
	v[10] = byteorder.LEUint32(a[28:])
	return 1, v
}

func (a *p3Addr) family() int {
	if a[0] == 0 {
		return syscall.AF_INET
	}
	return syscall.AF_INET6
}

func (a *p3Addr) ipPort() (IP, int) {
	port := int(byteorder.LEUint16(a[4:]))
	if a[0] == 0 {
		return IPv4(a[6], a[7], a[8], a[9]), port
	}
	ip := make(IP, IPv6len)
	for i := range 8 {
		byteorder.BEPutUint16(ip[2*i:], byteorder.LEUint16(a[12+2*i:]))
	}
	return ip, port
}

func (a *p3Addr) sockaddr() syscall.Sockaddr {
	ip, port := a.ipPort()
	if a[0] == 0 {
		sa := &syscall.SockaddrInet4{Port: port}
		copy(sa.Addr[:], ip.To4())
		return sa
	}
	sa := &syscall.SockaddrInet6{Port: port, ZoneId: byteorder.LEUint32(a[28:])}
	copy(sa.Addr[:], ip)
	return sa
}

// Memory for results, with the 4-byte alignment that the host checks.
type (
	p3Result  [p3ResultSize / 4]uint32     // result<T, error-code>
	p3AddrBuf [p3AddrResultSize / 4]uint32 // result<ip-socket-address, error-code>
)

func (r *p3Result) bytes() []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(r)), p3ResultSize)
}

// p3AddrResult returns the address in a result<ip-socket-address, error-code>.
func p3AddrResult(res *p3AddrBuf) (*p3Addr, error) {
	b := unsafe.Slice((*byte)(unsafe.Pointer(res)), p3AddrResultSize)
	if err := p3Error(b); err != nil {
		return nil, err
	}
	return (*p3Addr)(b[4:]), nil
}
