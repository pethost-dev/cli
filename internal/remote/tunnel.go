package remote

import (
	"errors"
	"io"
	"net"
	"strconv"

	"golang.org/x/crypto/ssh"
)

// Host is where a tunnel is always open, and so the host of the address it is given at: every
// client takes it for TCP, while a MySQL client takes "localhost" for its Unix socket and never
// reaches the port.
const Host = "127.0.0.1"

// Listen opens a port on this computer for a tunnel, reachable from this computer alone:
// Host and, where the computer has IPv6, ::1 as well, since many programs take "localhost" for
// the second. port 0 = one the system picks. It returns the port opened.
func Listen(port int) (listeners []net.Listener, opened int, err error) {
	v4, err := net.Listen("tcp4", net.JoinHostPort(Host, strconv.Itoa(port)))
	if err != nil {
		return nil, 0, err
	}
	opened = v4.Addr().(*net.TCPAddr).Port
	listeners = []net.Listener{v4}

	if !hasIPv6Loopback() {
		return listeners, opened, nil
	}
	// With ::1 there, the port is free only if it is free on both: a program that asks for
	// "localhost" must not reach somebody else's listener.
	v6, err := net.Listen("tcp6", net.JoinHostPort("::1", strconv.Itoa(opened)))
	if err != nil {
		v4.Close()
		return nil, 0, err
	}
	return append(listeners, v6), opened, nil
}

func hasIPv6Loopback() bool {
	probe, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		return false
	}
	probe.Close()
	return true
}

// Forward takes the connections that come to listener and joins each to a port of the
// connection's own container, until the listener is closed. Each is a channel of its own, so
// they run side by side. refused is told why a connection could not be joined: nothing listens
// on the port, say, in the machine's words.
func (c *Conn) Forward(listener net.Listener, port uint32, refused func(reason string)) {
	for {
		local, err := listener.Accept()
		if err != nil {
			return
		}
		c.guard(func() { c.join(local, port, refused) })
	}
}

// join carries one local connection to the container's port and back until both sides are done.
func (c *Conn) join(local net.Conn, port uint32, refused func(reason string)) {
	defer local.Close()

	// "localhost" is, to the machine's gateway, the container this connection logged in to.
	remote, err := c.client.Dial("tcp", net.JoinHostPort("localhost", strconv.FormatUint(uint64(port), 10)))
	if err != nil {
		refused(openFailure(err))
		return
	}
	defer remote.Close()

	done := make(chan struct{}, 2)
	c.guard(func() { carry(remote, local, done) })
	c.guard(func() { carry(local, remote, done) })
	<-done
	<-done
}

// carry copies one direction of a tunnel, and passes the end of what it read on as the end of
// what it writes, so that each side sees the other finish.
func carry(to, from net.Conn, done chan<- struct{}) {
	_, _ = io.Copy(to, from)
	if half, ok := to.(interface{ CloseWrite() error }); ok {
		_ = half.CloseWrite()
	}
	done <- struct{}{}
}

// openFailure is why the machine did not open a tunnel's channel: its own message where it
// gave one.
func openFailure(err error) string {
	if failure, ok := errors.AsType[*ssh.OpenChannelError](err); ok && failure.Message != "" {
		return failure.Message
	}
	return err.Error()
}
