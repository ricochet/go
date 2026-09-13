// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build wasip3

package http

import (
	"context"
	"errors"
	"io"
	"maps"
	"net/textproto"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unsafe"
)

// RoundTrip should be an internal detail,
// but widely used packages access it using linkname.
// Notable members of the hall of shame include:
//   - github.com/erda-project/erda-infra
//
// Do not remove or change the type signature.
// See go.dev/issue/67401.
//
//go:linkname badRoundTrip net/http.(*Transport).RoundTrip
func badRoundTrip(*Transport, *Request) (*Response, error)

// p3HTTPDisabled makes RoundTrip always use the connections that the
// Transport makes, for tests of their behavior.
var p3HTTPDisabled bool

// RoundTrip implements the [RoundTripper] interface.
//
// For higher-level HTTP client support (such as handling of cookies
// and redirects), see [Get], [Post], and the [Client] type.
//
// Like the RoundTripper interface, the error types returned
// by RoundTrip are unspecified.
//
// On wasip3, RoundTrip sends http and https requests with the host's
// wasi:http client, which makes the connections, unless one of the
// Transport's dial functions or its TLS configuration is set, or the request
// has a Host other than its URL's (wasi:http connects to the host that the
// request names): then the Transport makes the connections itself. The
// host's client doesn't use the Transport's proxy, connection, protocol and
// compression settings.
func (t *Transport) RoundTrip(req *Request) (*Response, error) {
	if t == nil {
		panic("transport is nil")
	}
	// The Transport has a documented contract that states that if the dial
	// functions are set, they will be used to set up the connections.
	if t.Dial != nil || t.DialContext != nil || t.DialTLS != nil || t.DialTLSContext != nil ||
		t.TLSClientConfig != nil || p3HTTPDisabled || req.URL == nil ||
		(req.URL.Scheme != "http" && req.URL.Scheme != "https") ||
		(req.Host != "" && req.Host != req.URL.Host) {
		return t.roundTrip(req)
	}
	return p3RoundTrip(req)
}

// p3RoundTrip sends req with wasi:http/client.
func p3RoundTrip(req *Request) (*Response, error) {
	ctx := req.Context()
	method := req.Method
	if method == "" {
		method = MethodGet
	}
	if !validMethod(method) {
		req.closeBody()
		return nil, errors.New("net/http: invalid method " + strconv.Quote(method))
	}
	if req.URL.Host == "" {
		req.closeBody()
		return nil, errors.New("http: no Host in request URL")
	}
	if err := ctx.Err(); err != nil {
		req.closeBody()
		return nil, context.Cause(ctx)
	}

	c := new(p3Cancel)
	stop := context.AfterFunc(ctx, func() {
		c.interrupt(context.Cause(ctx))
	})
	request, err := p3NewRequest(c, req, method)
	if err != nil {
		stop()
		return nil, err
	}

	res := new(p3Value)
	wasip3CabiHold()
	status := p3Send(request, unsafe.Pointer(res))
	if status&0xf != p3SubtaskReturned {
		subtask := status >> 4
		if _, ok := c.wait(subtask); !ok && p3SubtaskCancel(subtask) != p3SubtaskReturned {
			wasip3SubtaskDrop(subtask)
			wasip3CabiRelease()
			stop()
			return nil, c.error()
		}
		wasip3SubtaskDrop(subtask)
	}
	if *res.u8(0) != 0 {
		err := p3ErrorCode(res, 8)
		wasip3CabiRelease()
		stop()
		if cerr := c.error(); cerr != nil {
			return nil, cerr
		}
		return nil, err
	}
	wasip3CabiRelease()
	response := *res.u32(8)
	if cerr := c.error(); cerr != nil {
		// Cancelled as the response arrived.
		p3ResponseDrop(response)
		stop()
		return nil, cerr
	}

	code := int(p3ResponseStatusCode(response))
	header := p3Header(p3ResponseHeaders(response))
	pair := p3ResultNew()
	var ret [2]uint32
	p3ResponseConsumeBody(response, uint32(pair), &ret)
	body := &p3Body{
		cancel:   c,
		stop:     stop,
		stream:   ret[0],
		trailers: ret[1],
		result:   uint32(pair >> 32),
	}
	contentLength := int64(-1)
	if cl := header.Get("Content-Length"); cl != "" {
		if n, err := strconv.ParseInt(cl, 10, 64); err == nil && n >= 0 {
			contentLength = n
		}
	}
	statusLine := strconv.Itoa(code)
	if text := StatusText(code); text != "" {
		statusLine += " " + text
	}
	resp := &Response{
		Status:        statusLine,
		StatusCode:    code,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        header,
		Body:          body,
		ContentLength: contentLength,
		Request:       req,
	}
	// As the Transport does, move the declared trailers to Trailer, where
	// the body fills in their values at its end.
	if declared, ok := header["Trailer"]; ok {
		header.Del("Trailer")
		resp.Trailer = make(Header)
		for _, keys := range declared {
			for key := range strings.SplitSeq(keys, ",") {
				if key = textproto.TrimString(key); key != "" {
					resp.Trailer[CanonicalHeaderKey(key)] = nil
				}
			}
		}
	}
	body.resp = resp
	return resp, nil
}

