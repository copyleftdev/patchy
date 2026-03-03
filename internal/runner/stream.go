package runner

import "sync"

// BoundedBuffer is a fixed-capacity buffer that keeps the last N bytes.
// Prevents OOM from chatty tool output.
type BoundedBuffer struct {
	mu        sync.Mutex
	buf       []byte
	capacity  int64
	written   int64
	truncated bool
}

func NewBoundedBuffer(capacity int64) *BoundedBuffer {
	return &BoundedBuffer{
		buf:      make([]byte, 0, capacity),
		capacity: capacity,
	}
}

// Write implements io.Writer. Bytes beyond capacity overwrite oldest.
func (b *BoundedBuffer) Write(p []byte) (n int, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	n = len(p)
	b.written += int64(n)

	if int64(n) >= b.capacity {
		// Data alone exceeds capacity — keep only the tail
		b.buf = make([]byte, b.capacity)
		copy(b.buf, p[int64(n)-b.capacity:])
		b.truncated = true
		return n, nil
	}

	b.buf = append(b.buf, p...)
	if int64(len(b.buf)) > b.capacity {
		excess := int64(len(b.buf)) - b.capacity
		b.buf = b.buf[excess:]
		b.truncated = true
	}

	return n, nil
}

// Bytes returns the captured content.
func (b *BoundedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]byte, len(b.buf))
	copy(out, b.buf)
	return out
}

// String returns captured content as string.
func (b *BoundedBuffer) String() string {
	return string(b.Bytes())
}

// Truncated returns true if content was lost due to overflow.
func (b *BoundedBuffer) Truncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.truncated
}

// BytesWritten returns total bytes written (may exceed capacity).
func (b *BoundedBuffer) BytesWritten() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.written
}
