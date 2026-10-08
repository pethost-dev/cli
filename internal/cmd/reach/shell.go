package reach

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"

	pethost "github.com/pethost-dev/sdk/go"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/remote"
	"github.com/pethost-dev/cli/internal/say"
	"github.com/pethost-dev/cli/internal/target"
	"github.com/pethost-dev/cli/internal/ui"
)

// exitSessionBroke is how a shell ends when its connection did: the code ssh uses, and the one
// the machine's gateway ends a session with when it cannot run it.
const exitSessionBroke = 255

// shellCommand is `pethost shell [service]`.
func shellCommand(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "shell [service]",
		Short: "Open a shell in a container",
		Long: `Opens a shell in a service's running container, over SSH, with nothing to install.

On a terminal it is the container's own terminal: keys, colours, full-screen programs and the
window's size all pass through. With commands piped in, it runs them and exits as the last
one did. pethost exits as the shell did, and 255 when the connection broke.

The first use adds this computer's key to your machine: pethost machine lists the keys.`,
		Example: `  pethost shell
  pethost shell db
  echo 'ls -la /data' | pethost shell web`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: target.CompleteServices(a),
	}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		project, err := target.ForRead(ctx, a)
		if err != nil {
			return err
		}
		f, err := readFacts(ctx, a, project)
		if err != nil {
			return err
		}
		service, err := target.Service(a, firstOf(args), target.ServiceNames(f.project), func(service string) string {
			return "pethost shell " + app.ShellWord(service) + project.Flag()
		})
		if err != nil {
			return err
		}

		conn, err := dialService(ctx, a, f, service)
		if err != nil {
			return err
		}
		defer conn.Close()

		err = openShell(ctx, a, conn)
		if err != nil {
			sayHowToStart(a, project, f.service(service))
		}
		return err
	}
	return cmd
}

// sayHowToStart names, under the machine's refusal of a session, the command that starts the
// service when the machine had reported that its container does not run.
func sayHowToStart(a *app.App, project target.Project, service *pethost.Service) {
	if service == nil || service.ContainerRunning {
		return
	}
	start := "pethost start " + app.ShellWord(service.Service) + project.Flag()
	a.UI.Info("%s is %s. Start it:  %s", ui.Clean(service.Service), say.State(service.State), a.UI.Err.Bold(start))
}

// firstOf is a command's one optional argument; empty = not given.
func firstOf(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

// openShell runs the container's shell until it exits, and ends as it did.
func openShell(ctx context.Context, a *app.App, conn *remote.Conn) error {
	local, giveBack := localSide(a)
	defer giveBack()

	status, err := conn.Shell(ctx, local)
	// The terminal is the person's again before anything more is written to it.
	giveBack()

	if err != nil && local.Terminal != nil {
		a.UI.ErrLine("") // The cursor stands wherever the shell left it.
	}
	switch {
	case errors.Is(err, remote.ErrLost):
		return lostConnection(a, exitSessionBroke, "The connection to your machine was lost.")
	case err != nil:
		return err
	case status != 0:
		return &fail.Error{Exit: exitCodeOf(int32(min(status, exitSessionBroke))), Code: commandEnded, Shown: true,
			Message: fmt.Sprintf("The shell exited with code %d.", status)}
	}
	return nil
}

// localSide is this computer's side of the session, and what gives it back when the session
// has ended. At a terminal the keyboard and the screen are the program's: raw mode, the
// window's size, TERM. With stdin piped the program gets no terminal, and what it prints is
// cleaned like any other text from the machine before a person's terminal shows it.
func localSide(a *app.App) (local remote.Local, giveBack func()) {
	restore, err := a.UI.Raw()
	if err != nil {
		out, errOut := a.UI.Out.Cleaned(), a.UI.Err.Cleaned()
		return remote.Local{In: a.UI.In, Out: out, Err: errOut}, sync.OnceFunc(func() {
			_ = out.Close()
			_ = errOut.Close()
		})
	}

	stop := make(chan struct{})
	giveBack = sync.OnceFunc(func() {
		close(stop)
		restore()
	})
	out, said := io.Writer(a.UI.Out), io.Writer(a.UI.Err)
	if a.UI.Out.Terminal && a.UI.Err.Terminal {
		drawn := &drawn{Writer: out}
		out, said = drawn, &ownLine{Writer: said, after: drawn}
	}
	return remote.Local{
		In:  a.UI.Typed(stop),
		Out: out,
		Err: said,
		Terminal: &remote.Terminal{
			Name:    terminalName(),
			Size:    a.UI.Window,
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