// p3NewRequest returns a request resource for req, and starts writing its
// body.
func p3NewRequest(c *p3Cancel, req *Request, method string) (uint32, error) {
	fields := make([]string, 0, 2*len(req.Header)+6)
	for key, values := range req.Header {
		if p3Forbidden(key) || key == "User-Agent" {
			continue
		}
		for _, v := range values {
			fields = append(fields, key, v)
		}
	}
	userAgent := defaultUserAgent
	if req.Header.has("User-Agent") {
		userAgent = req.Header.Get("User-Agent")
	}
	if userAgent != "" {
		fields = append(fields, "User-Agent", userAgent)
	}
	if u := req.URL.User; u != nil && req.Header.Get("Authorization") == "" {
		password, _ := u.Password()
		fields = append(fields, "Authorization", "Basic "+basicAuth(u.Username(), password))
	}
	if len(req.Trailer) > 0 {
		// Declare the trailers, as the Transport does (and clients may only
		// send declared trailers).
		fields = append(fields, "Trailer", strings.Join(slices.Sorted(maps.Keys(req.Trailer)), ","))
	}
	length := req.outgoingLength()
	if length > 0 && req.Header.Get("Content-Length") == "" {
		fields = append(fields, "Content-Length", strconv.FormatInt(length, 10))
	}
	headers, err := p3NewFields(fields)
	if err != nil {
		req.closeBody()
		return 0, err
	}

	var contents, contentsWriter uint32
	if length != 0 {
		pair := p3BytesNew()
		contents, contentsWriter = uint32(pair), uint32(pair>>32)
	}
	pair := p3TrailersNew()
	trailers, trailersWriter := uint32(pair), uint32(pair>>32)
	var ret [2]uint32
	p3RequestNew(headers, b2u32(contents != 0), contents, trailers, 0, 0, &ret)
	request := ret[0]
	p3ResultDropReadable(ret[1]) // the result of sending the body

	host := req.Host
	if host == "" {
		host = req.URL.Host
	}
	host = removeZone(host)
	path := req.URL.RequestURI()
	scheme := uint32(0) // HTTP
	if req.URL.Scheme == "https" {
		scheme = 1
	}
	m, other := p3Method(method)
	ok := p3RequestSetMethod(request, m, p3StringPtr(other), uint32(len(other))) == 0 &&
		p3RequestSetScheme(request, 1, scheme, nil, 0) == 0 &&
		p3RequestSetAuthority(request, 1, p3StringPtr(host), uint32(len(host))) == 0 &&
		p3RequestSetPathWithQuery(request, 1, p3StringPtr(path), uint32(len(path))) == 0
	if !ok {
		// Dropping the request drops the ends of its body that it had.
		p3RequestDrop(request)
		if contentsWriter != 0 {
			p3BytesDropWritable(contentsWriter)
		}
		p3WriteTrailers(trailersWriter, nil, nil)
		req.closeBody()
		return 0, errors.New("net/http: invalid request for wasi:http: " + method + " " + req.URL.Redacted())
	}
	go p3WriteBody(c, req, contentsWriter, trailersWriter)
	return request, nil
}

func b2u32(b bool) uint32 {
	if b {
		return 1
	}
	return 0
}

// p3Method returns the case of method, and its name for the other case.
func p3Method(method string) (uint32, string) {
	for i, m := range [...]string{
		MethodGet, MethodHead, MethodPost, MethodPut, MethodDelete,
		MethodConnect, MethodOptions, MethodTrace, MethodPatch,
	} {
		if method == m {
			return uint32(i), ""
		}
	}
	return 9, method
}

// p3WriteBody writes req's body to the contents stream, if there is one,
// and then its trailers.
func p3WriteBody(c *p3Cancel, req *Request, contents, trailers uint32) {
	var err error
	if contents != 0 {
		err = p3CopyBody(c, contents, req.Body)
		p3BytesDropWritable(contents)
	}
	req.closeBody()
	p3WriteTrailers(trailers, req.Trailer, err)
}

// p3CopyBody writes body to stream.
func p3CopyBody(c *p3Cancel, stream uint32, body io.Reader) error {
	buf := p3Bufs.Get().(*[p3BufSize]byte)
	defer p3Bufs.Put(buf)
	for {
		n, rerr := body.Read(buf[:])
		for off := 0; off < n; {
			code := p3BytesWrite(stream, unsafe.Pointer(&buf[off]), uint32(n-off))
			if code == p3Blocked {
				payload, ok := c.wait(stream)
				if ok {
					code = payload
				} else {
					code = p3BytesCancelWrite(stream)
				}
			}
			off += int(code >> 4)
			switch code & 0xf {
			case p3Dropped:
				return errors.New("net/http: wasi:http client stopped reading the request body")
			case p3Cancelled:
				return c.error()
			}
		}
		if rerr == io.EOF {
			return nil
		}
		if rerr != nil {
			return rerr
		}
	}
}

