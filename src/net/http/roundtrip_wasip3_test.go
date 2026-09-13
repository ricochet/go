// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build wasip3

package http_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	. "net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// Tests of sending requests with the host's wasi:http client.

func newWASIHTTPServer(t *testing.T, h HandlerFunc) (*httptest.Server, *Client) {
	t.Cleanup(SetWASIHTTP(true))
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return ts, &Client{Transport: &Transport{}}
}

func TestWASIHTTPGet(t *testing.T) {
	ts, c := newWASIHTTPServer(t, func(w ResponseWriter, r *Request) {
		w.Header().Set("X-Echo", r.Header.Get("X-Test"))
		w.Header().Set("X-Host", r.Host)
		w.Header().Set("X-User-Agent", r.UserAgent())
		w.Header().Set("Trailer", "X-Trailer")
		w.WriteHeader(StatusTeapot)
		io.WriteString(w, "query "+r.URL.RawQuery)
		w.Header().Set("X-Trailer", "done")
	})
	req, _ := NewRequest("GET", ts.URL+"/path?q=1", nil)
	req.Header.Set("X-Test", "value")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != StatusTeapot || resp.Status != "418 I'm a teapot" {
		t.Errorf("status = %q", resp.Status)
	}
	if string(body) != "query q=1" {
		t.Errorf("body = %q", body)
	}
	for key, want := range map[string]string{
		"X-Echo":       "value",
		"X-Host":       strings.TrimPrefix(ts.URL, "http://"),
		"X-User-Agent": "Go-http-client/1.1",
	} {
		if got := resp.Header.Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if got := resp.Trailer.Get("X-Trailer"); got != "done" {
		t.Errorf("trailer = %q, want %q", got, "done")
	}
}

func TestWASIHTTPPostStream(t *testing.T) {
	ts, c := newWASIHTTPServer(t, func(w ResponseWriter, r *Request) {
		n, err := io.Copy(io.Discard, r.Body)
		fmt.Fprintf(w, "%d %v %s", n, err, r.Trailer.Get("X-Request-Trailer"))
	})
	const size = 1 << 20
	pr, pw := io.Pipe()
	go func() {
		chunk := bytes.Repeat([]byte("x"), 7919)
		for n := 0; n < size; n += len(chunk) {
			pw.Write(chunk[:min(len(chunk), size-n)])
		}
		pw.Close()
	}()
	req, _ := NewRequest("POST", ts.URL, pr)
	req.Trailer = Header{"X-Request-Trailer": nil}
	req.Trailer.Set("X-Request-Trailer", "sent")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if want := fmt.Sprintf("%d <nil> sent", size); string(body) != want {
		t.Errorf("body = %q, want %q", body, want)
	}
}

func TestWASIHTTPConcurrent(t *testing.T) {
	ts, c := newWASIHTTPServer(t, func(w ResponseWriter, r *Request) {
		io.WriteString(w, r.URL.RawQuery)
	})
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Go(func() {
			resp, err := c.Get(fmt.Sprintf("%s/?%d", ts.URL, i))
			if err != nil {
				t.Error(err)
				return
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if string(body) != fmt.Sprint(i) {
				t.Errorf("body = %q, want %d", body, i)
			}
		})
	}
	wg.Wait()
}

func TestWASIHTTPCancel(t *testing.T) {
	unblock := make(chan struct{})
	ts, c := newWASIHTTPServer(t, func(w ResponseWriter, r *Request) {
		if r.URL.Path == "/body" {
			io.WriteString(w, "partial")
			w.(Flusher).Flush()
		}
		select {
		case <-unblock:
		case <-r.Context().Done():
		}
	})
	defer close(unblock)

	t.Run("response", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		req, _ := NewRequestWithContext(ctx, "GET", ts.URL, nil)
		if _, err := c.Do(req); !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Do = %v, want %v", err, context.DeadlineExceeded)
		}
	})

	t.Run("body", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		req, _ := NewRequestWithContext(ctx, "GET", ts.URL+"/body", nil)
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		buf := make([]byte, len("partial"))
		if _, err := io.ReadFull(resp.Body, buf); err != nil {
			t.Fatal(err)
		}
		time.AfterFunc(50*time.Millisecond, cancel)
		if _, err := resp.Body.Read(buf); !errors.Is(err, context.Canceled) {
			t.Errorf("Read = %v, want %v", err, context.Canceled)
		}
	})

	t.Run("close", func(t *testing.T) {
		resp, err := c.Get(ts.URL + "/body")
		if err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, len("partial"))
		if _, err := io.ReadFull(resp.Body, buf); err != nil {
			t.Fatal(err)
		}
		time.AfterFunc(50*time.Millisecond, func() { resp.Body.Close() })
		if _, err := resp.Body.Read(buf); err == nil || !strings.Contains(err.Error(), "closed") {
			t.Errorf("Read = %v, want an error about the closed body", err)
		}
	})
}

func TestWASIHTTPRefused(t *testing.T) {
	t.Cleanup(SetWASIHTTP(true))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	_, err = (&Client{Transport: &Transport{}}).Get("http://" + addr)
	if err == nil || !strings.Contains(err.Error(), "wasi:http") {
		t.Errorf("Get = %v, want an error from wasi:http", err)
	}
}
