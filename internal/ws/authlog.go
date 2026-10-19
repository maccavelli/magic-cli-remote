package ws

import (
	"log/slog"
	"net"
	"sync"
	"time"
)

// authLogWindow is how often one (code, remote host) pair may log a rejection.
const authLogWindow = time.Minute

// authLogMaxEntries bounds the limiter's memory; the oldest window goes first.
const authLogMaxEntries = 1024

// authLogLimiter throttles the auth-rejection log per (code, remote host), so a
// scanner hammering the port cannot flood the log, while a phone that cannot
// log in still leaves a trace (MADR 0201 D1, 0073 F3(b)).
type authLogLimiter struct {
	mu      sync.Mutex
	entries map[string]*authLogEntry
}

type authLogEntry struct {
	start      time.Time
	suppressed int
}

// allow reports whether a rejection for key may be logged at now, and how many
// were suppressed in the window it closes.
func (l *authLogLimiter) allow(key string, now time.Time) (bool, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.entries == nil {
		l.entries = make(map[string]*authLogEntry)
	}
	e, ok := l.entries[key]
	if ok && now.Sub(e.start) < authLogWindow {
		e.suppressed++
		return false, 0
	}
	suppressed := 0
	if ok {
		suppressed = e.suppressed
	}
	if !ok && len(l.entries) >= authLogMaxEntries {
		var oldestKey string
		var oldest time.Time
		for k, v := range l.entries {
			if oldestKey == "" || v.start.Before(oldest) {
				oldestKey, oldest = k, v.start
			}
		}
		delete(l.entries, oldestKey)
	}
	l.entries[key] = &authLogEntry{start: now}
	return true, suppressed
}

// remoteHost strips the port from a remote address; an address without one is
// returned as it is.
func remoteHost(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

// logAuthRejection records one rejected authentication at warn: its code, the
// remote host and, when the token was valid, the device. No token, and no part
// of one, is ever passed here.
func (s *Server) logAuthRejection(code, remoteAddr, deviceID string) {
	host := remoteHost(remoteAddr)
	now := time.Now
	if s.now != nil {
		now = s.now
	}
	ok, suppressed := s.authLog.allow(code+"|"+host, now())
	if !ok {
		return
	}
	attrs := []any{slog.String("code", code), slog.String("remote_addr", host)}
	if deviceID != "" {
		attrs = append(attrs, slog.String("device_id", deviceID))
	}
	if suppressed > 0 {
		attrs = append(attrs, slog.Int("suppressed", suppressed))
	}
	s.log.Warn("authentication rejected", attrs...)
}
