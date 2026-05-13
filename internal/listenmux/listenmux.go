// Package listenmux multiplexes plain HTTP and TLS connections on a
// single TCP listener. It peeks the first byte of every accepted
// connection: TLS handshake records start with 0x16, so anything else
// (typically an ASCII HTTP method byte) is plain HTTP.
//
// The two virtual listeners returned from Split can be handed to
// http.Server instances normally — net/http never knows the difference.
package listenmux

import (
	"bufio"
	"io"
	"net"
	"sync"
	"time"
)

// peekTimeout caps how long we wait for the first byte. Slow-loris
// connections that never send anything would otherwise sit forever in
// the route() goroutine.
const peekTimeout = 5 * time.Second

// Split returns two virtual listeners — tls for connections starting
// with a TLS handshake byte, plain for everything else — and a Run
// function that drives the accept loop. Run blocks until ln returns an
// error; the caller usually invokes it in a goroutine.
func Split(ln net.Listener) (tlsL net.Listener, plainL net.Listener, run func()) {
	tlsLn := newVirt(ln.Addr())
	plainLn := newVirt(ln.Addr())
	run = func() {
		defer tlsLn.shutdown()
		defer plainLn.shutdown()
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go route(conn, tlsLn, plainLn)
		}
	}
	return tlsLn, plainLn, run
}

func route(conn net.Conn, tlsLn, plainLn *virtListener) {
	_ = conn.SetReadDeadline(time.Now().Add(peekTimeout))
	br := bufio.NewReader(conn)
	first, err := br.Peek(1)
	// Clear the deadline so the protocol handler gets the full timeout
	// budget of its http.Server.
	_ = conn.SetReadDeadline(time.Time{})
	if err != nil || len(first) == 0 {
		conn.Close()
		return
	}
	wrapped := &peekedConn{Conn: conn, r: br}
	target := plainLn
	if first[0] == 0x16 {
		target = tlsLn
	}
	if !target.push(wrapped) {
		conn.Close()
	}
}

// peekedConn replaces the underlying conn's Read with one that drains
// the bufio buffer first, so the peeked byte is still delivered to the
// protocol handler.
type peekedConn struct {
	net.Conn
	r io.Reader
}

func (p *peekedConn) Read(b []byte) (int, error) { return p.r.Read(b) }

// --- virtual listener -----------------------------------------------------

type virtListener struct {
	addr   net.Addr
	conns  chan net.Conn
	done   chan struct{}
	once   sync.Once
}

func newVirt(addr net.Addr) *virtListener {
	return &virtListener{
		addr:  addr,
		conns: make(chan net.Conn, 64),
		done:  make(chan struct{}),
	}
}

func (v *virtListener) push(c net.Conn) bool {
	select {
	case v.conns <- c:
		return true
	case <-v.done:
		return false
	}
}

func (v *virtListener) Accept() (net.Conn, error) {
	select {
	case c := <-v.conns:
		return c, nil
	case <-v.done:
		return nil, net.ErrClosed
	}
}

func (v *virtListener) Close() error {
	v.shutdown()
	return nil
}

func (v *virtListener) Addr() net.Addr { return v.addr }

func (v *virtListener) shutdown() {
	v.once.Do(func() {
		close(v.done)
		// Drain anything still queued.
		for {
			select {
			case c := <-v.conns:
				c.Close()
			default:
				return
			}
		}
	})
}
