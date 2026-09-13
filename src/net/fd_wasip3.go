// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build wasip3

package net

import (
	"context"
	"errors"
	"internal/byteorder"
	"internal/poll"
	"io"
	"os"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// TCP and UDP sockets for GOOS=wasip3, on wasi:sockets@0.3.0.
//
// Component Model operations complete asynchronously rather than report
// readiness, so a netFD keeps at most one read and one write operation in
// progress (a p3Op). When an operation would block, the runtime records its
// completion in the p3Op and makes the netFD's poll descriptor ready, so the
// goroutine that started it waits in the poll descriptor (with RawRead or
// RawWrite), which provides deadlines and closing. When waiting stops
// because of a deadline or Close, the operation is cancelled, keeping
// whatever it transferred.

const (
	readSyscallName  = "read"
	writeSyscallName = "write"

	p3BufSize = 64 << 10
)

// errP3Wait reports that an operation must wait for the poll descriptor.
var errP3Wait = errors.New("wait")

// A p3Op is an operation in progress on a waitable. The runtime depends on
// the layout of the first four fields.
type p3Op struct {
	event, payload, done uint32 // set when the operation completes
	handle               uint32 // the waitable, or 0 if there is no operation
	kind                 uint8
}

// Kinds of operations, which differ in how to cancel them.
const (
	p3OpBytesRead = iota
	p3OpBytesWrite
	p3OpAccept
	p3OpSubtask
)

// A p3Socket holds the Component Model resources of a socket.
type p3Socket struct {
	mu     sync.Mutex
	closed bool
	tcp    uint32 // the tcp-socket, or 0
	udp    uint32 // the udp-socket, or 0

	read, write p3Op

	// A listening TCP socket's stream of connections.
	accept uint32
	abuf   []byte

	// A connected TCP socket's streams.
	recv, recvResult uint32
	rbuf             []byte
	roff, rlen       int
	rerr             error // why receiving ended
	send, sendResult uint32
	wbuf             []byte
	werr             error // why sending ended
	wshut            bool  // shut down sending after the write in progress

	// A UDP socket's receive result, and the datagram it received.
	rres     []byte
	dgram    []byte
	dgramErr error
	dgramSrc p3Addr
}

// Network file descriptor.
type netFD struct {
	pfd poll.FD

	// immutable until Close
	family      int
	sotype      int
	isConnected bool // handshake completed or use of association with peer
	net         string
	laddr       Addr
	raddr       Addr

	s *p3Socket
}

func sysSocket(family, sotype, proto int) (int, error) {
	return 0, syscall.ENOSYS
}

// socket returns a network file descriptor that is ready for I/O.
func socket(ctx context.Context, net string, family, sotype, proto int, ipv6only bool, laddr, raddr sockaddr, ctrlCtxFn func(context.Context, string, string, syscall.RawConn) error) (fd *netFD, err error) {
	if family != syscall.AF_INET && family != syscall.AF_INET6 {
		return nil, os.NewSyscallError("socket", syscall.EAFNOSUPPORT)
	}
	s := new(p3Socket)
	var res p3Result
	wasiFamily := uint32(0)
	if family == syscall.AF_INET6 {
		wasiFamily = 1
	}
	switch sotype {
	case syscall.SOCK_STREAM:
		p3TCPCreate(wasiFamily, &res)
		s.tcp = res[1]
	case syscall.SOCK_DGRAM:
		p3UDPCreate(wasiFamily, &res)
		s.udp = res[1]
	default:
		return nil, os.NewSyscallError("socket", syscall.EPROTONOSUPPORT)
	}
	if err := p3Error(res.bytes()); err != nil {
		return nil, os.NewSyscallError("socket", err)
	}
	fd, err = newP3FD(s, net, family, sotype)
	if err != nil {
		return nil, err
	}

	if ctrlCtxFn != nil {
		var addr string
		if raddr != nil {
			addr = raddr.String()
		} else if laddr != nil {
			addr = laddr.String()
		}
		if err := ctrlCtxFn(ctx, fd.ctrlNetwork(), addr, newRawConn(fd)); err != nil {
			fd.Close()
			return nil, err
		}
	}

	if laddr != nil {
		if err := fd.bind(laddr); err != nil {
			fd.Close()
			return nil, err
		}
	}
	if raddr != nil {
		if err := fd.connect(ctx, raddr); err != nil {
			fd.Close()
			return nil, err
		}
		fd.isConnected = true
	} else if sotype == syscall.SOCK_STREAM {
		if err := fd.listen(); err != nil {
			fd.Close()
			return nil, err
		}
	}
	fd.setAddr(fd.localAddr(), fd.remoteAddr())
	return fd, nil
}

func newP3FD(s *p3Socket, net string, family, sotype int) (*netFD, error) {
	fd := &netFD{
		pfd: poll.FD{
			Sysfd:         wasip3SocketFD(),
			IsStream:      sotype == syscall.SOCK_STREAM,
			ZeroReadIsEOF: sotype != syscall.SOCK_DGRAM,
		},
		family: family,
		sotype: sotype,
		net:    net,
		s:      s,
	}
	if err := fd.pfd.Init(net, true); err != nil {
		fd.pfd.Close()
		s.close()
		return nil, err
	}
	return fd, nil
}

func (fd *netFD) ctrlNetwork() string {
	switch fd.net[len(fd.net)-1] {
	case '4', '6':
		return fd.net
	}
	if fd.family == syscall.AF_INET {
		return fd.net + "4"
	}
	return fd.net + "6"
}

func (fd *netFD) name() string {
	var ls, rs string
	if fd.laddr != nil {
		ls = fd.laddr.String()
	}
	if fd.raddr != nil {
		rs = fd.raddr.String()
	}
	return fd.net + ":" + ls + "->" + rs
}

func (fd *netFD) bind(laddr sockaddr) error {
	sa, err := laddr.sockaddr(fd.family)
	if err != nil {
		return err
	}
	if sa == nil {
		return nil
	}
	a, err := p3AddrFromSockaddr(sa)
	if err != nil {
		return os.NewSyscallError("bind", err)
	}
	disc, v := a.flat()
	var res p3Result
	if fd.s.tcp != 0 {
		p3TCPBind(fd.s.tcp, disc, v[0], v[1], v[2], v[3], v[4], v[5], v[6], v[7], v[8], v[9], v[10], &res)
	} else {
		p3UDPBind(fd.s.udp, disc, v[0], v[1], v[2], v[3], v[4], v[5], v[6], v[7], v[8], v[9], v[10], &res)
	}
	return os.NewSyscallError("bind", p3Error(res.bytes()))
}

func (fd *netFD) listen() error {
	var res p3Result
	p3TCPSetListenBacklogSize(fd.s.tcp, uint64(listenerBacklog()), &res)
	p3TCPListen(fd.s.tcp, &res)
	if err := p3Error(res.bytes()); err != nil {
		return os.NewSyscallError("listen", err)
	}
	fd.s.accept = res[1]
	return nil
}

func (fd *netFD) connect(ctx context.Context, raddr sockaddr) error {
	sa, err := raddr.sockaddr(fd.family)
	if err != nil {
		return err
	}
	a, err := p3AddrFromSockaddr(sa)
	if err != nil {
		return os.NewSyscallError("connect", err)
	}
	s := fd.s
	if s.udp != 0 {
		disc, v := a.flat()
		var res p3Result
		p3UDPConnect(s.udp, disc, v[0], v[1], v[2], v[3], v[4], v[5], v[6], v[7], v[8], v[9], v[10], &res)
		return os.NewSyscallError("connect", p3Error(res.bytes()))
	}

	params := make([]byte, 4+p3AddrSize)
	byteorder.LEPutUint32(params, s.tcp)
	copy(params[4:], a[:])
	res := make([]byte, p3ResultSize)
	status := p3TCPConnect(unsafe.Pointer(&params[0]), unsafe.Pointer(&res[0]))
	if status&0xf != p3SubtaskReturned {
		if deadline, hasDeadline := ctx.Deadline(); hasDeadline {
			fd.pfd.SetWriteDeadline(deadline)
			defer fd.pfd.SetWriteDeadline(noDeadline)
		}
		stop := context.AfterFunc(ctx, func() {
			fd.pfd.SetWriteDeadline(aLongTimeAgo)
		})
		defer stop()

		s.mu.Lock()
		s.write = p3Op{handle: status >> 4, kind: p3OpSubtask}
		wasip3PollArm(fd.pfd.RuntimeCtx(), unsafe.Pointer(&s.write), 'w')
		s.mu.Unlock()
		werr := fd.pfd.RawWrite(func(uintptr) bool {
			s.mu.Lock()
			defer s.mu.Unlock()
			return s.write.done != 0
		})
		if werr != nil {
			s.stop(&s.write)
		}
		s.mu.Lock()
		subtask, state := s.write.handle, s.write.payload
		s.write = p3Op{}
		s.mu.Unlock()
		wasip3SubtaskDrop(subtask)
		if state != p3SubtaskReturned {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return mapErr(ctxErr)
			}
			return werr
		}
	}
	runtime.KeepAlive(params)
	return os.NewSyscallError("connect", p3Error(res))
}

