package monte

import (
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
	"net"
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
