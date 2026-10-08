package remote

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"
)

// A Target is where a connection goes and what it must find there.
type Target struct {
	// The machine's name.
	Host string
	// Who logs in: "<service>.<project id>", which the machine's gateway reads as that service's
	// container.
	User string
	// The fingerprint of the machine's host key as the machine reports it, "SHA256:...": the one
	// key the connection takes. No file of known hosts is read or written, and nobody is asked.
	HostKeyFingerprint string
}

// How a connection fails before it is one: the kinds a DialError tells apart.
var (
	// Nothing answered at the machine's SSH port.
	ErrNotReached = errors.New("the machine's SSH did not answer")
	// What answered showed another host key than the machine reports: nothing was sent to it.
	ErrWrongHost = errors.New("the host key is not the machine's")
	// The machine did not let this key in.
	ErrRefused = errors.New("the machine did not take the key")
)

// A DialError is a connection that could not be made.
type DialError struct {
	// ErrNotReached, ErrWrongHost or ErrRefused.
	Kind error
	// What the network or SSH said.
	Cause error
}

func (e *DialError) Error() string { return e.Kind.Error() + ": " + e.Cause.Error() }

func (e *DialError) Unwrap() []error { return []error{e.Kind, e.Cause} }

// ErrLost is the end of a session or a tunnel whose connection ended under it.
var ErrLost = errors.New("the connection to the machine was lost")

// A Conn is an SSH connection to one service's container.
type Conn struct {
	client *ssh.Client
	// Closed once the connection has ended, whoever ended it.
	lost chan struct{}
	// What puts the person's terminal back while a session holds it raw; nil otherwise.
	restore atomic.Pointer[func()]
}

const (
	sshPort = "22"
	// How long the machine has to take the connection, and then to finish SSH's handshake.
	dialTimeout      = 15 * time.Second
	handshakeTimeout = 20 * time.Second
)

// Dial connects to the target and logs in with the key.
func Dial(ctx context.Context, target Target, key *Key) (*Conn, error) {
	address := net.JoinHostPort(target.Host, sshPort)
	dialer := net.Dialer{Timeout: dialTimeout}
	netConn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, &DialError{Kind: ErrNotReached, Cause: err}
	}

	// The handshake takes no context: a deadline bounds it, and the context's end closes the
	// connection under it.
	stop := context.AfterFunc(ctx, func() { netConn.Close() })
	_ = netConn.SetDeadline(time.Now().Add(handshakeTimeout))
	check := hostKeyCheck{want: target.HostKeyFingerprint}
	sshConn, channels, requests, err := ssh.NewClientConn(netConn, address, &ssh.ClientConfig{
		User:            target.User,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(key.signer)},
		HostKeyCallback: check.callback,
		ClientVersion:   "SSH-2.0-pethost",
	})
	if !stop() {
		err = ctx.Err()
	}
	if err != nil {
		netConn.Close()
		return nil, check.failure(ctx, err)
	}
	_ = netConn.SetDeadline(time.Time{})

	c := &Conn{client: ssh.NewClient(sshConn, channels, requests), lost: make(chan struct{})}
	c.guard(func() {
		_ = c.client.Wait()
		close(c.lost)
	})
	c.guard(c.keepAlive)
	return c, nil
}

// hostKeyCheck holds the key a host shows against the one expected, and remembers how far the
// handshake got: SSH's own error does not say.
type hostKeyCheck struct {
	want    string
	shown   bool
	matched bool
}

func (h *hostKeyCheck) callback(_ string, _ net.Addr, key ssh.PublicKey) error {
	h.shown = true
	h.matched = h.want != "" && ssh.FingerprintSHA256(key) == h.want
	if !h.matched {
		return ErrWrongHost
	}
	return nil
}

// failure is a failed handshake as a DialError: before the host showed its key the machine was
// not reached, a wrong key is a wrong host, and after the right one the login was refused.
func (h *hostKeyCheck) failure(ctx context.Context, err error) error {
	switch {
	case ctx.Err() != nil:
		return ctx.Err()
	case !h.shown:
		return &DialError{Kind: ErrNotReached, Cause: err}
	case !h.matched:
		return &DialError{Kind: ErrWrongHost, Cause: err}
	}
	return &DialError{Kind: ErrRefused, Cause: err}
}

// Lost is closed once the connection has ended.
func (c *Conn) Lost() <-chan struct{} {
	return c.lost
}

// Close ends the connection, and with it every session and tunnel on it.
func (c *Conn) Close() {
	_ = c.client.Close()
}

const (
	// How often the machine is asked whether it is still there: an idle shell must outlast the
	// routers between, which forget a connection that says nothing.
	keepAliveEvery = 30 * time.Second
	// How long its answer may take before the connection counts as lost.
	keepAliveWait = 15 * time.Second
)

// keepAlive asks the machine for a sign of life at intervals, and ends the connection when none
// comes: a shell then says so at once instead of hanging on a dead line.
func (c *Conn) keepAlive() {
	ticker := time.NewTicker(keepAliveEvery)
	defer ticker.Stop()
	for {
		select {
		case <-c.lost:
			return
		case <-ticker.C:
			if !c.answers() {
				c.Close()
				return
			}
		}
	}
}

// answers says whether the machine answers a request in time. Any answer counts, a refusal too:
// the request is one no server knows.
func (c *Conn) answers() bool {
	answered := make(chan error, 1)
	c.guard(func() {
		_, _, err := c.client.SendRequest("keepalive@openssh.com", true, nil)
		answered <- err
	})

	select {
	case err := <-answered:
		return err == nil
	case <-time.After(keepAliveWait):
		return false
	case <-c.lost:
		return false
	}
}

// guard runs work in a goroutine of its own. A panic there would end the process with the
// person's terminal still raw: it is put back first.
func (c *Conn) guard(work func()) {
	go func() {
		defer func() {
			if failure := recover(); failure != nil {
				if restore := c.restore.Load(); restore != nil {
					(*restore)()
				}
				panic(failure)
			}
		}()
		work()
	}()
}
