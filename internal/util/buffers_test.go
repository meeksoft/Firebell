package util

import (
	"testing"
)

func TestGetBuffer(t *testing.T) {
	t.Run("returns buffer of default size", func(t *testing.T) {
		buf := GetBuffer()
		if buf == nil {
			t.Fatal("GetBuffer() returned nil")
		}
		if len(*buf) != DefaultBufferSize {
			t.Errorf("buffer size = %d, want %d", len(*buf), DefaultBufferSize)
		}
		PutBuffer(buf)
	})

	t.Run("put and get reuses buffer", func(t *testing.T) {
		buf1 := GetBuffer()
		(*buf1)[0] = 'X' // Mark it
		PutBuffer(buf1)

		// Get another - might be the same one
		buf2 := GetBuffer()
		if buf2 == nil {
			t.Fatal("GetBuffer() returned nil")
		}
		PutBuffer(buf2)
	})
}

func TestGetBufferN(t *testing.T) {
	t.Run("small buffer uses pool", func(t *testing.T) {
		buf := GetBufferN(100)
		if len(buf) != 100 {
			t.Errorf("buffer size = %d, want 100", len(buf))
		}
	})

	t.Run("exact default size", func(t *testing.T) {
		buf := GetBufferN(DefaultBufferSize)
		if len(buf) != DefaultBufferSize {
			t.Errorf("buffer size = %d, want %d", len(buf), DefaultBufferSize)
		}
	})

	t.Run("large buffer allocates new", func(t *testing.T) {
		size := DefaultBufferSize + 1000
		buf := GetBufferN(size)
		if len(buf) != size {
			t.Errorf("buffer size = %d, want %d", len(buf), size)
		}
	})
}

func TestPutBuffer(t *testing.T) {
	t.Run("nil buffer is ignored", func(t *testing.T) {
		// Should not panic
		PutBuffer(nil)
	})

	t.Run("non-standard size buffer is ignored", func(t *testing.T) {
		nonStandard := make([]byte, 100)
		// Should not panic
		PutBuffer(&nonStandard)
	})
}