func (fd *netFD) localAddr() Addr {
	var res p3AddrBuf
	if fd.s.tcp != 0 {
		p3TCPLocalAddress(fd.s.tcp, &res)
	} else {
		p3UDPLocalAddress(fd.s.udp, &res)
	}
	a, err := p3AddrResult(&res)
	if err != nil {
		return nil
	}
	return fd.addr(a)
}

func (fd *netFD) remoteAddr() Addr {
	var res p3AddrBuf
	if fd.s.tcp != 0 {
		p3TCPRemoteAddress(fd.s.tcp, &res)
	} else {
		p3UDPRemoteAddress(fd.s.udp, &res)
	}
	a, err := p3AddrResult(&res)
	if err != nil {
		return nil
	}
	return fd.addr(a)
}

func (fd *netFD) addr(a *p3Addr) Addr {
	ip, port := a.ipPort()
	if fd.sotype == syscall.SOCK_STREAM {
		return &TCPAddr{IP: ip, Port: port}
	}
	return &UDPAddr{IP: ip, Port: port}
}

func (fd *netFD) setAddr(laddr, raddr Addr) {
	fd.laddr = laddr
	fd.raddr = raddr
	runtime.SetFinalizer(fd, (*netFD).Close)
}

func (fd *netFD) Close() error {
	runtime.SetFinalizer(fd, nil)
	// Closing the poll descriptor wakes up the goroutines waiting in it,
	// and waits for them to leave.
	err := fd.pfd.Close()
	fd.s.close()
	return err
}

