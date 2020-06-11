package monte

import (
	"net"
	"sync"
	"time"
)

var DefaultMaxClientConns = 4
var DefaultNumDialAttempts = 1
var DefaultReadBufferSize = 4096
var DefaultWriteBufferSize = 4096
var DefaultDialTimeout = 3 * time.Second
var DefaultReadTimeout = 3 * time.Second
var DefaultWriteTimeout = 3 * time.Second

type clientConn struct {
	conn  *Conn
	ready chan struct{}
	err   error
}

type Client struct {
	Addr string

	Handler   Handler
	ConnState ConnStateHandler

	Handshaker       Handshaker
	HandshakeTimeout time.Duration

	MaxConns        int
	NumDialAttempts int

	ReadBufferSize  int
	WriteBufferSize int

	DialTimeout  time.Duration
	ReadTimeout  time.Duration
	WriteTimeout time.Duration

	once     sync.Once
	shutdown sync.Once

	done chan struct{}

	mu    sync.Mutex
	conns []*clientConn
}

func (c *Client) Send(buf []byte) error {
	c.once.Do(c.init)

	cc := c.getClientConn()

	<-cc.ready
	if cc.err != nil {
		return cc.err
	}

	return cc.conn.Send(buf)
}

func (c *Client) SendNoWait(buf []byte) error {
	c.once.Do(c.init)

	cc := c.getClientConn()

	<-cc.ready
	if cc.err != nil {
		return cc.err
	}

	return cc.conn.SendNoWait(buf)
}

func (c *Client) Request(dst, buf []byte) ([]byte, error) {
	c.once.Do(c.init)

	cc := c.getClientConn()

	<-cc.ready
	if cc.err != nil {
		return nil, cc.err
	}

	return cc.conn.Request(dst, buf)
}

func (c *Client) NumPendingWrites() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	n := 0
	for _, cc := range c.conns {
		n += cc.conn.NumPendingWrites()
	}
	return n
}

func (c *Client) Shutdown() {
	c.once.Do(c.init)

	c.shutdown.Do(func() {
		close(c.done)
	})
}

func (c *Client) init() {
	c.done = make(chan struct{})
}

func (c *Client) deleteClientConn(conn *clientConn) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entries := c.conns[:]

	c.conns = c.conns[:0]
	for i := 0; i < len(entries); i++ {
		if entries[i] == conn {
			continue
		}
		c.conns = append(c.conns, entries[i])
	}
}

func (c *Client) newClientConn() *clientConn {
	cc := &clientConn{
		ready: make(chan struct{}),
		conn: &Conn{
			Handler:         c.getHandler(),
			ReadBufferSize:  c.getReadBufferSize(),
			WriteBufferSize: c.getWriteBufferSize(),
			ReadTimeout:     c.getReadTimeout(),
			WriteTimeout:    c.getWriteTimeout(),
		},
	}
	c.conns = append(c.conns, cc)

	go func() {
		defer c.deleteClientConn(cc)

		dialer := net.Dialer{Timeout: c.getDialTimeout()}

		var (
			conn    net.Conn
			bufConn BufferedConn
		)

		for i := 0; i < c.getNumDialAttempts(); i++ {
			conn, cc.err = dialer.Dial("tcp", c.Addr)
			if cc.err == nil {
				cc.err = conn.SetDeadline(time.Now().Add(c.getHandshakeTimeout()))
			}
			if cc.err == nil {
				bufConn, cc.err = c.getHandshaker().Handshake(conn)
			}
			if cc.err == nil {
				cc.err = conn.SetDeadline(zeroTime)
			}
			if cc.err == nil {
				break
			}
		}

		if cc.err != nil {
			if conn != nil {
				conn.Close()
			}
			close(cc.ready)
			return
		}

		close(cc.ready)

		c.getConnStateHandler().HandleConnState(bufConn, StateNew)

		cc.conn.close(cc.conn.Handle(c.done, bufConn))

		c.getConnStateHandler().HandleConnState(bufConn, StateClosed)
	}()

	return cc
}

func (c *Client) getClientConn() *clientConn {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.conns) == 0 {
		return c.newClientConn()
	}

	mc := c.conns[0]
	mp := mc.conn.NumPendingWrites()
	if mp == 0 {
		return mc
	}
	for i := 1; i < len(c.conns); i++ {
		cc := c.conns[i]
		cp := cc.conn.NumPendingWrites()
		if cp == 0 {
			return cc
		}
		if cp < mp {
			mc, mp = cc, cp
		}
	}
	if len(c.conns) < c.getMaxConns() {
		return c.newClientConn()
	}
	return mc
}

func (c *Client) getHandler() Handler {
	if c.Handler == nil {
		return DefaultHandler
	}
	return c.Handler
}

func (c *Client) getConnStateHandler() ConnStateHandler {
	if c.ConnState == nil {
		return DefaultConnStateHandler
	}
	return c.ConnState
}

func (c *Client) getHandshaker() Handshaker {
	if c.Handshaker == nil {
		return DefaultClientHandshaker
	}
	return c.Handshaker
}

func (c *Client) getMaxConns() int {
	if c.MaxConns <= 0 {
		return DefaultMaxClientConns
	}
	return c.MaxConns
}

func (c *Client) getNumDialAttempts() int {
	if c.NumDialAttempts <= 0 {
		return DefaultNumDialAttempts
	}
	return c.NumDialAttempts
}

func (c *Client) getReadBufferSize() int {
	if c.ReadBufferSize <= 0 {
		return DefaultReadBufferSize
	}
	return c.ReadBufferSize
}

func (c *Client) getWriteBufferSize() int {
	if c.WriteBufferSize <= 0 {
		return DefaultWriteBufferSize
	}
	return c.WriteBufferSize
}

func (c *Client) getHandshakeTimeout() time.Duration {
	if c.HandshakeTimeout <= 0 {
		return DefaultHandshakeTimeout
	}
	return c.HandshakeTimeout
}

func (c *Client) getDialTimeout() time.Duration {
	if c.DialTimeout <= 0 {
		return DefaultDialTimeout
	}
	return c.DialTimeout
}

func (c *Client) getReadTimeout() time.Duration {
	if c.ReadTimeout < 0 {
		return DefaultReadTimeout
	}
	return c.ReadTimeout
}

func (c *Client) getWriteTimeout() time.Duration {
	if c.WriteTimeout < 0 {
		return DefaultWriteTimeout
	}
	return c.WriteTimeout
}
