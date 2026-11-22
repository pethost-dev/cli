package reach

import (
	"context"
	"errors"

	pethost "github.com/pethost-dev/sdk/go"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/remote"
	"github.com/pethost-dev/cli/internal/say"
	"github.com/pethost-dev/cli/internal/session"
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
			sayHowToStart(a, project, serviceOf(f.project, service))
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
	status, err := session.Run(ctx, a.UI, conn, "")
	switch {
	case errors.Is(err, remote.ErrLost):
		return lostConnection(a, exitSessionBroke, "The connection to your machine was lost.")
	case err != nil:
		return err
	case status != 0:
		return fail.Ended(status, "The shell exited with code %d.", status)
	}
	return nil
}
