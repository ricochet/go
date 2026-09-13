// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build wasip3

package poll

// RuntimeCtx returns the runtime's poll descriptor for fd, or 0 if fd is not
// pollable. Package net uses it on GOOS=wasip3 to make fd ready when a
// Component Model operation completes.
func (fd *FD) RuntimeCtx() uintptr {
	return fd.pd.runtimeCtx
}
