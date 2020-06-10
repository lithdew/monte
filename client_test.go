package monte

import (
	"fmt"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestClientHandshakeTimeout(t *testing.T) {
	defer goleak.VerifyNone(t)

	ln, err := net.Listen("tcp", ":0")
	require.NoError(t, err)

	client := &Client{Addr: ln.Addr().String(), HandshakeTimeout: 1 * time.Millisecond}

	defer func() {
		client.Shutdown()
		require.NoError(t, ln.Close())
	}()

	attempts := 16
	go func() {
		for i := 0; i < attempts; i++ {
			_, _ = ln.Accept()
		}
	}()

	for i := 0; i < attempts; i++ {
		require.Error(t, client.Write([]byte("hello\n")))
	}
}

func TestEndToEnd(t *testing.T) {
	defer goleak.VerifyNone(t)

	ln, err := net.Listen("tcp", ":0")
	require.NoError(t, err)

	var server Server

	client := &Client{Addr: ln.Addr().String()}

	go func() {
		require.NoError(t, server.Serve(ln))
	}()

	n := 4
	m := 1024
	c := uint32(n * m)

	defer func() {
		server.Shutdown()
		client.Shutdown()

		require.NoError(t, ln.Close())
		require.EqualValues(t, 0, atomic.LoadUint32(&c))
	}()

	var wg sync.WaitGroup
	wg.Add(n)

	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			for j := 0; j < m; j++ {
				require.NoError(t, client.Write([]byte(fmt.Sprintf("[%d] hello %d", i, j))))
				atomic.AddUint32(&c, ^uint32(0))
			}
		}(i)
	}

	wg.Wait()
}
