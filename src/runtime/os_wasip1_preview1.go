// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build wasip1 && !wasip3

package runtime

import "unsafe"

// WASI preview 1 imports and the OS functions implemented with them.
// GOOS=wasip3 implements these with WASI 0.3 instead (see os_wasip3.go).

//go:wasmimport wasi_snapshot_preview1 proc_exit
func exit(code int32)

// wasip3BeforeIdle is for async exports on GOOS=wasip3 (see export_wasip3.go).
func wasip3BeforeIdle() *g { return nil }

//go:wasmimport wasi_snapshot_preview1 args_get
//go:noescape
func args_get(argv *uintptr32, argvBuf *byte) errno

//go:wasmimport wasi_snapshot_preview1 args_sizes_get
//go:noescape
func args_sizes_get(argc, argvBufLen *size) errno

//go:wasmimport wasi_snapshot_preview1 clock_time_get
//go:noescape
func clock_time_get(clock_id clockid, precision timestamp, time *timestamp) errno

//go:wasmimport wasi_snapshot_preview1 environ_get
//go:noescape
func environ_get(environ *uintptr32, environBuf *byte) errno

//go:wasmimport wasi_snapshot_preview1 environ_sizes_get
//go:noescape
func environ_sizes_get(environCount, environBufLen *size) errno

//go:wasmimport wasi_snapshot_preview1 fd_write
//go:noescape
func fd_write(fd int32, iovs unsafe.Pointer, iovsLen size, nwritten *size) errno

//go:wasmimport wasi_snapshot_preview1 random_get
//go:noescape
func random_get(buf *byte, bufLen size) errno

//go:wasmimport wasi_snapshot_preview1 poll_oneoff
//go:noescape
func poll_oneoff(in *subscription, out *event, nsubscriptions size, nevents *size) errno

func write1(fd uintptr, p unsafe.Pointer, n int32) int32 {
	iov := iovec{
		buf:    uintptr32(uintptr(p)),
		bufLen: size(n),
	}
	var nwritten size
	if fd_write(int32(fd), unsafe.Pointer(&iov), 1, &nwritten) != 0 {
		throw("fd_write failed")
	}
	return int32(nwritten)
}

func usleep(usec uint32) {
	var in subscription
	var out event
	var nevents size

	eventtype := in.u.eventtype()
	*eventtype = eventtypeClock

	subscription := in.u.subscriptionClock()
	subscription.id = clockMonotonic
	subscription.timeout = timestamp(usec) * 1e3
	subscription.precision = 1e3

	if poll_oneoff(&in, &out, 1, &nevents) != 0 {
		throw("wasi_snapshot_preview1.poll_oneoff")
	}
}

func readRandom(r []byte) int {
	if random_get(&r[0], size(len(r))) != 0 {
		return 0
	}
	return len(r)
}

func goenvs() {
	// arguments
	var argc size
	var argvBufLen size
	if args_sizes_get(&argc, &argvBufLen) != 0 {
		throw("args_sizes_get failed")
	}

	argslice = make([]string, argc)
	if argc > 0 {
		argv := make([]uintptr32, argc)
		argvBuf := make([]byte, argvBufLen)
		if args_get(&argv[0], &argvBuf[0]) != 0 {
			throw("args_get failed")
		}

		for i := range argslice {
			start := argv[i] - uintptr32(uintptr(unsafe.Pointer(&argvBuf[0])))
			end := start
			for argvBuf[end] != 0 {
				end++
			}
			argslice[i] = string(argvBuf[start:end])
		}
	}

	// environment
	var environCount size
	var environBufLen size
	if environ_sizes_get(&environCount, &environBufLen) != 0 {
		throw("environ_sizes_get failed")
	}

	envs = make([]string, environCount)
	if environCount > 0 {
		environ := make([]uintptr32, environCount)
		environBuf := make([]byte, environBufLen)
		if environ_get(&environ[0], &environBuf[0]) != 0 {
			throw("environ_get failed")
		}

		for i := range envs {
			start := environ[i] - uintptr32(uintptr(unsafe.Pointer(&environBuf[0])))
			end := start
			for environBuf[end] != 0 {
				end++
			}
			envs[i] = string(environBuf[start:end])
		}
	}
}

func walltime() (sec int64, nsec int32) {
	return walltime1()
}

func walltime1() (sec int64, nsec int32) {
	var time timestamp
	if clock_time_get(clockRealtime, 0, &time) != 0 {
		throw("clock_time_get failed")
	}
	return int64(time / 1000000000), int32(time % 1000000000)
}

func nanotime1() int64 {
	var time timestamp
	if clock_time_get(clockMonotonic, 0, &time) != 0 {
		throw("clock_time_get failed")
	}
	return int64(time)
}

//go:wasmimport wasi_snapshot_preview1 sched_yield
func sched_yield() errno