// start records op, which is waiting for waitable h, and arranges for its
// completion to make fd ready for mode. s.mu must be held.
func (fd *netFD) start(op *p3Op, h uint32, kind uint8, mode int32) {
	*op = p3Op{handle: h, kind: kind}
	wasip3PollArm(fd.pfd.RuntimeCtx(), unsafe.Pointer(op), mode)
}

// stop cancels op if it is still in progress. The operation's final result
// stays in op for its owner to consume.
func (s *p3Socket) stop(op *p3Op) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopLocked(op)
}

func (s *p3Socket) stopLocked(op *p3Op) {
	if s.closed || op.handle == 0 || op.done != 0 {
		return
	}
	if wasip3PollDisarm(unsafe.Pointer(op)) {
		return
	}
	switch op.kind {
	case p3OpBytesRead:
		op.payload = p3BytesCancelRead(op.handle)
	case p3OpBytesWrite:
		op.payload = p3BytesCancelWrite(op.handle)
	case p3OpAccept:
		op.payload = p3SocketsCancelRead(op.handle)
	case p3OpSubtask:
		op.payload = p3SubtaskCancel(op.handle)
	}
	op.done = 1
	if op == &s.read && s.udp != 0 {
		// Don't keep the host's allocations until the next read.
		s.udpReceived()
	}
}

