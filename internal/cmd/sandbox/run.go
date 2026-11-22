package sandbox

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/pethost-dev/sdk/go/sandboxes"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/ui"
)

// stdinAtMost is what -i may give a command: a request holds 1 MiB at most, its command and the
// rest beside.
const stdinAtMost = 1<<20 - 16<<10

type runOptions struct {
	cwd, user   string
	timeout     time.Duration
	keepAwake   bool
	waitForPort uint32
	detach      bool
	stdin       bool
}

// runCommand is `pethost sandbox run <id> -- <command>`.
func runCommand(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "run <id> -- <command>",
		Aliases: []string{"exec"},
		Short:   "Run a command in a sandbox: its output as it comes, its exit code",
		Long: `Runs a command in a sandbox, by bash -lc, as the user "user" in /home/user, and writes
its output as it comes. A paused sandbox wakes for it. One word after -- is a shell line as
it is ("npm ci && npm test"); several are quoted as a shell takes them.

pethost exits as the command did: 124 when it was stopped at its --timeout, 125 when
pethost itself failed or the command's end is not known. Ctrl-C stops the command. With
-d it runs on in the sandbox, and its id is the answer.`,
		Example: `  pethost sandbox run k7f3q2xy0a1b2c3d -- python3 -c 'print(6 * 7)'
  pethost sandbox run k7f3q2xy0a1b2c3d -- 'npm ci && npm test'
  pethost sandbox run k7f3q2xy0a1b2c3d --cwd /home/user/app -- npm test
  pethost sandbox run k7f3q2xy0a1b2c3d -d --wait-for-port 3000 -- npm run dev
  pethost sandbox run k7f3q2xy0a1b2c3d -i -- python3 < script.py`,
		Args:              cobra.ArbitraryArgs,
		ValidArgsFunction: completeSandboxes(a),
	}
	var options runOptions
	cmd.Flags().StringVar(&options.cwd, "cwd", "", "run it in this directory (default /home/user)")
	cmd.Flags().StringVar(&options.user, "user", "", `run it as this user (default "user"); "root" can do anything`)
	cmd.Flags().DurationVar(&options.timeout, "timeout", 0, "stop it after this long, as 10m (default: never)")
	cmd.Flags().BoolVar(&options.keepAwake, "keep-awake", false, "keep the sandbox from pausing while it runs: a build nobody watches")
	cmd.Flags().Uint32Var(&options.waitForPort, "wait-for-port", 0, "with -d: answer once something listens on this port")
	cmd.Flags().BoolVarP(&options.detach, "detach", "d", false, "leave it running and print its id")
	cmd.Flags().BoolVarP(&options.stdin, "stdin", "i", false, "give it what is piped in as its stdin, up to 1 MiB")

	cmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		// Worded as every command's wrong flag is, by the root's function.
		return fail.Own(a.UI, a.JSON, cmd.Root().FlagErrorFunc()(cmd, err))
	})
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		id, words, err := splitRun(cmd, args)
		if err != nil {
			return fail.Own(a.UI, a.JSON, err)
		}
		return fail.Own(a.UI, a.JSON, run(cmd.Context(), a, id, words, options))
	}
	return cmd
}

// splitRun is the sandbox named before "--" and the command after it.
func splitRun(cmd *cobra.Command, args []string) (string, []string, error) {
	dash := cmd.ArgsLenAtDash()
	switch {
	case dash < 0:
		return "", nil, fail.Usage("pethost sandbox run: put -- before the command, as in  pethost sandbox run <id> -- ls", cmd.UseLine())
	case dash != 1:
		return "", nil, fail.Usage("pethost sandbox run takes the sandbox's id before --", cmd.UseLine())
	case dash == len(args):
		return "", nil, fail.Usage("pethost sandbox run: there is no command after --", cmd.UseLine())
	}
	return args[0], args[dash:], nil
}

func run(ctx context.Context, a *app.App, id string, words []string, options runOptions) error {
	c, err := clientForChange(a)
	if err != nil {
		return err
	}
	id = idOf(ctx, c, id)
	timeout, err := seconds("timeout", options.timeout)
	if err != nil {
		return err
	}
	request := &sandboxes.RunCommandRequest{
		SandboxID: id, Command: joined(words), Cwd: options.cwd, User: options.user,
		TimeoutSeconds: timeout, KeepAwake: options.keepAwake, WaitForPort: options.waitForPort,
	}
	if request.Stdin, err = pipedIn(a, options); err != nil {
		return err
	}

	r := &running{a: a, c: c, id: id}
	switch {
	case options.detach:
		return r.detached(ctx, request)
	case a.JSON:
		return r.forJSON(ctx, request)
	}
	return r.following(ctx, request)
}

// pipedIn is what -i gives the command as its stdin: all that is piped in, within a request's
// bound.
func pipedIn(a *app.App, options runOptions) (string, error) {
	if !options.stdin {
		return "", nil
	}
	piped, err := io.ReadAll(io.LimitReader(a.UI.In, stdinAtMost+1))
	if err != nil {
		return "", err
	}
	if len(piped) > stdinAtMost {
		return "", fail.Usage("More than 1 MiB was piped in: copy it into the sandbox with  pethost sandbox cp - <id>:<path>, and give the command that file.", "pethost sandbox run <id> -i -- <command>")
	}
	return string(piped), nil
}

// detached starts the command and leaves it running: its id is the answer, once it has started,
// or once something listens on --wait-for-port; with --json the command as it then is. One that
// ended before is shown as run shows it.
func (r *running) detached(ctx context.Context, request *sandboxes.RunCommandRequest) error {
	if request.WaitForPort == 0 {
		request.WaitSeconds = new(uint32(0))
	}
	command, err := r.c.RunCommand(ctx, request)
	if err != nil {
		return about(r.id, err)
	}
	if r.a.JSON {
		if err := r.a.PrintJSON(command); err != nil {
			return err
		}
	}
	if command.State != sandboxes.CommandStateRunning {
		r.write(command)
		return r.ended(command)
	}
	if r.a.JSON {
		return nil
	}

	id := ui.Clean(command.CommandID)
	r.a.UI.Info("It runs on in the sandbox. Its output so far:  %s", r.a.UI.Err.Bold(
		fmt.Sprintf(`pethost api GetCommand '{"sandbox_id": "%s", "command_id": "%s"}'`, ui.Clean(r.id), id)))
	_, err = fmt.Fprintln(r.a.UI.Out, id)
	return err
}

// forJSON waits for the command's end by GetCommand's waits, and prints the command as it ended:
// one answer, as --json prints every command's. It starts the command without waiting, as
// following does, so that Ctrl-C knows its id and stops it, however soon it comes.
func (r *running) forJSON(ctx context.Context, request *sandboxes.RunCommandRequest) error {
	request.WaitSeconds = new(uint32(0))
	command, err := r.c.RunCommand(ctx, request)
	if err != nil {
		return about(r.id, err)
	}
	r.command = command.CommandID
	for command.State == sandboxes.CommandStateRunning {
		command, err = r.c.GetCommand(ctx, &sandboxes.GetCommandRequest{SandboxID: r.id, CommandID: r.command})
		if err != nil {
			return r.interrupted(ctx, about(r.id, err))
		}
	}
	if err := r.a.PrintJSON(command); err != nil {
		return err
	}
	return r.ended(command)
}
