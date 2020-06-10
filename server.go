package monte

import (
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

var DefaultMaxServerConns = 1024

var DefaultHandshakeTimeout = 3 * time.Second
var DefaultMaxConnWaitTimeout = 3 * time.Second

type Server struct {
	Handler Handler

	Handshaker       Handshaker
	HandshakeTimeout time.Duration

	MaxConns           int
	MaxConnWaitTimeout time.Duration

	ReadBufferSize  int
	WriteBufferSize int

	ReadTimeout  time.Duration
	WriteTimeout time.Duration

	once sync.Once
	mu   sync.Mutex
	wg   sync.WaitGroup

	sem  chan struct{}
	done chan struct{}
}

func (s *Server) init() {
	s.sem = make(chan struct{}, s.getMaxConns())
	s.done = make(chan struct{})
}

func (s *Server) getHandler() Handler {
	if s.Handler == nil {
		return DefaultHandler
	}
	return s.Handler
}

func (s *Server) getHandshaker() Handshaker {
	if s.Handshaker == nil {
		return DefaultServerHandshaker
	}
	return s.Handshaker
}

func (s *Server) getHandshakeTimeout() time.Duration {
	if s.HandshakeTimeout < 0 {
		return DefaultHandshakeTimeout
	}
	return s.HandshakeTimeout
}

func (s *Server) getMaxConns() int {
	if s.MaxConns <= 0 {
		return DefaultMaxServerConns
	}
	return s.MaxConns
}

func (s *Server) getMaxConnWaitTimeout() time.Duration {
	if s.MaxConnWaitTimeout <= 0 {
		return DefaultMaxConnWaitTimeout
	}
	return s.MaxConnWaitTimeout
}

func (s *Server) getReadTimeout() time.Duration {
	if s.ReadTimeout < 0 {
		return DefaultReadTimeout
	}
	return s.ReadTimeout
}

func (s *Server) getWriteTimeout() time.Duration {
	if s.WriteTimeout < 0 {
		return DefaultWriteTimeout
	}
	return s.WriteTimeout
}

func (s *Server) getReadBufferSize() int {
	if s.ReadBufferSize <= 0 {
		return DefaultReadBufferSize
	}
	return s.ReadBufferSize
}

func (s *Server) getWriteBufferSize() int {
	if s.WriteBufferSize <= 0 {
		return DefaultWriteBufferSize
	}
	return s.WriteBufferSize
}

func (s *Server) serverAvailable() bool {
	select {
	case <-s.done:
		return false
	case s.sem <- struct{}{}:
		return true
	default:
		timer := AcquireTimer(s.getMaxConnWaitTimeout())
		defer ReleaseTimer(timer)

		select {
		case <-timer.C:
			return false
		case <-s.done:
			return false
		case s.sem <- struct{}{}:
			return true
		}
	}
}

func (s *Server) wait(duration time.Duration) bool {
	timer := AcquireTimer(duration)
	defer ReleaseTimer(timer)

	select {
	case <-timer.C:
		return true
	case <-s.done:
		return false
	}
}

func (s *Server) client(conn net.Conn) error {
	defer func() { <-s.sem }()

	timeout := s.getHandshakeTimeout()

	if timeout != 0 {
		err := conn.SetDeadline(time.Now().Add(timeout))
		if err != nil {
			return err
		}
	}

	bufConn, err := s.getHandshaker().Handshake(conn)
	if err != nil {
		return err
	}

	if timeout != 0 {
		err = conn.SetDeadline(zeroTime)
		if err != nil {
			return err
		}
	}

	cc := &Conn{
		Handler:         s.getHandler(),
		ReadBufferSize:  s.getReadBufferSize(),
		WriteBufferSize: s.getWriteBufferSize(),
		ReadTimeout:     s.getReadTimeout(),
		WriteTimeout:    s.getWriteTimeout(),
	}

	cc.close(cc.Handle(s.done, bufConn))

	return nil
}

func (s *Server) Serve(ln net.Listener) error {
	s.once.Do(s.init)

	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			var netErr *net.OpError
			if !errors.As(err, &netErr) {
				return err
			}
			if netErr.Err.Error() == "use of closed network connection" {
				return nil
			}
			if !netErr.Temporary() {
				return err
			}
			ok := s.wait(100 * time.Millisecond)
			if !ok {
				return nil
			}
			continue
		}

		if !s.serverAvailable() {
			conn.Close()
			continue
		}

		s.wg.Add(1)

		go func() {
			defer s.wg.Done()
			s.client(conn)
			conn.Close()
		}()
	}
}

func (s *Server) Shutdown() {
	s.once.Do(s.init)

	close(s.done)
	s.wg.Wait()
}
