package reach

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	pethost "github.com/pethost-dev/sdk/go"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/target"
	"github.com/pethost-dev/cli/internal/ui"
)

// run exits as the command in the container did, so its own ends need codes no command is
// likely to use: the ones `timeout` and `docker run` gave these meanings.
const (
	exitTimedOut = 124 // The machine stopped the command at its timeout.
	exitOwn      = 125 // The CLI's own failure: the command did not run, or its end is not known.
)

type runOptions struct {
	user      string
	directory string
	timeout   uint32
	stdin     bool
}

// runCommand is `pethost run [service] -- <command>`.
func runCommand(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run [service] -- <command>",
		Short: "Run one command in a container: its output, its exit code",
		Long: `Runs one command in a service's running container and waits for its end.

The command's output goes to stdout and stderr as the command wrote it, and pethost exits as
the command did: 124 when the machine stopped it at its timeout, 125 when pethost itself
failed and the command's end is not known. The machine keeps the end of long output; for a
program that talks back, or all of a long output, use pethost shell.`,
		Example: `  pethost run -- ls /app
  pethost run db -- psql -U postgres -c 'select count(*) from recipes'
  pethost run web -- sh -c 'env | sort'
  pethost run db -i -- psql -U postgres < seed.sql`,
		Args:              cobra.ArbitraryArgs,
		ValidArgsFunction: target.CompleteServices(a),
	}

	var options runOptions
	cmd.Flags().StringVarP(&options.user, "run-as-user", "u", "", `run it as this user, not the container's; "root" can install packages`)
	cmd.Flags().StringVarP(&options.directory, "working-directory", "w", "", "run it in this directory, not the container's")
	cmd.Flags().Uint32Var(&options.timeout, "timeout-seconds", 0, "stop it after this long, not after the machine's usual time")
	cmd.Flags().BoolVarP(&options.stdin, "stdin", "i", false, "give it what is piped in as its stdin")

	cmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		// Worded as every command's wrong flag is, by the root's function.
		if parent := cmd.Parent(); parent != nil {
			err = parent.FlagErrorFunc()(cmd, err)
		}
		return ownFailure(a, err)
	})
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		service, command, err := splitAtDash(cmd, args)
		if err != nil {
			return ownFailure(a, err)
		}
		return ownFailure(a, runInService(cmd.Context(), a, service, command, options))
	}
	return cmd
}

// splitAtDash is the service named before "--", if one is, and the command after it.
func splitAtDash(cmd *cobra.Command, args []string) (service string, command []string, err error) {
	dash := cmd.ArgsLenAtDash()
	switch {
	case dash < 0:
		return "", nil, fail.Usage("pethost run: put -- before the command, as in  pethost run web -- ls /app", cmd.UseLine())
	case dash > 1:
		return "", nil, fail.Usage("pethost run takes one service before --", cmd.UseLine())
	case dash == len(args):
		return "", nil, fail.Usage("pethost run: there is no command after --", cmd.UseLine())
	}
	if dash == 1 {
		service = args[0]
	}
	return service, args[dash:], nil
}

// ownFailure is an end of run that is not the command's: it exits 125, whatever kind of failure
// it is, so that a script tells it from every code the command may exit with. Ctrl-C stays 130.
func ownFailure(a *app.App, err error) error {
	if err == nil {
		return nil
	}
	e := fail.From(err)
	if e.Exit == exitOwn || e.Exit == fail.ExitInterrupted || e.Code == commandEnded {
		return e
	}
	// How a failure is worded for people goes by its exit code: it is printed as the kind it
	// is before the code becomes run's own.
	if !a.JSON {
		fail.Print(a.UI, e, false)
		e.Shown = true
	}
	e.Exit = exitOwn
	return e
}

func runInService(ctx context.Context, a *app.App, named string, command []string, options runOptions) error {
	project, err := target.ForChange(ctx, a)
	if err != nil {
		return err
	}
	client, err := a.Client()
	if err != nil {
		return err
	}
	service, err := serviceToRunIn(ctx, a, client, project, named)
	if err != nil {
		return err
	}
	stdin, err := pipedIn(a, options)
	if err != nil {
		return err
	}

	answer, err := client.RunServiceCommand(ctx, &pethost.RunServiceCommandRequest{
		ProjectID:        project.ID,
		Service:          service,
		Command:          command,
		Stdin:            stdin,
		RunAsUser:        options.user,
		WorkingDirectory: options.directory,
		TimeoutSeconds:   options.timeout,
	})
	if err != nil {
		return refusedRun(ctx, a, client, project, service, err)
	}
	return showRun(a, project, service, answer)
}

