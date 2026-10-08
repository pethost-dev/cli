package change

import (
	pethost "github.com/pethost-dev/sdk/go"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/operation"
	"github.com/pethost-dev/cli/internal/target"
)

// startCommand is `pethost start [service...]`.
func startCommand(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start [service...]",
		Short: "Start the project's stopped services, or some of them",
		Long: `Start the project's stopped services and run its jobs again, or only the ones
named. A service that has no container yet is started by a deploy.`,
		Example: `  pethost start
  pethost start worker`,
		Args:              cobra.ArbitraryArgs,
		ValidArgsFunction: target.CompleteServices(a),
	}
	noQueue := operation.NoQueueFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		project, err := target.ForChange(cmd.Context(), a)
		if err != nil {
			return err
		}
		words := serviceWords{doing: "Starting…", done: "Started", all: "the services"}
		_, err = runServices(cmd.Context(), a, project, *noQueue, args, pethost.RunProjectActionStartServices{Services: args}, words)
		return err
	}
	return cmd
}