func (s *p3Socket) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopLocked(&s.read)
	s.stopLocked(&s.write)
	s.closed = true
	if s.udp != 0 && s.read.handle != 0 {
		s.udpReceived() // completed, but not consumed
	}
	if s.accept != 0 {
		if s.read.handle != 0 && s.read.payload>>4 == 1 {
			p3TCPDrop(byteorder.LEUint32(s.abuf)) // accepted, but not returned
		}
		p3SocketsDropReadable(s.accept)
	}
	if s.write.handle != 0 && s.write.kind == p3OpSubtask {
		wasip3SubtaskDrop(s.write.handle)
	}
	if s.recv != 0 {
		p3BytesDropReadable(s.recv)
		p3ResultDropReadable(s.recvResult)
	}
	if s.send != 0 {
		p3BytesDropWritable(s.send)
		p3ResultDropReadable(s.sendResult)
	}
	if s.tcp != 0 {
		p3TCPDrop(s.tcp)
	}
	if s.udp != 0 {
		p3UDPDrop(s.udp)
	}
	s.read, s.write = p3Op{}, p3Op{}
	s.accept, s.recv, s.send, s.tcp, s.udp = 0, 0, 0, 0, 0
}

func (fd *netFD) accept() (*netFD, error) {
	var h uint32
	var aerr error
	err := fd.pfd.RawRead(func(uintptr) bool {
		h, aerr = fd.acceptStep()
		return aerr != errP3Wait
	})
	if err != nil {
		fd.s.stop(&fd.s.read)
		return nil, err
	}
	if aerr != nil {
		return nil, aerr
	}
	s := &p3Socket{tcp: h}
	var res p3AddrBuf
	p3TCPLocalAddress(h, &res)
	family := syscall.AF_INET
	if a, err := p3AddrResult(&res); err == nil {
		family = a.family()
	}
	netfd, err := newP3FD(s, fd.net, family, syscall.SOCK_STREAM)
	if err != nil {
		return nil, err
	}
	netfd.isConnected = true
	netfd.setAddr(netfd.localAddr(), netfd.remoteAddr())
	return netfd, nil
}

// acceptStep returns an accepted socket, or errP3Wait.
func (fd *netFD) acceptStep() (uint32, error) {
	s := fd.s
	s.mu.Lock()
	defer s.mu.Unlock()
	for {
		var code uint32
		switch {
		case s.closed:
			return 0, poll.ErrNetClosing
		case s.read.handle == 0:
			if s.abuf == nil {
				s.abuf = make([]byte, 4)
			}
			code = p3SocketsRead(s.accept, unsafe.Pointer(&s.abuf[0]), 1)
			if code == p3Blocked {
				fd.start(&s.read, s.accept, p3OpAccept, 'r')
				return 0, errP3Wait
			}
		case s.read.done == 0:
			return 0, errP3Wait
		default:
			code = s.read.payload
			s.read = p3Op{}
		}
		if code>>4 == 1 {
			return byteorder.LEUint32(s.abuf), nil
		}
		if code&0xf == p3Dropped {
			return 0, os.NewSyscallError("accept", syscall.EINVAL)
		}
		// Cancelled before a connection arrived: start over.
	}
}

func (fd *netFD) Read(p []byte) (int, error) {
	if fd.sotype == syscall.SOCK_DGRAM {
		n, _, err := fd.readFrom(p)
		return n, err
	}
	if len(p) == 0 {
		return 0, nil
	}
	var n int
	var rerr error
	err := fd.pfd.RawRead(func(uintptr) bool {
		n, rerr = fd.receiveStep(p, true)
		return rerr != errP3Wait
	})
	runtime.KeepAlive(fd)
	if err != nil {
		fd.s.stop(&fd.s.read)
		// Return what the read got before it was cancelled.
		if n, rerr = fd.receiveStep(p, false); n > 0 && err != poll.ErrNetClosing {
			return n, nil
		}
		return 0, err
	}
	if rerr == io.EOF {
		return n, io.EOF
	}
	return n, wrapSyscallError(readSyscallName, rerr)
}