// refusedRun is the machine's refusal of the command, with the command that starts the service
// when its container does not run. The refusal carries no reason to branch on, so the project
// is read once, for the machine's own fact of the service.
func refusedRun(ctx context.Context, a *app.App, client *pethost.Client, project target.Project, service string, refusal error) error {
	if api, ok := errors.AsType[*pethost.Error](refusal); !ok || api.Code != pethost.CodeFailedPrecondition {
		return refusal
	}
	answer, err := client.GetProject(ctx, &pethost.GetProjectRequest{ProjectID: project.ID})
	if err != nil || answer.Project == nil {
		return refusal
	}

	listed := serviceOf(answer.Project, service)
	if listed == nil || listed.ContainerRunning {
		return refusal
	}
	e := fail.From(refusal)
	e.Message = strings.TrimSuffix(e.Message, ".") + "."
	return e.WithLead("Start it:", "pethost start "+app.ShellWord(service)+project.Flag())
}

// serviceToRunIn is the service named, which the machine checks; else the project's only one,
// the person's pick, or a refusal with the command for each.
func serviceToRunIn(ctx context.Context, a *app.App, client *pethost.Client, project target.Project, named string) (string, error) {
	if named != "" {
		return named, nil
	}
	answer, err := client.GetProject(ctx, &pethost.GetProjectRequest{ProjectID: project.ID})
	if err != nil {
		return "", err
	}
	return target.Service(a, "", target.ServiceNames(answer.Project), func(service string) string {
		return a.CommandLine(service)
	})
}

// pipedIn is what -i gives the command as its stdin: all that is piped in.
func pipedIn(a *app.App, options runOptions) (string, error) {
	if !options.stdin {
		return "", nil
	}
	piped, err := io.ReadAll(a.UI.In)
	return string(piped), err
}

// commandEnded is the code of the end that is the command's own, not the CLI's failure.
const commandEnded = "COMMAND_ENDED"

// showRun writes the command's output where the command wrote it, and ends as the command did.
func showRun(a *app.App, project target.Project, service string, answer *pethost.RunServiceCommandResponse) error {
	if a.JSON {
		if err := a.PrintJSON(answer); err != nil {
			return err
		}
	} else {
		writeOutput(a.UI.Out, answer.Stdout)
		writeOutput(a.UI.Err, answer.Stderr)
		sayCut(a, project, service, answer)
	}

	switch {
	case answer.TimedOut:
		return &fail.Error{Exit: exitTimedOut, Code: commandEnded, Shown: true,
			Message: "The machine stopped the command at its timeout."}
	case answer.ExitCode != 0:
		return &fail.Error{Exit: exitCodeOf(int(answer.ExitCode)), Code: commandEnded, Shown: true,
			Message: fmt.Sprintf("The command exited with code %d.", answer.ExitCode)}
	}
	return nil
}

// writeOutput writes one of the command's streams: as it is for a program, and for a person
// without what a terminal would obey.
func writeOutput(stream *ui.Stream, output string) {
	if stream.Terminal {
		output = ui.Clean(output)
	}
	_, _ = io.WriteString(stream, output)
}

// sayCut says, on stderr, what the output does not show by itself: that its start is missing,
// and that the command was stopped.
func sayCut(a *app.App, project target.Project, service string, answer *pethost.RunServiceCommandResponse) {
	shell := a.UI.Err.Bold("pethost shell " + app.ShellWord(service) + project.Flag())
	if answer.OutputTruncated {
		a.UI.ErrLine("The start of the output was cut: the machine keeps the end. For all of it:  " + shell)
	}
	if answer.TimedOut {
		a.UI.ErrLine(a.UI.Err.Cross() + "The machine stopped the command at its timeout. Give it longer with --timeout-seconds, or run it in  " + shell)
	}
}

// exitCodeOf is a command's exit code as this process can exit with it: one byte, and never 0
// for a command that did not exit with 0.
func exitCodeOf(code int) int {
	if code < 0 || code > 255 {
		return 255
	}
	return code
}
