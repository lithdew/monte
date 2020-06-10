package monte

import (
	"net"
	"sync"
	"time"
)

type clientConn struct {
	Addr string

	Handler Handler

	Handshaker       Handshaker
	HandshakeTimeout time.Duration

	NumDialAttempts int

	conn   *Conn // the underlying connection of this client instance
	dialed bool  // true if peer is dialed and handshaked, and protected by mu

	mu   sync.Mutex    // makes (dial and write) and (clear writes and reset conn) mutually exclusive
	done chan struct{} // signals if the client is shutting down
}

func (c *clientConn) Write(buf []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	err := c.dial()
	if err == nil {
		err = c.conn.Write(buf)
	}
	return err
}

func (c *clientConn) WriteNoWait(buf []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	err := c.dial()
	if err == nil {
		err = c.conn.WriteNoWait(buf)
	}
	return err
}

func (c *clientConn) NumPendingWrites() int {
	return c.conn.NumPendingWrites()
}

func (c *clientConn) dial() error {
	if c.dialed {
		return nil
	}

	var err error
	var conn net.Conn
	var bufConn BufferedConn

	d := net.Dialer{Timeout: 3 * time.Second}

	for i := 0; i < c.getNumDialAttempts(); i++ {
		conn, err = d.Dial("tcp", c.Addr)
		if err == nil {
			err = conn.SetDeadline(time.Now().Add(c.getHandshakeTimeout()))
		}
		if err == nil {
			bufConn, err = c.getHandshaker().Handshake(conn)
		}
		if err == nil {
			err = conn.SetDeadline(zeroTime)
		}
		if err == nil {
			break
		}
	}

	if err != nil {
		return err
	}

	go func() {
		err := c.conn.Handle(c.done, bufConn)

		c.mu.Lock()
		c.conn.close(err)
		c.dialed = false
		c.mu.Unlock()
	}()

	c.dialed = true

	return nil
}

func (c *clientConn) getHandler() Handler {
	if c.Handler == nil {
		return DefaultHandler
	}
	return c.Handler
}

func (c *clientConn) getHandshaker() Handshaker {
	if c.Handshaker == nil {
		return DefaultClientHandshaker
	}
	return c.Handshaker
}

func (c *clientConn) getHandshakeTimeout() time.Duration {
	if c.HandshakeTimeout <= 0 {
		return DefaultHandshakeTimeout
	}
	return c.HandshakeTimeout
}

func (c *clientConn) getNumDialAttempts() int {
	if c.NumDialAttempts <= 0 {
		return DefaultNumDialAttempts
	}
	return c.NumDialAttempts
}
