package runner

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"io"
	"sort"
	"sync"
	"time"
)

// crockford is the Crockford base32 alphabet used by ULIDs.
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// NewSessionID returns "s_" followed by a ULID (time-ordered, 26 characters).
func NewSessionID(now time.Time) string {
	var b [16]byte
	binary.BigEndian.PutUint64(b[:8], uint64(now.UnixMilli())<<16) //nolint:gosec // milliseconds since 1970 fit 48 bits
	_, _ = rand.Read(b[6:])
	out := make([]byte, 26)
	// 128 bits → 26 base32 digits, most significant first.
	hi, lo := binary.BigEndian.Uint64(b[:8]), binary.BigEndian.Uint64(b[8:])
	for i := 25; i >= 0; i-- {
		out[i] = crockford[lo&31]
		lo = lo>>5 | (hi&31)<<59
		hi >>= 5
	}
	return "s_" + string(out)
}

// Mask is the replacement for a secret in output and messages.
const Mask = "••••"

// masker replaces secrets in a byte stream, also when a secret is split across
// writes: it holds back the longest suffix that could start a secret.
type masker struct {
	mu      sync.Mutex
	out     io.Writer
	secrets [][]byte
	pending []byte
}

func newMasker(out io.Writer, secrets []string) *masker {
	m := &masker{out: out}
	for _, s := range secrets {
		if s != "" {
			m.secrets = append(m.secrets, []byte(s))
		}
	}
	// Longer secrets first, so a secret containing another one is masked whole.
	sort.Slice(m.secrets, func(i, j int) bool { return len(m.secrets[i]) > len(m.secrets[j]) })
	return m
}

func (m *masker) Write(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	buf := append(m.pending, p...)
	buf = maskBytes(buf, m.secrets)
	keep := m.holdBack(buf)
	m.pending = append([]byte(nil), buf[len(buf)-keep:]...)
	if _, err := m.out.Write(buf[:len(buf)-keep]); err != nil {
		return 0, err
	}
	return len(p), nil
}

// holdBack returns how many trailing bytes could be the start of a secret.
func (m *masker) holdBack(buf []byte) int {
	keep := 0
	for _, s := range m.secrets {
		for n := min(len(s)-1, len(buf)); n > keep; n-- {
			if bytes.HasSuffix(buf, s[:n]) {
				keep = n
				break
			}
		}
	}
	return keep
}

// Flush writes what is held back.
func (m *masker) Flush() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.pending) == 0 {
		return nil
	}
	_, err := m.out.Write(m.pending)
	m.pending = nil
	return err
}

func maskBytes(b []byte, secrets [][]byte) []byte {
	for _, s := range secrets {
		b = bytes.ReplaceAll(b, s, []byte(Mask))
	}
	return b
}

// MaskString replaces every secret in s.
func MaskString(s string, secrets []string) string {
	bs := make([][]byte, 0, len(secrets))
	for _, x := range secrets {
		if x != "" {
			bs = append(bs, []byte(x))
		}
	}
	sort.Slice(bs, func(i, j int) bool { return len(bs[i]) > len(bs[j]) })
	return string(maskBytes([]byte(s), bs))
}

// Locks serialises sessions: two actions of the same plugin never run at the same
// time, and actions can name further locks (e.g. "system-packages").
type Locks struct {
	mu    sync.Mutex
	held  map[string]bool
	waitc chan struct{} // closed and replaced whenever a lock is released
}

// NewLocks returns an empty lock table.
func NewLocks() *Locks {
	return &Locks{held: map[string]bool{}, waitc: make(chan struct{})}
}

// Acquire takes all names at once (no partial holds, so no deadlocks). queued is
// called once if the caller has to wait. The returned function releases the locks.
func (l *Locks) Acquire(ctx context.Context, names []string, queued func()) (func(), error) {
	names = uniqueSorted(names)
	notified := false
	for {
		l.mu.Lock()
		free := true
		for _, n := range names {
			if l.held[n] {
				free = false
				break
			}
		}
		if free {
			for _, n := range names {
				l.held[n] = true
			}
			l.mu.Unlock()
			return func() { l.release(names) }, nil
		}
		wait := l.waitc
		l.mu.Unlock()
		if !notified && queued != nil {
			queued()
			notified = true
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-wait:
		}
	}
}

func (l *Locks) release(names []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, n := range names {
		delete(l.held, n)
	}
	close(l.waitc)
	l.waitc = make(chan struct{})
}

func uniqueSorted(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}
