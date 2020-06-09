package monte

import (
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
	"net"
	"testing"
)

func TestServerShutdown(t *testing.T) {
	defer goleak.VerifyNone(t)

	srv := &Server{}

	ln, err := net.Listen("tcp", ":0")
	require.NoError(t, err)

	go func() {
		srv.Shutdown()
		ln.Close()
	}()

	require.NoError(t, srv.Serve(ln))
}
