// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build wasip3

package net

import (
	"context"
	"errors"
	"internal/byteorder"
	"unsafe"
)

// On GOOS=wasip3, the host resolves names with
// wasi:sockets/ip-name-lookup, which takes the place of the cgo resolver.
// Other lookups fall back to the Go resolver.

const cgoAvailable = true

var errWasip3LookupUnsupported = errors.New("lookup not supported by wasi:sockets")

func cgoLookupHost(ctx context.Context, name string) (addrs []string, err error) {
	ips, err := cgoLookupIP(ctx, "ip", name)
	if err != nil {
		return nil, err
	}
	for _, ip := range ips {
		addrs = append(addrs, ip.String())
	}
	return addrs, nil
}

func cgoLookupPort(ctx context.Context, network, service string) (port int, err error) {
	return goLookupPort(network, service)
}

func cgoLookupIP(ctx context.Context, network, name string) (addrs []IPAddr, err error) {
	if err := ctx.Err(); err != nil {
		return nil, mapErr(err)
	}
	nameBuf := make([]byte, len(name)+1)
	copy(nameBuf, name)
	res := make([]byte, 12)
	wasip3CabiHold()
	defer wasip3CabiRelease()
	status := p3ResolveAddresses(unsafe.Pointer(&nameBuf[0]), uint32(len(name)), unsafe.Pointer(&res[0]))
	if status&0xf != p3SubtaskReturned {
		// The lookup can't be cancelled when ctx is done, but it isn't
		// waited for.
		subtask := status >> 4
		done := make(chan struct{})
		go func() {
			for {
				if _, state := wasip3Await(subtask); state == p3SubtaskReturned {
					break
				}
			}
			wasip3SubtaskDrop(subtask)
			close(done)
		}()
		select {
		case <-done:
		case <-ctx.Done():
			// Keep the allocator held until the lookup returns.
			wasip3CabiHold()
			go func() {
				<-done
				wasip3CabiRelease()
			}()
			return nil, mapErr(ctx.Err())
		}
	}
	if res[0] != 0 {
		dnsErr := &DNSError{Name: name}
		switch res[4] {
		case 2: // name-unresolvable
			dnsErr.Err = errNoSuchHost.Error()
			dnsErr.IsNotFound = true
		case 3: // temporary-resolver-failure
			dnsErr.Err = "temporary failure in name resolution"
			dnsErr.IsTemporary = true
		default:
			dnsErr.Err = "name resolution failed"
		}
		return nil, dnsErr
	}
	list := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(byteorder.LEUint32(res[4:])))), byteorder.LEUint32(res[8:])*p3IPAddrSize)
	for len(list) >= p3IPAddrSize {
		e := list[:p3IPAddrSize]
		list = list[p3IPAddrSize:]
		var ip IP
		if e[0] == 0 {
			if network == "ip6" {
				continue
			}
			ip = IPv4(e[2], e[3], e[4], e[5])
		} else {
			if network == "ip4" {
				continue
			}
			ip = make(IP, IPv6len)
			for i := range 8 {
				byteorder.BEPutUint16(ip[2*i:], byteorder.LEUint16(e[2+2*i:]))
			}
		}
		addrs = append(addrs, IPAddr{IP: ip})
	}
	if len(addrs) == 0 {
		return nil, &DNSError{Err: errNoSuchHost.Error(), Name: name, IsNotFound: true}
	}
	return addrs, nil
}

func cgoLookupCNAME(ctx context.Context, name string) (cname string, err error) {
	return "", &DNSError{Err: errWasip3LookupUnsupported.Error(), Name: name}
}

func cgoLookupPTR(ctx context.Context, addr string) (ptrs []string, err error) {
	return nil, &DNSError{Err: errWasip3LookupUnsupported.Error(), Name: addr}
}