// receiveStep reads from a TCP socket into p, or returns errP3Wait. Unless
// start is set, it only returns data that has already been received.
func (fd *netFD) receiveStep(p []byte, start bool) (int, error) {
	s := fd.s
	s.mu.Lock()
	defer s.mu.Unlock()
	for {
		if s.roff < s.rlen {
			n := copy(p, s.rbuf[s.roff:s.rlen])
			s.roff += n
			return n, nil
		}
		var code uint32
		switch {
		case s.closed:
			return 0, poll.ErrNetClosing
		case s.read.handle != 0 && s.read.done == 0:
			return 0, errP3Wait
		case s.read.handle != 0:
			code = s.read.payload
			s.read = p3Op{}
		case s.rerr != nil:
			return 0, s.rerr
		case !start:
			return 0, errP3Wait
		default:
			if s.recv == 0 {
				var pair [2]uint32
				p3TCPReceive(s.tcp, &pair)
				s.recv, s.recvResult = pair[0], pair[1]
				s.rbuf = make([]byte, p3BufSize)
			}
			code = p3BytesRead(s.recv, unsafe.Pointer(&s.rbuf[0]), uint32(len(s.rbuf)))
			if code == p3Blocked {
				fd.start(&s.read, s.recv, p3OpBytesRead, 'r')
				return 0, errP3Wait
			}
		}
		s.roff, s.rlen = 0, int(code>>4)
		if code&0xf == p3Dropped {
			s.rerr = p3AwaitResult(s.recvResult)
			if s.rerr == nil {
				s.rerr = io.EOF
			}
		}
	}
}

func (fd *netFD) Write(p []byte) (int, error) {
	if fd.sotype == syscall.SOCK_DGRAM {
		return fd.writeTo(p, nil)
	}
	nn := 0
	var werr error
	err := fd.pfd.RawWrite(func(uintptr) bool {
		for nn < len(p) {
			n, err := fd.sendStep(p[nn:])
			nn += n
			if err == errP3Wait {
				return false
			}
			if err != nil {
				werr = err
				return true
			}
		}
		return true
	})
	runtime.KeepAlive(fd)
	if err != nil {
		fd.s.stop(&fd.s.write)
		// Count what the write sent before it was cancelled.
		n, _ := fd.sendStep(nil)
		return nn + n, err
	}
	return nn, wrapSyscallError(writeSyscallName, werr)
}

// sendStep writes a prefix of p to a TCP socket, or returns errP3Wait.
func (fd *netFD) sendStep(p []byte) (int, error) {
	s := fd.s
	s.mu.Lock()
	defer s.mu.Unlock()
	var code uint32
	switch {
	case s.closed:
		return 0, poll.ErrNetClosing
	case s.write.handle != 0 && s.write.done == 0:
		return 0, errP3Wait
	case s.write.handle != 0:
		code = s.write.payload
		s.write = p3Op{}
	case len(p) == 0:
		return 0, nil
	case s.werr != nil:
		return 0, s.werr
	default:
		if s.send == 0 {
			s.openSend()
		}
		n := copy(s.wbuf, p)
		code = p3BytesWrite(s.send, unsafe.Pointer(&s.wbuf[0]), uint32(n))
		if code == p3Blocked {
			fd.start(&s.write, s.send, p3OpBytesWrite, 'w')
			return 0, errP3Wait
		}
	}
	if code&0xf == p3Dropped {
		s.werr = p3AwaitResult(s.sendResult)
		if s.werr == nil {
			s.werr = syscall.EPIPE
		}
	} else if s.wshut {
		s.shutdownSend()
	}
	return int(code >> 4), nil
}

// openSend starts sending on a TCP socket. s.mu must be held.
func (s *p3Socket) openSend() {
	pair := p3BytesNew()
	s.sendResult = p3TCPSend(s.tcp, uint32(pair))
	s.send = uint32(pair >> 32)
	s.wbuf = make([]byte, p3BufSize)
}

