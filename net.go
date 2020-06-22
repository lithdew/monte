package monte

import "net"

type ConnState int

const (
	StateNew ConnState = iota
	StateClosed
)

type ConnStateHandler interface {
	HandleConnState(conn *Conn, state ConnState)
}

type ConnStateHandlerFunc func(conn *Conn, state ConnState)

func (fn ConnStateHandlerFunc) HandleConnState(conn *Conn, state ConnState) { fn(conn, state) }

var DefaultConnStateHandler ConnStateHandlerFunc = func(conn *Conn, state ConnState) {}

type Handler interface {
	HandleMessage(ctx *Context) error
}

type HandlerFunc func(ctx *Context) error

func (fn HandlerFunc) HandleMessage(ctx *Context) error { return fn(ctx) }

var DefaultHandler HandlerFunc = func(ctx *Context) error { return nil }

type Handshaker interface {
	Handshake(conn net.Conn) (BufferedConn, error)
}

type HandshakerFunc func(conn net.Conn) (BufferedConn, error)

func (fn HandshakerFunc) Handshake(conn net.Conn) (BufferedConn, error) { return fn(conn) }

var DefaultClientHandshaker HandshakerFunc = func(conn net.Conn) (BufferedConn, error) {
	var session Session
	err := session.DoClient(conn)
	if err != nil {
		return nil, err
	}
	return NewSessionConn(session.Suite(), conn), nil
}

var DefaultServerHandshaker HandshakerFunc = func(conn net.Conn) (BufferedConn, error) {
	var session Session
	err := session.DoServer(conn)
	if err != nil {
		return nil, err
	}
	return NewSessionConn(session.Suite(), conn), nil
}
