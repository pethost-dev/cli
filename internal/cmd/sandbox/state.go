package sandbox

import (
	"cmp"
	"context"
	"fmt"
	"strings"

	"github.com/pethost-dev/sdk/go/sandboxes"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/ui"
)

// The commands that change a sandbox's state: pause, resume, stop and rm. Each takes one id or
// several, calls for all at once, and writes each sandbox with its new state, in the order given.

// change is one of them: what it calls with a sandbox's id.
type change func(ctx context.Context, c *sandboxes.Client, id string) (*sandboxes.Sandbox, error)

func pauseCommand(a *app.App) *cobra.Command {
	return stateCommand(a, &cobra.Command{
		Use:   "pause <id>...",
		Short: "Pause sandboxes: memory, processes and disk kept, woken in a moment",
		Long: `Pauses sandboxes: their memory, processes and disk are kept on their server, which keeps
their place, and they wake at the next command, SSH login or request to a port. A paused
sandbox costs its paused price. One that is paused or stopped already stays as it is.`,
		Example: `  pethost sandbox pause k7f3q2xy0a1b2c3d
  pethost sandbox pause $(pethost sandbox ls --state running -q | cut -f1)`,
	}, func(ctx context.Context, c *sandboxes.Client, id string) (*sandboxes.Sandbox, error) {
		return c.PauseSandbox(ctx, &sandboxes.PauseSandboxRequest{SandboxID: id})
	})
}

func resumeCommand(a *app.App) *cobra.Command {
	return stateCommand(a, &cobra.Command{
		Use:     "resume <id>...",
		Aliases: []string{"start"},
		Short:   "Resume paused or stopped sandboxes",
		Long: `Resumes sandboxes without running anything in them. A paused one goes on where it
paused; a stopped one starts again, its files as it left them, its disk first fetched
from storage when it is not on its server.`,
		Example: `  pethost sandbox resume k7f3q2xy0a1b2c3d`,
	}, func(ctx context.Context, c *sandboxes.Client, id string) (*sandboxes.Sandbox, error) {
		return c.ResumeSandbox(ctx, &sandboxes.ResumeSandboxRequest{SandboxID: id})
	})
}

func stopCommand(a *app.App) *cobra.Command {
	return stateCommand(a, &cobra.Command{
		Use:   "stop <id>...",
		Short: "Stop sandboxes: their processes end, their disk is kept",
		Long: `Stops sandboxes: their processes are asked to end and killed 5 s later, and their disk
is kept in storage, where it costs least. Resume starts them again.`,
		Example: `  pethost sandbox stop k7f3q2xy0a1b2c3d`,
	}, func(ctx context.Context, c *sandboxes.Client, id string) (*sandboxes.Sandbox, error) {
		return c.StopSandbox(ctx, &sandboxes.StopSandboxRequest{SandboxID: id})
	})
}

func rmCommand(a *app.App) *cobra.Command {
	remove := func(ctx context.Context, c *sandboxes.Client, id string) (*sandboxes.Sandbox, error) {
		return c.DeleteSandbox(ctx, &sandboxes.DeleteSandboxRequest{SandboxID: id})
	}
	cmd := stateCommand(a, &cobra.Command{
		Use:     "rm <id>...",
		Aliases: []string{"delete"},
		Short:   "Delete sandboxes, their memory and disk for good",
		Long: `Deletes sandboxes: their memory and disk are gone for good. It asks first, naming each
sandbox by its name and id, and needs --yes where nobody can be asked.`,
		Example: `  pethost sandbox rm k7f3q2xy0a1b2c3d
  pethost sandbox rm k7f3q2xy0a1b2c3d k9a8b7c6d5e4f3g2 --yes`,
	}, remove)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		c, err := clientForChange(a)
		if err != nil {
			return err
		}
		doomed, err := sandboxesOf(cmd.Context(), c, args)
		if err != nil {
			return err
		}
		if yes, err := a.UI.Confirm(deleteQuestion(doomed), a.CommandLine("--yes")); err != nil || !yes {
			return declined(err, "Nothing was deleted.")
		}

		// The sandboxes asked about are the ones deleted, by their ids: a name another sandbox
		// took meanwhile changes nothing of them.
		ids := make([]string, len(doomed))
		for i, sandbox := range doomed {
			ids[i] = sandbox.SandboxID
		}
		return changeEach(cmd.Context(), a, c, ids, remove)
	}
	return cmd
}

// deleteQuestion asks whether to delete the sandboxes, each named as a person reads it.
func deleteQuestion(doomed []*sandboxes.Sandbox) string {
	words := make([]string, len(doomed))
	for i, sandbox := range doomed {
		words[i] = nameWords(sandbox)
	}
	if len(doomed) == 1 {
		return fmt.Sprintf("Delete %s for good, its memory and disk?", words[0])
	}
	return fmt.Sprintf("Delete these %d sandboxes for good, their memory and disk: %s?", len(doomed), strings.Join(words, ", "))
}

// stateCommand gives a command of a change its arguments and its run.
func stateCommand(a *app.App, cmd *cobra.Command, call change) *cobra.Command {
	cmd.Args = cobra.MinimumNArgs(1)
	cmd.ValidArgsFunction = completeEachSandbox(a)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		c, err := clientForChange(a)
		if err != nil {
			return err
		}
		return changeEach(cmd.Context(), a, c, args, call)
	}
	return cmd
}

// changeEach calls for every sandbox at once, then writes each one that changed, in the order
// given, and ends as the first that failed.
func changeEach(ctx context.Context, a *app.App, c *sandboxes.Client, ids []string, call change) error {
	answers := make([]*sandboxes.Sandbox, len(ids))
	errs := make([]error, len(ids))
	calls := make([]func() error, len(ids))
	for i, typed := range ids {
		calls[i] = func() error {
			answers[i], errs[i] = call(ctx, c, idOf(ctx, c, typed))
			return nil
		}
	}
	_ = app.Together(calls...)

	var first error
	table := a.UI.Out.Table()
	for i, answer := range answers {
		if errs[i] != nil {
			first = cmp.Or(first, failedOf(a, ids, i, errs[i]))
			continue
		}
		if a.JSON {
			if err := a.PrintJSON(answer); err != nil {
				return err
			}
			continue
		}
		table.Row(a.UI.Out.Bold(ui.Clean(answer.SandboxID)), stateWord(a.UI.Out, answer.State))
	}
	table.Flush()
	return first
}

// failedOf is the failure of the change of ids[i]. Of several ids each failure is said at once,
// beside its id, and the command ends as the first did.
func failedOf(a *app.App, ids []string, i int, err error) error {
	err = about(ids[i], err)
	if len(ids) == 1 {
		return err
	}
	e := fail.From(err)
	if !a.JSON {
		a.UI.ErrLine(a.UI.Err.Cross() + ui.Clean(ids[i]) + ": " + e.Message)
		e.Shown = true
	}
	return e
}

// cleaned are ids as a person reads them.
func cleaned(ids []string) []string {
	words := make([]string, len(ids))
	for i, id := range ids {
		words[i] = ui.Clean(id)
	}
	return words
}

// declined is the end of a command whose question was answered no, or could not be asked.
func declined(err error, message string) error {
	if err != nil {
		return err
	}
	return fail.Declined(message)
}