func (fd *netFD) closeRead() error {
	s := fd.s
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.tcp == 0 {
		return nil
	}
	s.stopLocked(&s.read)
	if s.recv != 0 {
		// The reader, if any, consumes the stopped read's result.
		if s.read.handle == 0 {
			p3BytesDropReadable(s.recv)
			p3ResultDropReadable(s.recvResult)
			s.recv = 0
		}
	}
	if s.rerr == nil {
		s.rerr = io.EOF
	}
	wasip3PollReady(fd.pfd.RuntimeCtx(), 'r')
	return nil
}

func (fd *netFD) closeWrite() error {
	s := fd.s
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.tcp == 0 || s.werr != nil {
		return nil
	}
	if s.write.handle != 0 {
		// Shut down once the write in progress finishes.
		s.wshut = true
		return nil
	}
	s.shutdownSend()
	return nil
}

// shutdownSend shuts down the sending side of a TCP connection, by ending its
// stream. s.mu must be held.
func (s *p3Socket) shutdownSend() {
	if s.send == 0 {
		s.openSend()
	}
	p3BytesDropWritable(s.send)
	p3ResultDropReadable(s.sendResult)
	s.send = 0
	s.werr = syscall.EPIPE
}

func (fd *netFD) readFrom(p []byte) (int, syscall.Sockaddr, error) {
	var n int
	var from syscall.Sockaddr
	var rerr error
	err := fd.pfd.RawRead(func(uintptr) bool {
		n, from, rerr = fd.receiveFromStep(p, true)
		return rerr != errP3Wait
	})
	runtime.KeepAlive(fd)
	if err != nil {
		fd.s.stop(&fd.s.read)
		if n, from, rerr = fd.receiveFromStep(p, false); rerr == nil && err != poll.ErrNetClosing {
			return n, from, nil
		}
		return 0, nil, err
	}
	return n, from, wrapSyscallError("recvfrom", rerr)
}

// receiveFromStep receives a datagram into p, or returns errP3Wait. Unless
// start is set, it only returns a datagram that has already been received.
func (fd *netFD) receiveFromStep(p []byte, start bool) (int, syscall.Sockaddr, error) {
	s := fd.s
	s.mu.Lock()
	defer s.mu.Unlock()
	for {
		if s.dgram != nil || s.dgramErr != nil {
			n := copy(p, s.dgram)
			err := s.dgramErr
			s.dgram, s.dgramErr = nil, nil
			if err != nil {
				return 0, nil, err
			}
			return n, s.dgramSrc.sockaddr(), nil
		}
		switch {
		case s.closed:
			return 0, nil, poll.ErrNetClosing
		case s.read.handle != 0 && s.read.done == 0:
			return 0, nil, errP3Wait
		case s.read.handle != 0:
			s.udpReceived()
			continue
		case !start:
			return 0, nil, errP3Wait
		}
		if s.rres == nil {
			s.rres = make([]byte, 44)
		}
		wasip3CabiHold()
		status := p3UDPReceive(s.udp, unsafe.Pointer(&s.rres[0]))
		if status&0xf != p3SubtaskReturned {
			fd.start(&s.read, status>>4, p3OpSubtask, 'r')
			return 0, nil, errP3Wait
		}
		s.read = p3Op{done: 1, payload: p3SubtaskReturned}
		s.udpReceived()
	}
}

// udpReceived moves the result of the completed receive operation into
// s.dgram. s.mu must be held.
func (s *p3Socket) udpReceived() {
	defer wasip3CabiRelease()
	if s.read.handle != 0 {
		wasip3SubtaskDrop(s.read.handle)
	}
	state := s.read.payload
	s.read = p3Op{}
	if state != p3SubtaskReturned {
		return
	}
	if err := p3Error(s.rres); err != nil {
		s.dgramErr = err
		return
	}
	ptr, n := byteorder.LEUint32(s.rres[4:]), byteorder.LEUint32(s.rres[8:])
	s.dgram = make([]byte, n)
	copy(s.dgram, unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), n))
	copy(s.dgramSrc[:], s.rres[12:])
}

