// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build wasip3

package runtime

import "unsafe"

// cabi_realloc for GOOS=wasip3.
//
// The host allocates the memory for the strings and lists that it passes to
// the module with cabi_realloc: results of imports, and parameters of
// exports. None of it is in the heap.
//
//   - While the runtime or the syscall package holds the allocator around an
//     import call (see syscall_wasip3CabiHold), the host is lowering the
//     call's results, which the caller copies out before it releases the
//     allocator. They come from chunks that the allocator bumps through and
//     reuses once nothing holds it.
//
//   - Otherwise, in a c-shared module, the host is lowering the parameters of
//     an export call. They come from an arena of the call, which is freed
//     when the call returns, or for an async export (see export_wasip3.go),
//     when its function returns. So the memory of an export's string and
//     pointer parameters is only valid until its function returns.

// cabiRealloc is exported as cabi_realloc (see cmd/link/internal/wasm), unless
// the program exports its own, as Component Model bindings do. Its wrapper
// runs it on the system stack (see cmd/internal/obj/wasm).
//
//go:wasmexport cabi_realloc_go
func cabiRealloc(old unsafe.Pointer, oldSize, align, newSize uint32) unsafe.Pointer {
	if old != nil || oldSize != 0 {
		// The host only allocates new values.
		throw("runtime: cabi_realloc called to reallocate")
	}
	if newSize == 0 {
		return unsafe.Pointer(uintptr(align))
	}
	if p3Cabi.holds > 0 || !islibrary {
		return cabiBump(uintptr(newSize), uintptr(align))
	}
	return p3CabiPending.alloc(uintptr(newSize), uintptr(align))
}

var (
	// p3CabiPending holds the allocations for the parameters of the next
	// export call, made since the last one started.
	p3CabiPending p3Arena

	// p3CabiCall holds those of the running export call.
	p3CabiCall p3Arena
)

// wasip3ExportEnter is called by //go:wasmexport wrappers when a call starts.
//
//go:nosplit
func wasip3ExportEnter() {
	p3CabiCall.free()
	p3CabiCall = p3CabiPending
	p3CabiPending = p3Arena{}
}

// wasip3ExportExit is called by //go:wasmexport wrappers when a call returns.
//
//go:nosplit
func wasip3ExportExit() {
	p3CabiCall.free()
}

// A p3Arena is memory that's freed all at once. It consists of chunks of
// linear memory, which start with a header: the chunk's size, and the next
// chunk of the arena (or of a free list). Chunks are aligned to 8.
type p3Arena struct {
	chunks    uintptr // the newest chunk, or 0
	next, end uintptr // the unused part of the newest chunk
}

const (
	p3ChunkHeader = 16

	// Chunks are small, carved out of a page, or large, whole pages.
	p3SmallChunk = 4 << 10
)

// Free lists of small and large chunks.
var p3FreeSmall, p3FreeLarge uintptr

// alloc allocates n bytes aligned to align.
//
//go:nosplit
func (a *p3Arena) alloc(n, align uintptr) unsafe.Pointer {
	p := alignUp(a.next, align)
	if a.chunks == 0 || p+n > a.end {
		c := p3TakeChunk(p3ChunkHeader + alignUp(n, 8))
		if c == 0 {
			return nil
		}
		*(*uintptr)(unsafe.Pointer(c + 8)) = a.chunks
		a.chunks, a.end = c, c+*(*uintptr)(unsafe.Pointer(c))
		p = c + p3ChunkHeader
	}
	a.next = p + n
	return unsafe.Pointer(p)
}

// free returns the arena's chunks to the free lists.
//
//go:nosplit
func (a *p3Arena) free() {
	for c := a.chunks; c != 0; {
		next := *(*uintptr)(unsafe.Pointer(c + 8))
		p3FreeChunk(c)
		c = next
	}
	*a = p3Arena{}
}

// p3FreeChunk adds chunk c to its free list.
//
//go:nosplit
func p3FreeChunk(c uintptr) {
	list := &p3FreeLarge
	if *(*uintptr)(unsafe.Pointer(c)) == p3SmallChunk {
		list = &p3FreeSmall
	}
	*(*uintptr)(unsafe.Pointer(c + 8)) = *list
	*list = c
}

// p3TakeChunk returns a chunk of at least n bytes, whose header has its size:
// a free one, or a new one.
//
//go:nosplit
func p3TakeChunk(n uintptr) uintptr {
	if n <= p3SmallChunk {
		if p3FreeSmall == 0 {
			page := uintptr(sbrk(physPageSize))
			if page == 0 {
				return 0
			}
			for c := page; c < page+physPageSize; c += p3SmallChunk {
				*(*uintptr)(unsafe.Pointer(c)) = p3SmallChunk
				p3FreeChunk(c)
			}
		}
		c := p3FreeSmall
		p3FreeSmall = *(*uintptr)(unsafe.Pointer(c + 8))
		return c
	}
	for prev := &p3FreeLarge; *prev != 0; prev = (*uintptr)(unsafe.Pointer(*prev + 8)) {
		c := *prev
		if *(*uintptr)(unsafe.Pointer(c)) >= n {
			*prev = *(*uintptr)(unsafe.Pointer(c + 8))
			return c
		}
	}
	size := alignUp(n, physPageSize)
	c := uintptr(sbrk(size))
	if c != 0 {
		*(*uintptr)(unsafe.Pointer(c)) = size
	}
	return c
}

// cabiChunk is the start of the chunk that cabiNext and cabiEnd delimit the
// unused part of.
var cabiChunk uintptr

// cabiBump allocates n bytes aligned to align from the current chunk, or from
// a new chunk if they don't fit.
func cabiBump(n, align uintptr) unsafe.Pointer {
	p := alignUp(cabiNext, align)
	if cabiNext == 0 || p+n > cabiEnd {
		// Chunks double, so that a new one doesn't have to be made again
		// for results of the same size, since the allocator abandons the
		// earlier chunks when it reuses the newest (see
		// syscall_wasip3CabiRelease).
		size := max(64<<10, 2*(cabiEnd-cabiChunk), alignUp(n, physPageSize))
		base := sbrk(size)
		if base == nil {
			return nil
		}
		cabiChunk, cabiEnd = uintptr(base), uintptr(base)+size
		p = cabiChunk
	}
	cabiNext = p + n
	return unsafe.Pointer(p)
}
