package transport

import "bytes"

type cappedBuffer struct {
	buf       bytes.Buffer
	limit     int64
	truncated bool
}

func newCappedBuffer(limit int64) *cappedBuffer {
	if limit <= 0 {
		limit = 1024 * 1024
	}
	return &cappedBuffer{limit: limit}
}
func (b *cappedBuffer) Write(p []byte) (int, error) {
	remaining := b.limit - int64(b.buf.Len())
	if remaining > 0 {
		if int64(len(p)) > remaining {
			_, _ = b.buf.Write(p[:remaining])
			b.truncated = true
		} else {
			_, _ = b.buf.Write(p)
		}
	} else if len(p) > 0 {
		b.truncated = true
	}
	return len(p), nil
}
func (b *cappedBuffer) String() string  { return b.buf.String() }
func (b *cappedBuffer) Truncated() bool { return b.truncated }
func limitFor(command, fallback int64) int64 {
	if command > 0 {
		return command
	}
	if fallback > 0 {
		return fallback
	}
	return 1024 * 1024
}