// p3WriteTrailers writes the trailers of a request, or the error that ended
// its body, to the trailers future, and drops it.
func p3WriteTrailers(writer uint32, trailer Header, err error) {
	v := new(p3Value)
	fields := uint32(0)
	switch {
	case err != nil:
		*v.u8(0) = 1 // err
		*v.u8(8) = p3InternalError
	case len(trailer) > 0:
		var list []string
		for key, values := range trailer {
			for _, value := range values {
				list = append(list, key, value)
			}
		}
		if f, err := p3NewFields(list); err == nil {
			fields = f
			*v.u8(8) = 1 // some
			*v.u32(12) = fields
		} else {
			*v.u8(0) = 1
			*v.u8(8) = p3InternalError
		}
	}
	code := p3TrailersWrite(writer, unsafe.Pointer(v))
	if code == p3Blocked {
		// The host reads the trailers once the body ends, or drops the
		// future with the request, so this doesn't need cancelling (a
		// future's writer can't be dropped before it's written).
		code = p3Await(writer)
	}
	if code&0xf != p3Completed && fields != 0 {
		p3FieldsDrop(fields)
	}
	p3TrailersDropWritable(writer)
}

// A p3Body is a response body, which the host's HTTP client streams.
type p3Body struct {
	cancel *p3Cancel
	stop   func() bool
	resp   *Response

	mu       sync.Mutex
	stream   uint32 // 0 once dropped
	trailers uint32 // the trailers future, 0 once dropped
	result   uint32 // the future for the result of handling the response, 0 once written
	buf      *[p3BufSize]byte
	value    p3Value
	err      error
}

func (b *p3Body) Read(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for b.err == nil && len(p) > 0 {
		if b.buf == nil {
			b.buf = p3Bufs.Get().(*[p3BufSize]byte)
		}
		code := p3BytesRead(b.stream, unsafe.Pointer(&b.buf[0]), uint32(min(len(p), len(b.buf))))
		if code == p3Blocked {
			payload, ok := b.cancel.wait(b.stream)
			if ok {
				code = payload
			} else {
				code = p3BytesCancelRead(b.stream)
			}
		}
		n := copy(p, b.buf[:code>>4])
		switch code & 0xf {
		case p3Dropped:
			b.err = b.end()
		case p3Cancelled:
			b.err = b.cancel.error()
		}
		if n > 0 {
			return n, nil
		}
	}
	return 0, b.err
}

// end is called when the body's contents have ended. It reads the
// trailers, and returns io.EOF or why the body didn't arrive. b.mu must be
// held.
func (b *p3Body) end() error {
	p3BytesDropReadable(b.stream)
	b.stream = 0
	wasip3CabiHold()
	defer wasip3CabiRelease()
	v := &b.value
	code := p3TrailersRead(b.trailers, unsafe.Pointer(v))
	if code == p3Blocked {
		payload, ok := b.cancel.wait(b.trailers)
		if ok {
			code = payload
		} else {
			code = p3TrailersCancelRead(b.trailers)
		}
	}
	err := error(io.EOF)
	switch {
	case code&0xf == p3Cancelled:
		err = b.cancel.error()
	case code&0xf == p3Dropped:
		err = io.ErrUnexpectedEOF
	case *v.u8(0) != 0:
		err = p3ErrorCode(v, 8)
	case *v.u8(8) != 0:
		if b.resp.Trailer == nil {
			b.resp.Trailer = make(Header)
		}
		for key, values := range p3Header(*v.u32(12)) {
			b.resp.Trailer[key] = values
		}
	}
	b.release()
	return err
}

func (b *p3Body) Close() error {
	b.cancel.interrupt(errReadOnClosedResBody)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.err == nil {
		b.err = errReadOnClosedResBody
	}
	b.release()
	return nil
}

// release frees the body's resources, reporting the response handled.
// b.mu must be held.
func (b *p3Body) release() {
	if b.stream != 0 {
		p3BytesDropReadable(b.stream)
		b.stream = 0
	}
	if b.trailers != 0 {
		p3TrailersDropReadable(b.trailers)
		b.trailers = 0
	}
	if b.result != 0 {
		// The host reads the result right away. It's ok, in any case: the
		// client is done with the response.
		v := new(p3Value)
		if p3ResultWrite(b.result, unsafe.Pointer(v)) == p3Blocked {
			p3Await(b.result)
		}
		p3ResultDropWritable(b.result)
		b.result = 0
	}
	if b.buf != nil {
		p3Bufs.Put(b.buf)
		b.buf = nil
	}
	b.stop()
}