func (fd *netFD) writeTo(p []byte, sa syscall.Sockaddr) (int, error) {
	params := make([]byte, 48)
	data := make([]byte, len(p)+1)
	copy(data, p)
	byteorder.LEPutUint32(params, fd.s.udp)
	byteorder.LEPutUint32(params[4:], uint32(uintptr(unsafe.Pointer(&data[0]))))
	byteorder.LEPutUint32(params[8:], uint32(len(p)))
	if sa != nil {
		a, err := p3AddrFromSockaddr(sa)
		if err != nil {
			return 0, os.NewSyscallError("sendto", err)
		}
		params[12] = 1
		copy(params[16:], a[:])
	}
	res := make([]byte, p3ResultSize)
	s := fd.s
	started := false
	var state uint32
	err := fd.pfd.RawWrite(func(uintptr) bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.closed {
			return true
		}
		if !started {
			started = true
			status := p3UDPSend(unsafe.Pointer(&params[0]), unsafe.Pointer(&res[0]))
			if status&0xf == p3SubtaskReturned {
				state = p3SubtaskReturned
				return true
			}
			fd.start(&s.write, status>>4, p3OpSubtask, 'w')
		}
		return s.write.handle == 0 || s.write.done != 0
	})
	runtime.KeepAlive(data)
	if err != nil {
		s.stop(&s.write)
	}
	s.mu.Lock()
	if s.write.handle != 0 && s.write.done != 0 {
		state = s.write.payload
		wasip3SubtaskDrop(s.write.handle)
		s.write = p3Op{}
	}
	s.mu.Unlock()
	if err != nil && state != p3SubtaskReturned {
		return 0, err
	}
	if !started || state != p3SubtaskReturned {
		return 0, poll.ErrNetClosing
	}
	if err := p3Error(res); err != nil {
		return 0, wrapSyscallError("sendto", err)
	}
	return len(p), nil
}

func (fd *netFD) readFromInet4(p []byte, from *syscall.SockaddrInet4) (int, error) {
	n, sa, err := fd.readFrom(p)
	if sa, ok := sa.(*syscall.SockaddrInet4); ok {
		*from = *sa
	}
	return n, err
}

func (fd *netFD) readFromInet6(p []byte, from *syscall.SockaddrInet6) (int, error) {
	n, sa, err := fd.readFrom(p)
	if sa, ok := sa.(*syscall.SockaddrInet6); ok {
		*from = *sa
	}
	return n, err
}

func (fd *netFD) readMsg(p []byte, oob []byte, flags int) (n, oobn, retflags int, sa syscall.Sockaddr, err error) {
	n, sa, err = fd.readFrom(p)
	return n, 0, 0, sa, err
}

func (fd *netFD) readMsgInet4(p []byte, oob []byte, flags int, sa *syscall.SockaddrInet4) (n, oobn, retflags int, err error) {
	n, err = fd.readFromInet4(p, sa)
	return n, 0, 0, err
}

func (fd *netFD) readMsgInet6(p []byte, oob []byte, flags int, sa *syscall.SockaddrInet6) (n, oobn, retflags int, err error) {
	n, err = fd.readFromInet6(p, sa)
	return n, 0, 0, err
}

func (fd *netFD) writeMsg(p []byte, oob []byte, sa syscall.Sockaddr) (n int, oobn int, err error) {
	if len(oob) > 0 {
		return 0, 0, syscall.ENOPROTOOPT
	}
	n, err = fd.writeTo(p, sa)
	return n, 0, err
}

// A nil *SockaddrInet4 or *SockaddrInet6 means a connected socket's peer, so
// these don't pass it on as a non-nil interface.

func (fd *netFD) writeMsgInet4(p []byte, oob []byte, sa *syscall.SockaddrInet4) (n int, oobn int, err error) {
	if sa == nil {
		return fd.writeMsg(p, oob, nil)
	}
	return fd.writeMsg(p, oob, sa)
}

