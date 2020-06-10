package monte

import (
	"sync"
	"time"
)

var DefaultMaxClientConns = 4
var DefaultNumDialAttempts = 1
var DefaultReadBufferSize = 4096
var DefaultWriteBufferSize = 4096
var DefaultReadTimeout = 3 * time.Second
var DefaultWriteTimeout = 3 * time.Second

// handles dialing and managing multiple clients

type Client struct {
	Addr string

	Handler Handler

	Handshaker       Handshaker
	HandshakeTimeout time.Duration

	MaxConns        int
	NumDialAttempts int

	ReadBufferSize  int
	WriteBufferSize int

	ReadTimeout  time.Duration
	WriteTimeout time.Duration

	once     sync.Once
	shutdown sync.Once

	done chan struct{}

	mu    sync.Mutex
	conns []*clientConn
}

func (c *Client) Write(buf []byte) error {
	c.once.Do(c.init)
	return c.getConn().Write(buf)
}

func (c *Client) WriteNoWait(buf []byte) error {
	c.once.Do(c.init)
	return c.getConn().WriteNoWait(buf)
}

func (c *Client) Shutdown() {
	c.once.Do(c.init)

	shutdown := func() {
		close(c.done)
	}
	c.shutdown.Do(shutdown)
}

func (c *Client) init() {
	c.done = make(chan struct{})
}

func (c *Client) newConn() *clientConn {
	cc := &clientConn{
		Addr:             c.Addr,
		Handler:          c.getHandler(),
		Handshaker:       c.getHandshaker(),
		HandshakeTimeout: c.getHandshakeTimeout(),

		done: c.done,

		conn: &Conn{
			ReadBufferSize:  c.getReadBufferSize(),
			WriteBufferSize: c.getWriteBufferSize(),
			ReadTimeout:     c.getReadTimeout(),
			WriteTimeout:    c.getWriteTimeout(),
		},
	}
	c.conns = append(c.conns, cc)
	return cc
}

func (c *Client) getConn() *clientConn {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.conns) == 0 {
		return c.newConn()
	}

	mc := c.conns[0]
	mp := mc.NumPendingWrites()
	if mp == 0 {
		return mc
	}
	for i := 1; i < len(c.conns); i++ {
		cc := c.conns[i]
		cp := cc.NumPendingWrites()
		if cp == 0 {
			return cc
		}
		if cp < mp {
			mc, mp = cc, cp
		}
	}
	if len(c.conns) < c.getMaxConns() {
		return c.newConn()
	}
	return mc
}

func (c *Client) getHandler() Handler {
	if c.Handler == nil {
		return DefaultHandler
	}
	return c.Handler
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

func (c *Client) getReadTimeout() time.Duration {
	if c.ReadTimeout <= 0 {
		return DefaultReadTimeout
	}
	return c.ReadTimeout
}

func (c *Client) getWriteTimeout() time.Duration {
	if c.WriteTimeout <= 0 {
		return DefaultWriteTimeout
	}
	return c.WriteTimeout
}
