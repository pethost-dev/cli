package remote

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"sync"

	"golang.org/x/crypto/ssh"
)

// A Local is this side of a session: where the program's input comes from and where its output
// goes.
type Local struct {
	In       io.Reader
	Out, Err io.Writer
	// The person's terminal, which the program then gets one like. nil = none: the program reads
	// a pipe and sees its end.
	Terminal *Terminal
}

// A Terminal is the person's terminal while a session holds it: already in raw mode, so that
// every key reaches the program and the program draws the screen.
type Terminal struct {
	// Its kind, as TERM says it: "xterm-256color".
	Name string
	// Size reads the window's size now.
	Size func() (columns, rows int)
	// Restore takes it out of raw mode. The session's caller does when the session ends; the
	// session itself only before a panic ends the process.
	Restore func()
}

// Shell runs the container's login shell on the connection until it exits, and returns its exit
// status. The machine's refusal of the login target comes the same way: its sentence on Err,
// and its status. ErrLost = the connection ended before the shell did.
func (c *Conn) Shell(ctx context.Context, local Local) (status int, err error) {
	channel, requests, err := c.client.OpenChannel("session", nil)
	if err != nil {
		return 0, c.orLost(err)
	}
	defer channel.Close()

	if local.Terminal != nil {
		c.restore.Store(&local.Terminal.Restore)
		defer c.restore.Store(nil)
	}

	// The output is read from the first moment: what the machine says when it refuses the
	// session arrives before the answer to any request.
	s := &session{conn: c, channel: channel, local: local}
	written := s.copyOutput()
	exited := s.exitStatus(requests)

	startErr := s.start()
	if startErr != nil {
		channel.Close()
	} else {
		c.guard(s.copyInput)
		stop := make(chan struct{})
		defer close(stop)
		if local.Terminal != nil {
			c.guard(func() { watchWindow(stop, s.resize) })
		}
	}

	select {
	case status = <-exited:
		<-written
	case <-ctx.Done():
		return 0, ctx.Err()
	}

	switch {
	case status >= 0:
		return status, nil
	case startErr != nil:
		return 0, c.orLost(startErr)
	}
	return 0, ErrLost
}

// orLost is err, or ErrLost when the connection has ended: that is then why err happened.
func (c *Conn) orLost(err error) error {
	select {
	case <-c.lost:
		return ErrLost
	default:
		return err
	}
}

// session is one program on a session channel.
type session struct {
	conn    *Conn
	channel ssh.Channel
	local   Local
	// The terminal's size as the machine knows it.
	columns, rows int
}

// The payloads of the session requests, as RFC 4254 lays them out (6.2, 6.7).
type (
	ptyRequest struct {
		Term          string
		Columns, Rows uint32
		Width, Height uint32 // In pixels; 0 = not said.
		Modes         string
	}
	windowChange struct {
		Columns, Rows uint32
		Width, Height uint32
	}
)

// noModes is an empty list of terminal modes: the program's terminal keeps its own.
const noModes = "\x00"

// start asks for a terminal, where the person has one, and then for the shell.
func (s *session) start() error {
	if terminal := s.local.Terminal; terminal != nil {
		s.columns, s.rows = terminal.Size()
		request := ptyRequest{Term: terminal.Name, Columns: uint32(s.columns), Rows: uint32(s.rows), Modes: noModes}
		if err := s.request("pty-req", ssh.Marshal(request)); err != nil {
			return err
		}
	}
	return s.request("shell", nil)
}

func (s *session) request(name string, payload []byte) error {
	granted, err := s.channel.SendRequest(name, true, payload)
	if err == nil && !granted {
		return errors.New("the machine refused the session's " + name)
	}
	return err
}

// resize tells the machine the window's size when it has changed.
func (s *session) resize() {
	columns, rows := s.local.Terminal.Size()
	if columns == s.columns && rows == s.rows {
		return
	}
	s.columns, s.rows = columns, rows
	change := windowChange{Columns: uint32(columns), Rows: uint32(rows)}
	_, _ = s.channel.SendRequest("window-change", false, ssh.Marshal(change))
}

// copyInput sends what is typed or piped in. A pipe's end closes the program's stdin. A
// terminal has no end, and the program reads Ctrl-D itself: when its input ends all the same,
// the terminal is gone, and the session ends with it instead of staying open on the machine
// with nobody at it.
func (s *session) copyInput() {
	_, err := io.Copy(s.channel, s.local.In)
	switch {
	case s.local.Terminal != nil:
		_ = s.channel.Close()
	case err == nil:
		_ = s.channel.CloseWrite()
	}
}

// copyOutput writes the program's two streams as they come; the channel it returns is closed
// when both have ended, which they do when the session's channel closes.
func (s *session) copyOutput() <-chan struct{} {
	var streams sync.WaitGroup
	streams.Add(2)
	s.conn.guard(func() {
		defer streams.Done()
		_, _ = io.Copy(s.local.Out, s.channel)
	})
	s.conn.guard(func() {
		defer streams.Done()
		_, _ = io.Copy(s.local.Err, s.channel.Stderr())
	})

	written := make(chan struct{})
	s.conn.guard(func() {
		streams.Wait()
		close(written)
	})
	return written
}

// exitStatus answers the machine's requests on the channel and gives, once the channel has
// closed, the program's exit status; -1 = the channel closed without one.
func (s *session) exitStatus(requests <-chan *ssh.Request) <-chan int {
	exited := make(chan int, 1)
	s.conn.guard(func() {
		status := -1
		for request := range requests {
			if request.Type == "exit-status" && len(request.Payload) >= 4 {
				status = int(binary.BigEndian.Uint32(request.Payload))
			}
			if request.WantReply {
				_ = request.Reply(false, nil)
			}
		}
		exited <- status
	})
	return exited
}
