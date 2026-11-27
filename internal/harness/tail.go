package harness

import "sync"

// Tail is an io.Writer that keeps the last 4 KiB written to it, for stderr.
type Tail struct {
	mu  sync.Mutex
	buf []byte
}

func (t *Tail) Write(b []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, b...)
	if over := len(t.buf) - 4096; over > 0 {
		t.buf = t.buf[over:]
	}
	return len(b), nil
}

// String returns the kept bytes.
func (t *Tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}
