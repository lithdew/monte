package monte

import (
	"sync"
	"time"
)

var DefaultMaxClientConns = 4
var DefaultReadBufferSize = 4096
var DefaultWriteBufferSize = 4096
var DefaultReadTimeout = 3 * time.Second
var DefaultWriteTimeout = 3 * time.Second

// handles dialing and managing multiple clients

type Client struct {
	Addr string

	Handler    Handler
	Handshaker Handshaker

	MaxConns int

	ReadBufferSize  int
	WriteBufferSize int

	ReadTimeout  time.Duration
	WriteTimeout time.Duration

	once     sync.Once
	shutdown sync.Once

	done chan struct{}

	mu    sync.Mutex
	conns []*Conn
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

func (c *Client) getConn() *Conn {
	c.mu.Lock()
	defer c.mu.Unlock()

	// TODO(kenta): multiple conns

	if len(c.conns) == 0 {
		return c.createConn()
	}
	return c.conns[0]
}

func (c *Client) createConn() *Conn {
	cc := &Conn{
		Addr:            c.Addr,
		Handler:         c.getHandler(),
		Handshaker:      c.getHandshaker(),
		MaxConns:        c.getMaxConns(),
		ReadBufferSize:  c.getReadBufferSize(),
		WriteBufferSize: c.getWriteBufferSize(),
		ReadTimeout:     c.getReadTimeout(),
		WriteTimeout:    c.getWriteTimeout(),
	}

	go func() {
		// TODO(kenta): dial logic

		err := cc.Handle(nil, c.done)
		_ = err
	}()

	c.conns = append(c.conns, cc)
	return cc
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
