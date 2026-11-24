package sandbox

import (
	"errors"
	"fmt"

	"github.com/pethost-dev/sdk/go/sandboxes"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/fail"
)

// setCommand is `pethost sandbox set <id>`.
func setCommand(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set <id>",
		Short: "Change a sandbox's name, size, lifecycle, egress or environment",
		Long: `Changes the sandbox's settings: each flag its own field or list, the rest as it is.
Egress changes at once, the environment for what starts after, and a new size restarts a
running sandbox at it (a paused one is refused: resume or stop it first). --env replaces
the whole environment, --allow and --deny each their list. --name gives it a name, unique
in the workspace, and --name "" takes its name away.

It reads the sandbox, changes what the flags say and sends it back only if nothing else
changed it meanwhile. A new name goes first, in a call of its own, as the API renames: a
name another sandbox has changes nothing. The answer is the settings' new revision.`,
		Example: `  pethost sandbox set k7f3q2xy0a1b2c3d --name api-dev
  pethost sandbox set api-dev --size large --grow-disk
  pethost sandbox set k7f3q2xy0a1b2c3d --pause-after 0 --stop-after 2h
  pethost sandbox set k7f3q2xy0a1b2c3d --allow pypi.org --allow files.pythonhosted.org
  pethost sandbox set k7f3q2xy0a1b2c3d --env DEBUG=1 --env OPENAI_API_KEY=secret:openai`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeSandboxes(a),
	}
	settings := addSettingsFlags(cmd.Flags())
	var growDisk bool
	var name string
	cmd.Flags().BoolVar(&growDisk, "grow-disk", false, "grow its disk to its size's, which never shrinks again")
	cmd.Flags().StringVar(&name, "name", "", `a name to call it by, unique in the workspace; "" takes it away`)

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		c, err := clientForChange(a)
		if err != nil {
			return err
		}
		id, err := idOf(ctx, c, args[0])
		if err != nil {
			return err
		}
		sandbox, err := c.GetSandbox(ctx, &sandboxes.GetSandboxRequest{SandboxID: id})
		if err != nil {
			return about(id, err)
		}

		changed := &sandboxes.SandboxSettings{}
		if err := settings.apply(changed, sandbox.Settings); err != nil {
			return err
		}
		renamed := cmd.Flags().Changed("name")
		reset := *changed != (sandboxes.SandboxSettings{}) || growDisk
		if !reset && !renamed {
			return fail.Usage("pethost sandbox set: name what to change, as --size large.", cmd.UseLine())
		}

		// A rename changes no revision, so the rest is sent at the one read.
		revision := sandbox.Revision
		if renamed {
			if sandbox, err = c.UpdateSandbox(ctx, &sandboxes.UpdateSandboxRequest{SandboxID: id, Name: &name}); err != nil {
				return about(id, err)
			}
		}
		if reset {
			request := &sandboxes.UpdateSandboxRequest{SandboxID: id, Settings: changed, IfRevision: revision, GrowDisk: growDisk}
			next, err := c.UpdateSandbox(ctx, request)
			if err != nil {
				if renamed {
					a.UI.Done("Renamed %s.", nameWords(sandbox))
				}
				return changedMeanwhile(id, err)
			}
			sandbox = next
		}

		if a.JSON {
			return a.PrintJSON(sandbox)
		}
		a.UI.Done("Changed %s: %s, %s.", nameWords(sandbox), word(sizeOf(sandbox)), word(sandbox.State))
		_, err = fmt.Fprintln(a.UI.Out, sandbox.Revision)
		return err
	}
	return cmd
}

// changedMeanwhile is a refused change of the settings, where the sandbox changed between the
// read and the change: the command, run again, reads it anew. A new name sent before stays.
func changedMeanwhile(id string, err error) error {
	api, ok := errors.AsType[*sandboxes.Error](err)
	if !ok || api.Code != sandboxes.CodeAborted {
		return about(id, err)
	}
	e := fail.From(api)
	e.Message = "The sandbox changed meanwhile, and its settings were not changed: run the command again."
	return e
}
