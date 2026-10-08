package change

import (
	"context"
	"fmt"

	pethost "github.com/pethost-dev/sdk/go"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/operation"
	"github.com/pethost-dev/cli/internal/target"
	"github.com/pethost-dev/cli/internal/ui"
)

// domainRmCommand is `pethost domain rm <host>`: the project's routes as they are, less the
// host's. Nothing is asked: one `domain add` brings the address back, and the command says
// which.
func domainRmCommand(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "rm <host>",
		Short:   "Take an address away",
		Example: `  pethost domain rm recipes.example.com`,
		Args:    cobra.ExactArgs(1),
	}
	noQueue := operation.NoQueueFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		project, err := target.ForChange(cmd.Context(), a)
		if err != nil {
			return err
		}
		return removeDomain(cmd.Context(), a, project, *noQueue, args[0])
	}
	return cmd
}

func removeDomain(ctx context.Context, a *app.App, project target.Project, noQueue bool, host string) error {
	client, err := a.Client()
	if err != nil {
		return err
	}

	var removed []pethost.Route
	var done *outcome
	err = againIfChanged(func() error {
		answer, err := client.GetProject(ctx, &pethost.GetProjectRequest{ProjectID: project.ID})
		if err != nil {
			return err
		}

		var kept []pethost.Route
		kept, removed = withoutHost(answer.Project.Routes, host)
		if len(removed) == 0 {
			return fail.Failed("%s is not an address of %s.", ui.Clean(host), ui.Clean(project.ID)).
				WithLead("Its addresses:", "pethost domain"+project.Flag())
		}

		// Routes replace all the project has; none at all has a word of its own.
		change := &pethost.ProjectExtension{Routes: kept}
		if len(kept) == 0 {
			change = &pethost.ProjectExtension{RemoveRoutes: true}
		}
		done, err = deploy(ctx, a, project, noQueue, &pethost.DeployProjectRequest{
			BaseDeployID: answer.Project.DeployID,
			XPethost:     change,
		}, routeWords)
		return err
	})
	if err != nil {
		return err
	}

	a.UI.Done("%s is no longer an address of %s.", ui.Clean(removed[0].Host), ui.Clean(project.ID))
	if done.Project != nil && len(done.Project.Routes) == 0 {
		a.UI.Info("  It has no address now.")
	}
	if len(removed) == 1 && everyPath(removed[0]) && !removed[0].StripPath {
		back := fmt.Sprintf("pethost domain add %s %s:%d%s", word(removed[0].Host), word(removed[0].Service), removed[0].Port, project.Flag())
		a.UI.Info("  To bring it back:  %s", a.UI.Err.Bold(back))
	}
	return done.said(a)
}

// withoutHost splits the project's routes into those that stay and the host's, which go.
func withoutHost(routes []pethost.Route, host string) (kept, removed []pethost.Route) {
	for _, route := range routes {
		if sameHost(route, host) {
			removed = append(removed, route)
		} else {
			kept = append(kept, route)
		}
	}
	return kept, removed
}
