package relay

import (
	"net"
	"sync"
)

// limitedListener caps concurrent accepted connections (0142 F19).
// Accept blocks when the semaphore is full; it does not RST the peer.
type limitedListener struct {
	net.Listener
	sem chan struct{}
}

func limitListener(ln net.Listener, n int) net.Listener {
	if n <= 0 {
		return ln
	}
	return &limitedListener{Listener: ln, sem: make(chan struct{}, n)}
}

func (l *limitedListener) Accept() (net.Conn, error) {
	l.sem <- struct{}{}
	c, err := l.Listener.Accept()
	if err != nil {
		<-l.sem
		return nil, err
	}
	return &limitedConn{Conn: c, release: l.sem}, nil
}

type limitedConn struct {
	net.Conn
	release chan struct{}
	once    sync.Once
}

func (c *limitedConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(func() { <-c.release })
	return err
}
