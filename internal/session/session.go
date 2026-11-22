// Package session is this computer's side of a program's session over SSH, which internal/remote
// runs: at a terminal the keyboard and the screen are the program's, raw, with the window's size
// and TERM; with stdin piped the program gets no terminal, and what it prints is cleaned like
// any other text from elsewhere before a person's terminal shows it. pethost shell and pethost
// sandbox shell are sessions.
package session

import (
	"context"
	"io"
	"os"
	"sync"
	"sync/atomic"

	"github.com/pethost-dev/cli/internal/remote"
	"github.com/pethost-dev/cli/internal/ui"
)

// Run runs the login shell on the connection, or command when it is not empty, until it exits,
// and returns its exit status as remote's Shell does: remote.ErrLost = the connection ended
// first. The terminal is the person's again when it returns.
func Run(ctx context.Context, screen *ui.Screen, conn *remote.Conn, command string) (int, error) {
	local, giveBack := localSide(screen)
	local.Command = command
	defer giveBack()

	status, err := conn.Shell(ctx, local)
	// The terminal is the person's again before anything more is written to it.
	giveBack()

	if err != nil && local.Terminal != nil {
		screen.ErrLine("") // The cursor stands wherever the program left it.
	}
	return status, err
}

// localSide is this computer's side of the session, and what gives it back when the session
// has ended. At a terminal the keyboard and the screen are the program's: raw mode, the
// window's size, TERM. With stdin piped the program gets no terminal, and what it prints is
// cleaned like any other text from the machine before a person's terminal shows it.
func localSide(screen *ui.Screen) (local remote.Local, giveBack func()) {
	restore, err := screen.Raw()
	if err != nil {
		out, errOut := screen.Out.Cleaned(), screen.Err.Cleaned()
		return remote.Local{In: screen.In, Out: out, Err: errOut}, sync.OnceFunc(func() {
			_ = out.Close()
			_ = errOut.Close()
		})
	}

	stop := make(chan struct{})
	giveBack = sync.OnceFunc(func() {
		close(stop)
		restore()
	})
	out, said := io.Writer(screen.Out), io.Writer(screen.Err)
	if screen.Out.Terminal && screen.Err.Terminal {
		drawn := &drawn{Writer: out}
		out, said = drawn, &ownLine{Writer: said, after: drawn}
	}
	return remote.Local{
		In:  screen.Typed(stop),
		Out: out,
		Err: said,
		Terminal: &remote.Terminal{
			Name:    terminalName(),
			Size:    screen.Window,
			Restore: restore,
		},
	}, giveBack
}

// On a terminal the session's stderr is the machine's gateway speaking: why it opens no
// session, or why it ended one. The program's own stderr comes with its output. So the
// gateway's sentence would start wherever the program left the cursor, glued to a prompt or
// over a full-screen program's last line: it gets a line of its own.

// drawn is the program's output, which remembers whether any came.
type drawn struct {
	io.Writer
	any atomic.Bool
}

func (d *drawn) Write(p []byte) (int, error) {
	if len(p) > 0 {
		d.any.Store(true)
	}
	return d.Writer.Write(p)
}

// ownLine is the gateway's words, started on a new, empty line once the program has drawn.
type ownLine struct {
	io.Writer
	after *drawn
	begun bool
}

func (o *ownLine) Write(p []byte) (int, error) {
	if !o.begun && o.after.any.Load() {
		if _, err := io.WriteString(o.Writer, "\r\n\x1b[K"); err != nil {
			return 0, err
		}
	}
	o.begun = true
	return o.Writer.Write(p)
}

// terminalName is the kind of terminal the program is told it has: TERM, or the common one
// where nothing sets it, as on Windows, whose console speaks the same sequences.
func terminalName() string {
	if name := os.Getenv("TERM"); name != "" {
		return name
	}
	return "xterm-256color"
}
