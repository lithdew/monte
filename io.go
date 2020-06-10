package monte

import (
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/lithdew/bytesutil"
	"io"
	"net"
)

type BufferedConn interface {
	net.Conn
	Flush() error
}

func Read(dst []byte, r io.Reader) ([]byte, error) {
	_, err := io.ReadFull(r, dst[:])
	if err != nil {
		return nil, err
	}
	return dst, nil
}

func Write(w io.Writer, buf []byte) error {
	n, err := w.Write(buf)
	if n != len(buf) {
		return io.ErrShortWrite
	}
	return err
}

func ReadSized(dst []byte, r io.Reader, max int) ([]byte, error) {
	var buf [4]byte
	_, err := io.ReadFull(r, buf[:])
	if err != nil {
		return nil, err
	}
	n := bytesutil.Uint32BE(buf[:])
	if int(n) > max {
		return nil, fmt.Errorf("max is %d bytes, got %d bytes", max, n)
	}
	dst = bytesutil.ExtendSlice(dst, int(n))
	_, err = io.ReadFull(r, dst[:])
	if err != nil {
		return nil, err
	}
	return dst, nil
}

func WriteSized(w io.Writer, buf []byte) error {
	buf = bytesutil.ExtendSlice(buf, len(buf)+4)
	binary.BigEndian.PutUint32(buf[len(buf)-4:], uint32(len(buf))-4)
	_, err := w.Write(buf[len(buf)-4:])
	if err == nil {
		_, err = w.Write(buf[:len(buf)-4])
	}
	return err
}

func IsEOF(err error) bool {
	if errors.Is(err, io.EOF) {
		return true
	}
	var netErr *net.OpError
	if !errors.As(err, &netErr) {
		return false
	}
	if netErr.Err.Error() == "use of closed network connection" {
		return true
	}
	return false
}
