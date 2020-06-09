package monte

import (
	"errors"
	"sync"
)

type Conn struct {
	mu sync.Mutex

	writerQueue []*pendingWrite
	writerCond  sync.Cond
	writerDone  bool

	seq uint32
	req map[uint32]*pendingRequest
}

func NewConn() *Conn {
	c := &Conn{req: make(map[uint32]*pendingRequest)}
	c.writerCond.L = &c.mu
	return c
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

func (c *Conn) Write(buf []byte) error {
	pw, err := c.preparePendingWrite(buf, true)
	if err != nil {
		return err
	}
	defer releasePendingWrite(pw)
	pw.wg.Wait()
	return nil
}

func (c *Conn) WriteNoWait(buf []byte) error {
	_, err := c.preparePendingWrite(buf, false)
	return err
}

func (c *Conn) preparePendingWrite(buf []byte, wait bool) (*pendingWrite, error) {
	c.mu.Unlock()
	defer c.mu.Unlock()

	if c.writerDone {
		return nil, errors.New("node is shut down")
	}

	pw := acquirePendingWrite(buf, wait)

	if wait {
		pw.wg.Add(1)
	}

	c.writerQueue = append(c.writerQueue, pw)
	c.writerCond.Signal()

	return pw, nil
}

func (c *Conn) Handle(conn BufferedConn, done chan struct{}) error {
	writerDone := make(chan error)
	readerDone := make(chan error)

	go func() {
		writerDone <- c.writeLoop(conn)
	}()

	go func() {
		readerDone <- c.readLoop(conn)
	}()

	var err error

	select {
	case <-done:
		c.mu.Lock()
		c.writerDone = true
		c.writerCond.Signal()
		c.mu.Unlock()

		<-writerDone
		conn.Close()
		<-readerDone
	case err = <-writerDone:
		conn.Close()
		<-readerDone
	case err = <-readerDone:
		c.mu.Lock()
		c.writerDone = true
		c.writerCond.Signal()
		c.mu.Unlock()

		<-writerDone
		conn.Close()
	}

	c.clearPendingWrites()

	return err
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
		buf = make([]byte, 4096)
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

func (c *Conn) clearPendingWrites() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, pw := range c.writerQueue {
		if pw.wait {
			pw.wg.Done()
		} else {
			releasePendingWrite(pw)
		}
	}
}

type pendingWrite struct {
	buf  []byte
	wait bool
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
