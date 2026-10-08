package change

import (
	pethost "github.com/pethost-dev/sdk/go"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/operation"
	"github.com/pethost-dev/cli/internal/target"
)

// stopCommand is `pethost stop [service...]`. Nothing is asked: `pethost start` undoes it.
func stopCommand(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stop [service...]",
		Short: "Stop the project's services, or some of them",
		Long: `Stop the project's services, or the ones named, each with its grace period.
They stay stopped until "pethost start" or the next deploy; a restart of the
machine may start them, as their restart policies say.`,
		Example: `  pethost stop
  pethost stop worker`,
		Args:              cobra.ArbitraryArgs,
		ValidArgsFunction: target.CompleteServices(a),
	}
	noQueue := operation.NoQueueFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		project, err := target.ForChange(cmd.Context(), a)
		if err != nil {
			return err
		}
		words := serviceWords{doing: "Stopping…", done: "Stopped", all: "the services"}
		if _, err := runServices(cmd.Context(), a, project, *noQueue, args, pethost.RunProjectActionStopServices{Services: args}, words); err != nil {
			return err
		}
		a.UI.Info("  Start again:  %s", a.UI.Err.Bold(again("pethost start", args, project)))
		return nil
	}
	return cmd
}
