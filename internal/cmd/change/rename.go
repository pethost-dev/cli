package change

import (
	pethost "github.com/pethost-dev/sdk/go"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/operation"
	"github.com/pethost-dev/cli/internal/target"
	"github.com/pethost-dev/cli/internal/ui"
)

// renameCommand is `pethost rename <name>`: the name people see. The id, which is the address
// and what -p takes, never changes.
func renameCommand(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rename <name>",
		Short: "Change the name people see",
		Long: `Change the name people see in the panel and in lists. The project's id, which is
its address and what -p takes, never changes. An empty name removes it: people then
see the id.`,
		Example: `  pethost rename 'Recipe Box'
  pethost rename ''                 no name: people see the id`,
		Args: cobra.ExactArgs(1),
	}
	noQueue := operation.NoQueueFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		project, err := target.ForChange(cmd.Context(), a)
		if err != nil {
			return err
		}

		done, err := deploy(cmd.Context(), a, project, *noQueue, &pethost.DeployProjectRequest{
			XPethost: &pethost.ProjectExtension{Metadata: &pethost.ProjectMetadata{Name: &args[0]}},
		}, operation.Words{Doing: "Renaming…", Failed: "It was not renamed"})
		if err != nil {
			return err
		}

		if args[0] == "" {
			a.UI.Done("%s has no name now: people see its id.", ui.Clean(project.ID))
		} else {
			a.UI.Done("%s is called %s now.", ui.Clean(project.ID), a.UI.Err.Bold(ui.Clean(args[0])))
		}
		return done.said(a)
	}
	return cmd
}
