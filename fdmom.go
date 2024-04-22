// Package fdmom provides supervision over file descriptors.
package fdmom

import (
	"errors"
	"fmt"
	"net"
	"os"
)

// ErrTimeout is a reason for no results.
var ErrTimeout = errors.New("fdmom: await interrupted by timeout")

// A Filer grants its file (descriptor).
type filer interface {
	File() (*os.File, error)
}

// ListenerFile returns a new representative of the underlying file from the
// listener. The representative has a different file descriptor. Closing the
// file does not affect the listener, and vise versa. Attempting to change
// properties of the listener with the return may or may not have the desired
// effect.
func ListenerFile(l net.Listener) (*os.File, error) {
	withFile, ok := l.(filer)
	if !ok {
		return nil, fmt.Errorf("listener %T does not provide its file", l)
	}
	return withFile.File()
}

// A NetConner grants the underlying connection such as *tls.Conn does.
type netConner interface {
	NetConn() net.Conn
}

// ConnFile returns a new representative of the underlying file from the
// connection. The representative has a different file descriptor. Closing the
// file does not affect the connection, and vise versa. Attempting to change
// properties of the connection with the return may or may not have the desired
// effect.
func ConnFile(conn net.Conn) (*os.File, error) {
	nested, ok := conn.(netConner)
	if ok {
		conn = nested.NetConn()
	}

	withFile, ok := conn.(filer)
	if !ok {
		return nil, fmt.Errorf("connection %T does not provide its file", conn)
	}
	return withFile.File()
}
