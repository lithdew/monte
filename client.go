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

	mu    sync.Mutex
	conns []BufferedConn
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
