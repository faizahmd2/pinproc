//go:build linux

package journal

import (
	"errors"
	"syscall"
)

// readKmsg drains the kernel ring buffer without blocking: each read(2) on
// /dev/kmsg returns one record, EAGAIN marks the end, EPIPE a record that was
// overwritten while reading (skipped).
func readKmsg(path string) ([]kmsgRecord, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	defer syscall.Close(fd)
	buf := make([]byte, 8192)
	var out []kmsgRecord
	for i := 0; i < 20000; i++ {
		n, err := syscall.Read(fd, buf)
		if err != nil {
			if errors.Is(err, syscall.EPIPE) {
				continue
			}
			if errors.Is(err, syscall.EAGAIN) {
				break
			}
			return out, err
		}
		if n <= 0 {
			break
		}
		if r, ok := parseKmsg(buf[:n]); ok {
			out = append(out, r)
		}
	}
	return out, nil
}
