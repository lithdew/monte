package monte

import (
	"encoding/binary"
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

	pr := acquirePendingRequest(dst)
	defer releasePendingRequest(pr)

	pr.wg.Add(1)

	seq := c.next()

	c.mu.Lock()
	c.reqs[seq] = pr
	c.mu.Unlock()

	err := c.sendNoWait(seq, payload)

	if err != nil {
		pr.wg.Done()

		c.mu.Lock()
		delete(c.reqs, seq)
		c.mu.Unlock()
		return nil, err
	}

	pr.wg.Wait()

	return pr.dst, pr.err
}

func (c *Conn) init() {
	c.reqs = make(map[uint32]*pendingRequest)
	c.writerCond.L = &c.mu
}

func (c *Conn) send(seq uint32, payload []byte) error {
	buf := bytebufferpool.Get()
	defer bytebufferpool.Put(buf)

	buf.B = bytesutil.ExtendSlice(buf.B, 4+len(payload))
	binary.BigEndian.PutUint32(buf.B[:4], seq)
	copy(buf.B[4:], payload)

	return c.write(buf)
}

func (c *Conn) sendNoWait(seq uint32, payload []byte) error {
	buf := bytebufferpool.Get()
	buf.B = bytesutil.ExtendSlice(buf.B, 4+len(payload))
	binary.BigEndian.PutUint32(buf.B[:4], seq)
	copy(buf.B[4:], payload)
	return c.writeNoWait(buf)
}

func (c *Conn) write(buf *bytebufferpool.ByteBuffer) error {
	pw, err := c.preparePendingWrite(buf, true)
	if err != nil {
		return err
	}
	defer releasePendingWrite(pw)
	pw.wg.Wait()
	return pw.err
}

func (c *Conn) writeNoWait(buf *bytebufferpool.ByteBuffer) error {
	_, err := c.preparePendingWrite(buf, false)
	return err
}

func (c *Conn) preparePendingWrite(buf *bytebufferpool.ByteBuffer, wait bool) (*pendingWrite, error) {
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
	var queue []*pendingWrite
	var err error

	for {
		c.mu.Lock()
		for !c.writerDone && len(c.writerQueue) == 0 {
			c.writerCond.Wait()
		}
		done := c.writerDone

		if n := len(c.writerQueue) - cap(queue); n > 0 {
			queue = append(queue[:cap(queue)], make([]*pendingWrite, n)...)
		}
		queue = queue[:len(c.writerQueue)]

		copy(queue, c.writerQueue)

		c.writerQueue = c.writerQueue[:0]
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
						bytebufferpool.Put(pw.buf)
						releasePendingWrite(pw)
					}
				}
				break
			}
		}

		for _, pw := range queue {
			if err == nil {
				_, err = conn.Write(pw.buf.B)
			}
			if pw.wait {
				pw.err = err
				pw.wg.Done()
			} else {
				bytebufferpool.Put(pw.buf)
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

	if err != nil {
		err = fmt.Errorf("write_loop: %w", err)
	}

	return err
}

func (c *Conn) readLoop(conn BufferedConn) error {
	buf := make([]byte, c.getReadBufferSize())

	var (
		n   int
		err error
	)

	for {
		timeout := c.getReadTimeout()
		if timeout > 0 {
			err = conn.SetReadDeadline(time.Now().Add(timeout))
			if err != nil {
				break
			}
		}

		n, err = conn.Read(buf)
		if err != nil {
			break
		}

		data := buf[:n]
		if len(data) < 4 {
			err = fmt.Errorf("no sequence number to decode: %w", io.ErrUnexpectedEOF)
			break
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
			err = c.call(seq, data)
			if err != nil {
				err = fmt.Errorf("handler encountered an error: %w", err)
				break
			}
			continue
		}

		// received response

		pr.dst = bytesutil.ExtendSlice(pr.dst, len(data))
		copy(pr.dst, data)

		pr.wg.Done()
	}

	if err != nil {
		err = fmt.Errorf("read_loop: %w", err)
	}

	return err
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
			bytebufferpool.Put(pw.buf)
			releasePendingWrite(pw)
		}
	}

	c.writerQueue = nil

	for seq := range c.reqs {
		pr := c.reqs[seq]
		pr.err = err
		pr.wg.Done()

		delete(c.reqs, seq)
	}

	c.seq = 0
}
