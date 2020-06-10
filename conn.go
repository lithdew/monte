package monte

import (
	"fmt"
	"io"
	"sync"
	"time"
)

type Conn struct {
	ReadBufferSize  int
	WriteBufferSize int

	ReadTimeout  time.Duration
	WriteTimeout time.Duration

	mu   sync.Mutex
	once sync.Once

	writerQueue []*pendingWrite
	writerCond  sync.Cond
	writerDone  bool

	reqs map[uint32]*pendingRequest
	seq  uint32
}

func (c *Conn) Write(buf []byte) error {
	c.once.Do(c.init)
	pw, err := c.preparePendingWrite(buf, true)
	if err != nil {
		return err
	}
	defer releasePendingWrite(pw)
	pw.wg.Wait()
	return pw.err
}

func (c *Conn) WriteNoWait(buf []byte) error {
	c.once.Do(c.init)
	_, err := c.preparePendingWrite(buf, false)
	return err
}

func (c *Conn) NumPendingWrites() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.writerQueue)
}

func (c *Conn) Handle(done chan struct{}, conn BufferedConn) error {
	c.once.Do(c.init)

	writerDone := make(chan error)
	go func() {
		writerDone <- c.writeLoop(conn)
	}()

	readerDone := make(chan error)
	go func() {
		readerDone <- c.readLoop(conn)
	}()

	var err error

	select {
	case <-done:
		c.closeWriter()
		err = <-writerDone
		conn.Close()
		if err == nil {
			err = <-readerDone
		} else {
			<-readerDone
		}
	case err = <-writerDone:
		conn.Close()
		if err == nil {
			err = <-readerDone
		} else {
			<-readerDone
		}
	case err = <-readerDone:
		c.closeWriter()
		if err == nil {
			err = <-writerDone
		} else {
			<-writerDone
		}
		conn.Close()
	}

	return err
}

func (c *Conn) init() {
	c.reqs = make(map[uint32]*pendingRequest)
	c.writerCond.L = &c.mu
}

func (c *Conn) preparePendingWrite(buf []byte, wait bool) (*pendingWrite, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.writerDone {
		return nil, fmt.Errorf("node is shut down: %w", io.EOF)
	}

	pw := acquirePendingWrite(buf, wait)
	if wait {
		pw.wg.Add(1)
	}

	c.writerQueue = append(c.writerQueue, pw)
	c.writerCond.Signal()

	return pw, nil
}

func (c *Conn) closeWriter() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writerDone = true
	c.writerCond.Signal()
}

func (c *Conn) getReadBufferSize() int {
	if c.ReadBufferSize <= 0 {
		return DefaultReadBufferSize
	}
	return c.ReadBufferSize
}

func (c *Conn) getWriteBufferSize() int {
	if c.WriteBufferSize <= 0 {
		return DefaultWriteBufferSize
	}
	return c.WriteBufferSize
}

func (c *Conn) getReadTimeout() time.Duration {
	if c.ReadTimeout <= 0 {
		return DefaultReadTimeout
	}
	return c.ReadTimeout
}

func (c *Conn) getWriteTimeout() time.Duration {
	if c.WriteTimeout <= 0 {
		return DefaultWriteTimeout
	}
	return c.WriteTimeout
}

func (c *Conn) next() uint32 {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.seq++
	if c.seq == 0 {
		c.seq = 1
	}
	return c.seq
}

func (c *Conn) writeLoop(conn BufferedConn) error {
	var err error

	for {
		c.mu.Lock()
		for !c.writerDone && len(c.writerQueue) == 0 {
			c.writerCond.Wait()
		}
		done, queue := c.writerDone, c.writerQueue
		c.writerQueue = nil
		c.mu.Unlock()

		if done && len(queue) == 0 {
			break
		}

		for _, pw := range queue {
			if err == nil {
				_, err = conn.Write(pw.buf)
			}
			if pw.wait {
				pw.err = err
				pw.wg.Done()
			} else {
				releasePendingWrite(pw)
			}
		}

		if err != nil {
			break
		}

		err = conn.Flush()
		if err != nil {
			break
		}
	}

	return err
}

func (c *Conn) readLoop(conn BufferedConn) error {
	var (
		buf = make([]byte, c.getReadBufferSize())
		n   int
		err error
	)

	for {
		n, err = conn.Read(buf)
		if err != nil {
			break
		}

		buf = buf[:n]

		// TODO
	}

	return err
}

func (c *Conn) close(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, pw := range c.writerQueue {
		if pw.wait {
			pw.err = err
			pw.wg.Done()
		} else {
			releasePendingWrite(pw)
		}
	}
	c.writerQueue = nil
	c.writerDone = false
	for seq := range c.reqs {
		delete(c.reqs, seq)
	}
	c.seq = 0
}

type pendingWrite struct {
	buf  []byte
	wait bool
	err  error
	wg   sync.WaitGroup
}

var pendingWritePool sync.Pool

func acquirePendingWrite(buf []byte, wait bool) *pendingWrite {
	v := pendingWritePool.Get()
	if v == nil {
		v = &pendingWrite{}
	}
	wi := v.(*pendingWrite)
	wi.buf = buf
	wi.wait = wait
	return wi
}

func releasePendingWrite(wi *pendingWrite) {
	pendingWritePool.Put(wi)
}

type pendingRequest struct {
	dst []byte
	wg  sync.WaitGroup
}

var pendingRequestPool sync.Pool

func acquirePendingRequest(dst []byte) *pendingRequest {
	v := pendingRequestPool.Get()
	if v == nil {
		v = &pendingRequest{}
	}
	wi := v.(*pendingRequest)
	wi.dst = dst
	return wi
}

func releasePendingRequest(wi *pendingRequest) {
	pendingRequestPool.Put(wi)
}
