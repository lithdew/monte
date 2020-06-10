package monte

import "net"

type Handler interface {
	Handle(done chan struct{}, conn BufferedConn) error
}

type HandlerFunc func(done chan struct{}, conn BufferedConn) error

func (h HandlerFunc) Handle(done chan struct{}, conn BufferedConn) error {
	return h(done, conn)
}

var DefaultHandler HandlerFunc = func(_ chan struct{}, conn BufferedConn) error { return nil }

type Handshaker interface {
	Handshake(conn net.Conn) (BufferedConn, error)
}

type HandshakerFunc func(conn net.Conn) (BufferedConn, error)

func (h HandshakerFunc) Handshake(conn net.Conn) (BufferedConn, error) {
	return h(conn)
}

var DefaultClientHandshaker HandshakerFunc = func(conn net.Conn) (BufferedConn, error) {
	session, err := NewSession()
	if err != nil {
		return nil, err
	}
	err = session.DoClient(conn)
	if err != nil {
		return nil, err
	}
	return NewSessionConn(session.Suite(), conn), nil
}

var DefaultServerHandshaker HandshakerFunc = func(conn net.Conn) (BufferedConn, error) {
	session, err := NewSession()
	if err != nil {
		return nil, err
	}
	err = session.DoServer(conn)
	if err != nil {
		return nil, err
	}
	return NewSessionConn(session.Suite(), conn), nil
}
