package monte

import (
	"fmt"
	"github.com/lithdew/bytesutil"
	"github.com/valyala/bytebufferpool"
	"io"
	"sync"
	"time"
)

type Conn struct {
	Handler Handler

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

func (c *Conn) write(buf []byte) error {
	c.once.Do(c.init)
	pw, err := c.preparePendingWrite(buf, true)
	if err != nil {
		return err
	}
	defer releasePendingWrite(pw)
	pw.wg.Wait()
	return pw.err
}

func (c *Conn) writeNoWait(buf []byte) error {
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
		close(writerDone)
	}()

	readerDone := make(chan error)
	go func() {
		readerDone <- c.readLoop(conn)
		close(readerDone)
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
		c.closeWriter()
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

func (c *Conn) Send(payload []byte) error       { c.once.Do(c.init); return c.send(0, payload) }
func (c *Conn) SendNoWait(payload []byte) error { c.once.Do(c.init); return c.sendNoWait(0, payload) }

func (c *Conn) Request(dst []byte, payload []byte) ([]byte, error) {
	c.once.Do(c.init)

	buf := bytebufferpool.Get()
	defer bytebufferpool.Put(buf)

	seq := c.next()

	buf.B = bytesutil.AppendUint32BE(buf.B, seq)
	buf.B = append(buf.B, payload...)

	pr := acquirePendingRequest(dst)
	defer releasePendingRequest(pr)

	pr.wg.Add(1)

	c.mu.Lock()
	c.reqs[seq] = pr
	c.mu.Unlock()

	err := c.writeNoWait(buf.B)

	if err != nil {
		pr.wg.Done()

		c.mu.Lock()
		delete(c.reqs, seq)
		c.mu.Unlock()
		return nil, err
	}

	pr.wg.Wait()

	return pr.dst, nil
}

func (c *Conn) init() {
	c.reqs = make(map[uint32]*pendingRequest)
	c.writerCond.L = &c.mu
}

func (c *Conn) send(seq uint32, payload []byte) error {
	buf := bytebufferpool.Get()
	defer bytebufferpool.Put(buf)

	buf.B = bytesutil.AppendUint32BE(buf.B, seq)
	buf.B = append(buf.B, payload...)

	return c.write(buf.B)
}

func (c *Conn) sendNoWait(seq uint32, payload []byte) error {
	buf := bytebufferpool.Get()
	defer bytebufferpool.Put(buf)

	buf.B = bytesutil.AppendUint32BE(buf.B, seq)
	buf.B = append(buf.B, payload...)

	return c.writeNoWait(buf.B)
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

func (c *Conn) getHandler() Handler {
	if c.Handler == nil {
		return DefaultHandler
	}
	return c.Handler
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
	if c.ReadTimeout < 0 {
		return DefaultReadTimeout
	}
	return c.ReadTimeout
}

func (c *Conn) getWriteTimeout() time.Duration {
	if c.WriteTimeout < 0 {
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

		timeout := c.getWriteTimeout()
		if timeout > 0 {
			err = conn.SetWriteDeadline(time.Now().Add(timeout))
			if err != nil {
				for _, pw := range queue {
					if pw.wait {
						pw.err = err
						pw.wg.Done()
					} else {
						releasePendingWrite(pw)
					}
				}
				break
			}
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
	buf := make([]byte, c.getReadBufferSize())

	for {
		timeout := c.getReadTimeout()
		if timeout > 0 {
			err := conn.SetReadDeadline(time.Now().Add(timeout))
			if err != nil {
				return err
			}
		}

		n, err := conn.Read(buf)
		if err != nil {
			return err
		}

		data := buf[:n]
		if len(data) < 4 {
			return fmt.Errorf("no sequence number to decode: %w", io.ErrUnexpectedEOF)
		}

		seq := bytesutil.Uint32BE(data)
		data = data[4:]

		c.mu.Lock()
		pr, exists := c.reqs[seq]
		if exists {
			delete(c.reqs, seq)
		}
		c.mu.Unlock()

		if seq == 0 || !exists {
			err := c.call(seq, data)
			if err != nil {
				return fmt.Errorf("handler encountered an error: %w", err)
			}
			continue
		}

		// received response

		pr.dst = bytesutil.ExtendSlice(pr.dst, len(data))
		copy(pr.dst, data)

		pr.wg.Done()
	}
}

func (c *Conn) call(seq uint32, data []byte) error {
	ctx := acquireContext(c, seq, data)
	defer releaseContext(ctx)
	return c.getHandler().HandleMessage(ctx)
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

	for seq := range c.reqs {
		pr := c.reqs[seq]
		pr.wg.Done()

		delete(c.reqs, seq)
		releasePendingRequest(pr)
	}

	c.seq = 0
}
