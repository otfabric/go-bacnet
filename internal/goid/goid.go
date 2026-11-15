// SPDX-License-Identifier: MIT

// Package goid returns the identifier of the calling goroutine.
//
// The server runs the application's code (hooks, objects, the diagnostic
// function) on goroutines of its own and waits for them in Close. A Close
// called from such code would wait for the goroutine it runs on; with the
// identifier Close can tell that it was called on one of the server's
// goroutines and leave that one out. The identifier is read once when such
// a goroutine starts and once per Close, never per request.
package goid

import (
	"bytes"
	"runtime"
	"strconv"
)

// ID returns the identifier of the calling goroutine, 0 if the runtime does
// not report it.
func ID() uint64 {
	var buf [64]byte
	return parse(buf[:runtime.Stack(buf[:], false)])
}

// parse reads the identifier from the first line of a goroutine's stack:
// "goroutine 12 [running]:".
func parse(stack []byte) uint64 {
	rest, ok := bytes.CutPrefix(stack, []byte("goroutine "))
	if !ok {
		return 0
	}
	n := bytes.IndexByte(rest, ' ')
	if n < 0 {
		return 0
	}
	id, err := strconv.ParseUint(string(rest[:n]), 10, 64)
	if err != nil {
		return 0
	}
	return id
}