func (fd *netFD) writeMsgInet6(p []byte, oob []byte, sa *syscall.SockaddrInet6) (n int, oobn int, err error) {
	if sa == nil {
		return fd.writeMsg(p, oob, nil)
	}
	return fd.writeMsg(p, oob, sa)
}

func (fd *netFD) writeToInet4(p []byte, sa *syscall.SockaddrInet4) (n int, err error) {
	if sa == nil {
		return fd.writeTo(p, nil)
	}
	return fd.writeTo(p, sa)
}

func (fd *netFD) writeToInet6(p []byte, sa *syscall.SockaddrInet6) (n int, err error) {
	if sa == nil {
		return fd.writeTo(p, nil)
	}
	return fd.writeTo(p, sa)
}

func (fd *netFD) dup() (f *os.File, err error) {
	return nil, os.NewSyscallError("dup", syscall.ENOTSUP)
}

func (fd *netFD) SetDeadline(t time.Time) error {
	return fd.pfd.SetDeadline(t)
}

func (fd *netFD) SetReadDeadline(t time.Time) error {
	return fd.pfd.SetReadDeadline(t)
}

func (fd *netFD) SetWriteDeadline(t time.Time) error {
	return fd.pfd.SetWriteDeadline(t)
}

func setDefaultSockopts(s, family, sotype int, ipv6only bool) error {
	return nil
}

func setDefaultListenerSockopts(s int) error {
	return nil
}

func setDefaultMulticastSockopts(s int) error {
	return nil
}

func setReadBuffer(fd *netFD, bytes int) error {
	var res p3Result
	if fd.s.tcp != 0 {
		p3TCPSetReceiveBufferSize(fd.s.tcp, uint64(bytes), &res)
	} else {
		p3UDPSetReceiveBufferSize(fd.s.udp, uint64(bytes), &res)
	}
	return wrapSyscallError("setsockopt", p3Error(res.bytes()))
}

func setWriteBuffer(fd *netFD, bytes int) error {
	var res p3Result
	if fd.s.tcp != 0 {
		p3TCPSetSendBufferSize(fd.s.tcp, uint64(bytes), &res)
	} else {
		p3UDPSetSendBufferSize(fd.s.udp, uint64(bytes), &res)
	}
	return wrapSyscallError("setsockopt", p3Error(res.bytes()))
}

func setKeepAlive(fd *netFD, keepalive bool) error {
	if fd.s.tcp == 0 {
		return syscall.ENOPROTOOPT
	}
	var res p3Result
	p3TCPSetKeepAliveEnabled(fd.s.tcp, keepalive, &res)
	return wrapSyscallError("setsockopt", p3Error(res.bytes()))
}

func setKeepAliveIdle(fd *netFD, d time.Duration) error {
	if d == 0 {
		d = defaultTCPKeepAliveIdle
	} else if d < 0 {
		return nil
	}
	var res p3Result
	p3TCPSetKeepAliveIdleTime(fd.s.tcp, uint64(roundDurationUp(d, time.Second)*time.Second), &res)
	return wrapSyscallError("setsockopt", p3Error(res.bytes()))
}

func setKeepAliveInterval(fd *netFD, d time.Duration) error {
	if d == 0 {
		d = defaultTCPKeepAliveInterval
	} else if d < 0 {
		return nil
	}
	var res p3Result
	p3TCPSetKeepAliveInterval(fd.s.tcp, uint64(roundDurationUp(d, time.Second)*time.Second), &res)
	return wrapSyscallError("setsockopt", p3Error(res.bytes()))
}

func setKeepAliveCount(fd *netFD, n int) error {
	if n == 0 {
		n = defaultTCPKeepAliveCount
	} else if n < 0 {
		return nil
	}
	var res p3Result
	p3TCPSetKeepAliveCount(fd.s.tcp, uint32(n), &res)
	return wrapSyscallError("setsockopt", p3Error(res.bytes()))
}

func setNoDelay(fd *netFD, noDelay bool) error {
	return syscall.ENOPROTOOPT
}

func setLinger(fd *netFD, sec int) error {
	return syscall.ENOPROTOOPT
}
