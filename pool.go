package monte

import (
	"github.com/valyala/bytebufferpool"
	"sync"
	"time"
)

type Context struct {
	conn *Conn
	seq  uint32
	buf  []byte
}

func (c *Context) Conn() *Conn            { return c.conn }
func (c *Context) Body() []byte           { return c.buf }
func (c *Context) Reply(buf []byte) error { return c.conn.send(c.seq, buf) }

var contextPool sync.Pool

func acquireContext(conn *Conn, seq uint32, buf []byte) *Context {
	v := contextPool.Get()
	if v == nil {
		v = &Context{}
	}
	ctx := v.(*Context)
	ctx.conn = conn
	ctx.seq = seq
	ctx.buf = buf
	return ctx
}

func releaseContext(ctx *Context) { contextPool.Put(ctx) }

type pendingWrite struct {
	buf  *bytebufferpool.ByteBuffer // payload
	wait bool                       // signal to caller if they're waiting
	err  error                      // keeps track of any socket errors on write
	wg   sync.WaitGroup             // signals the caller that this write is complete
}

var pendingWritePool sync.Pool

func acquirePendingWrite(buf *bytebufferpool.ByteBuffer, wait bool) *pendingWrite {
	v := pendingWritePool.Get()
	if v == nil {
		v = &pendingWrite{}
	}
	pw := v.(*pendingWrite)
	pw.buf = buf
	pw.wait = wait
	return pw
}

func releasePendingWrite(pw *pendingWrite) { pendingWritePool.Put(pw) }

type pendingRequest struct {
	dst []byte         // dst to copy response to
	err error          // error while waiting for response
	wg  sync.WaitGroup // signals the caller that the response has been received
}

var pendingRequestPool sync.Pool

func acquirePendingRequest(dst []byte) *pendingRequest {
	v := pendingRequestPool.Get()
	if v == nil {
		v = &pendingRequest{}
	}
	pr := v.(*pendingRequest)
	pr.dst = dst
	return pr
}

func releasePendingRequest(pr *pendingRequest) { pendingRequestPool.Put(pr) }

var zeroTime time.Time

var timerPool sync.Pool

func AcquireTimer(timeout time.Duration) *time.Timer {
	v := timerPool.Get()
	if v == nil {
		return time.NewTimer(timeout)
	}
	t := v.(*time.Timer)
	t.Reset(timeout)
	return t
}

func ReleaseTimer(t *time.Timer) {
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
	timerPool.Put(t)
}
