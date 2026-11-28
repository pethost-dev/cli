package sandbox

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/pethost-dev/sdk/go/sandboxes"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/kept"
	"github.com/pethost-dev/cli/internal/remote"
	"github.com/pethost-dev/cli/internal/session"
	"github.com/pethost-dev/cli/internal/ui"
)

const (
	// exitSessionBroke is how a shell ends when its connection did: the code ssh uses.
	exitSessionBroke = 255
	// shellTokenSeconds is how long the token a shell signs in with lasts: it is used once, to
	// sign in, and the session outlives it.
	shellTokenSeconds = 300
	// gatewayPort is where a sandbox server's SSH gateway listens.
	//
	// ponytail: the API says the gateway's address only inside Sandbox.ssh_command, a command for
	// people, which is not parsed; when Connection gains its port (and the gateway's host key,
	// which knownHostsFile then gives way to, as pethost shell checks a machine's), use them.
	gatewayPort = "2222"
	// knownHostsFile is where the host keys of the sandbox servers a shell reached are kept,
	// beside the sign-in, in OpenSSH's format.
	knownHostsFile = "sandbox_known_hosts"
)

// shellCommand is `pethost sandbox shell <id> [-- <command>]`.
func shellCommand(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:     "shell <id> [-- <command>]",
		Aliases: []string{"ssh"},
		Short:   "Open a shell in a sandbox, or run a program there on a terminal",
		Long: `Opens a shell in a sandbox over SSH, with nothing to install, as the user "user". A paused
sandbox wakes for it. After --, a program to run instead of the shell, on a terminal:
pethost sandbox shell <id> -- claude.

On a terminal it is the sandbox's own terminal: keys, colours, full-screen programs and
the window's size all pass through. With commands piped in, it runs them and exits as the
last one did. pethost exits as the shell did, and 255 when the connection broke.

It signs in with a token that lasts five minutes, never shown. The server's SSH key is
kept the first time and must be the same from then on: a changed one is refused.`,
		Example: `  pethost sandbox shell k7f3q2xy0a1b2c3d
  pethost sandbox shell k7f3q2xy0a1b2c3d -- htop
  echo 'ls -la' | pethost sandbox shell k7f3q2xy0a1b2c3d`,
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: completeSandboxes(a),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, command := args[0], ""
			dash := cmd.ArgsLenAtDash()
			switch {
			case dash < 0 && len(args) > 1:
				return fail.Usage("pethost sandbox shell: put -- before the program, as in  pethost sandbox shell <id> -- htop", cmd.UseLine())
			case dash >= 0 && dash != 1:
				return fail.Usage("pethost sandbox shell takes the sandbox's id before --", cmd.UseLine())
			case dash == 1 && len(args) > 1:
				command = joined(args[1:])
			}
			return shell(cmd.Context(), a, id, command)
		},
	}
}

func shell(ctx context.Context, a *app.App, id, command string) error {
	c, err := client(a)
	if err != nil {
		return err
	}
	id, err = idOf(ctx, c, id)
	if err != nil {
		return err
	}
	conn, err := dialSandbox(ctx, a, c, id)
	if err != nil {
		return err
	}
	defer conn.Close()

	status, err := session.Run(ctx, a.UI, conn, command)
	switch {
	case errors.Is(err, remote.ErrLost):
		return &fail.Error{Exit: exitSessionBroke, Code: "CONNECTION_LOST", Message: "The connection to the sandbox was lost.",
			Lead: "To connect again, run:", Next: a.CommandLine()}
	case err != nil:
		return err
	case status != 0:
		return fail.Ended(status, "The shell exited with code %d.", status)
	}
	return nil
}

// dialSandbox opens SSH to a sandbox's server's gateway, signed in with a short token.
func dialSandbox(ctx context.Context, a *app.App, c *sandboxes.Client, id string) (*remote.Conn, error) {
	connection, err := c.ConnectSandbox(ctx, &sandboxes.ConnectSandboxRequest{SandboxID: id, TTLSeconds: shellTokenSeconds})
	if err != nil {
		return nil, about(id, err)
	}
	server, err := url.Parse(connection.URL)
	if err != nil || server.Hostname() == "" {
		return nil, fail.Failed("The API gave the sandbox's server an address SSH cannot reach.")
	}
	known, err := kept.File(knownHostsFile)
	if err != nil {
		return nil, err
	}

	a.Debugf("ssh to %s", server.Hostname())
	conn, err := remote.Dial(ctx, remote.Target{
		Host: server.Hostname(), Port: gatewayPort, User: id, Password: connection.Token, KnownHosts: known,
	}, nil)
	if err != nil {
		return nil, dialFailure(server.Hostname(), known, err)
	}
	return conn, nil
}

// dialFailure is a connection that could not be made, in the words of what a person can do
// about it.
func dialFailure(host, known string, err error) error {
	failure, ok := errors.AsType[*remote.DialError](err)
	if !ok {
		return err
	}

	host = ui.Clean(host)
	switch failure.Kind {
	case remote.ErrWrongHost:
		return fail.Failed("What answers at %s shows another SSH key than the one kept for it in %s: nothing was sent to it. If the server's key really changed, remove its line there.", host, known)
	case remote.ErrRefused:
		if said := strings.TrimSpace(ui.Clean(failure.Banner)); said != "" {
			return fail.Failed("The sandbox's server took nobody in: %s", strings.TrimPrefix(said, "pethost: "))
		}
		return fail.Failed("The sandbox's server did not take the sign-in: %v.", fail.Innermost(failure.Cause))
	}
	e := fail.Failed("The sandbox's server does not answer SSH at %s: %v. Try again in a minute.", host, fail.Innermost(failure.Cause))
	e.Exit = fail.ExitLater
	return e
}
