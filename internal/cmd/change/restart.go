package change

import (
	"context"
	"slices"

	pethost "github.com/pethost-dev/sdk/go"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/operation"
	"github.com/pethost-dev/cli/internal/target"
	"github.com/pethost-dev/cli/internal/ui"
)

// restartCommand is `pethost restart [service...]`: the processes again in the same
// containers. --pull gives each service a new container on its image's newest version, which
// is an operation of its own for each.
func restartCommand(a *app.App) *cobra.Command {
	var pull bool
	cmd := &cobra.Command{
		Use:   "restart [service...]",
		Short: "Restart the project's services, or some of them",
		Long: `Restart the project's running services, or the ones named: the same containers,
their processes started again.

  --pull   gives each a new container instead, on the newest image of its tag, or
           built again on the newest base images. What a container wrote outside
           its volumes is then gone.`,
		Example: `  pethost restart
  pethost restart web
  pethost restart web --pull`,
		Args:              cobra.ArbitraryArgs,
		ValidArgsFunction: target.CompleteServices(a),
	}
	cmd.Flags().BoolVar(&pull, "pull", false, "start again on the image's newest version, in a new container")
	noQueue := operation.NoQueueFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		project, err := target.ForChange(cmd.Context(), a)
		if err != nil {
			return err
		}
		if pull {
			return recreate(cmd.Context(), a, project, *noQueue, args)
		}
		words := serviceWords{doing: "Restarting…", done: "Restarted", all: "the running services"}
		after, err := runServices(cmd.Context(), a, project, *noQueue, args, pethost.RunProjectActionRestartServices{Services: args}, words)
		if err != nil {
			return err
		}
		if len(args) == 0 {
			sayStillStopped(a, project, after)
		}
		return nil
	}
	return cmd
}

// sayStillStopped names, after a restart of the running services, the command that starts the
// ones the machine reports stopped: a restart leaves them as they are.
func sayStillStopped(a *app.App, project target.Project, after *pethost.Project) {
	if after == nil {
		return
	}
	var stopped []string
	for _, service := range after.Services {
		if service.State == pethost.ServiceStateStopped {
			stopped = append(stopped, service.Service)
		}
	}
	if len(stopped) > 0 {
		a.UI.Info("  A stopped service stays stopped. Start it:  %s", a.UI.Err.Bold(startAgain(stopped, project)))
	}
}

// recreate gives each service named a new container on its image's newest version, one after
// another; with none named, each service of the project.
func recreate(ctx context.Context, a *app.App, project target.Project, noQueue bool, services []string) error {
	if len(services) == 0 {
		client, err := a.Client()
		if err != nil {
			return err
		}
		answer, err := client.GetProject(ctx, &pethost.GetProjectRequest{ProjectID: project.ID})
		if err != nil {
			return err
		}
		services = target.ServiceNames(answer.Project)
	}

	var last *outcome
	for _, service := range services {
		name := ui.Clean(service)
		done, err := operate(ctx, a, project, noQueue,
			pethost.RunProjectActionRecreateService{Service: service, PullLatestImage: true},
			operation.Words{Doing: "Giving " + name + " a new container…", Failed: name + " got no new container"})
		if err != nil {
			return err
		}
		a.UI.Done("%s, in %s.", recreated(done.Project, service), done.took())
		done.Notes(a)
		last = done
	}
	if last == nil {
		return nil
	}
	if a.JSON {
		return a.PrintJSON(last.answer)
	}
	last.Problems(a)
	return nil
}

// recreated says what a recreation did, by whether the machine builds the service's image.
func recreated(project *pethost.Project, service string) string {
	name := ui.Clean(service)
	builds := project != nil && slices.ContainsFunc(project.Services, func(candidate pethost.Service) bool {
		return candidate.Service == service && candidate.BuildsImage
	})
	if builds {
		return name + " was built again on the newest base images and runs in a new container"
	}
	return name + " runs in a new container, on the newest image of its tag"
}
