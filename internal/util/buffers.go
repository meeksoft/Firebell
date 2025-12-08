// Package util provides shared utilities for ai-chime.
package util

import (
	"sync"
)

// DefaultBufferSize is the default size for pooled buffers.
const DefaultBufferSize = 4096

// bufferPool is a sync.Pool for reusable byte buffers.
// This reduces allocations in hot paths like log tailing.
var bufferPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, DefaultBufferSize)
		return &buf
	},
}

// GetBuffer retrieves a buffer from the pool.
// The buffer should be returned via PutBuffer when done.
func GetBuffer() *[]byte {
	return bufferPool.Get().(*[]byte)
}

// PutBuffer returns a buffer to the pool for reuse.
func PutBuffer(buf *[]byte) {
	if buf == nil || len(*buf) != DefaultBufferSize {
		return // Don't pool non-standard buffers
	}
	bufferPool.Put(buf)
}

// GetBufferN retrieves a buffer of at least size n.
// If n <= DefaultBufferSize, uses the pool; otherwise allocates.
func GetBufferN(n int) []byte {
	if n <= DefaultBufferSize {
		buf := GetBuffer()
		return (*buf)[:n]
	}
	return make([]byte, n)
}
