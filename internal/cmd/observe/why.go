package observe

import (
	"context"

	pethost "github.com/pethost-dev/sdk/go"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/target"
)

// whyCommand is `pethost why`.
func whyCommand(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "why",
		Short: "What is wrong or slow: problems, failing paths, the log lines near them",
		Long: `One screen from three facts the machine keeps: the project's problems, as the machine words
them; the paths whose requests failed (answered 5xx) in the last 24 hours; and what the service
wrote at that moment. It says what it saw, and never guesses a cause.

With --json it prints the three answers the screen is made from, one a line: the project
(GetProject), the day's traffic and the day's failed requests (QueryHttpTraffic).`,
		Example: `  pethost why
  pethost why -p recipes`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWhy(cmd.Context(), a)
		},
	}
}

// why is one run of `pethost why`: what the machine answered, then what stands near it.
type why struct {
	ctx     context.Context
	a       *app.App
	client  *pethost.Client
	project target.Project

	// The project as it is: its problems, its services, its newest deploy.
	answer *pethost.GetProjectResponse
	// Every request of the last day: the totals and the busiest paths.
	day *pethost.QueryHTTPTrafficResponse
	// Those of them that failed: the paths that fail most, and the latest failures.
	failed *pethost.QueryHTTPTrafficResponse

	problems []problemSeen
	paths    []pathSeen
}

func runWhy(ctx context.Context, a *app.App) error {
	project, err := target.ForRead(ctx, a)
	if err != nil {
		return err
	}
	client, err := a.Client()
	if err != nil {
		return err
	}

	w := &why{ctx: ctx, a: a, client: client, project: project}
	if err := w.ask(); err != nil {
		return err
	}
	if a.JSON {
		return w.printJSON()
	}
	if err := w.lookNear(); err != nil {
		return err
	}
	w.print()
	return nil
}

// failedLatest is how many of the latest failed requests are asked for: the most the API
// gives, so that each path that fails has its newest failure among them.
const failedLatest = 200

// ask makes the three calls the screen starts from, at once: none depends on another.
func (w *why) ask() error {
	id := w.project.ID
	return app.Together(
		func() (err error) {
			w.answer, err = w.client.GetProject(w.ctx, &pethost.GetProjectRequest{ProjectID: id})
			return err
		},
		func() (err error) {
			w.day, err = w.client.QueryHTTPTraffic(w.ctx, &pethost.QueryHTTPTrafficRequest{ProjectID: id})
			return err
		},
		func() (err error) {
			w.failed, err = w.client.QueryHTTPTraffic(w.ctx, &pethost.QueryHTTPTrafficRequest{
				ProjectID: id, Filter: &pethost.HTTPTrafficFilter{StatusClass: serverErrors}, RequestLimit: failedLatest,
			})
			return err
		},
	)
}

// serverErrors is the class of statuses the machine counts as failed: 5xx.
const serverErrors = 5

// printJSON writes the three answers, one a line: the project, the day's traffic, the failed
// requests.
func (w *why) printJSON() error {
	for _, answer := range []any{w.answer, w.day, w.failed} {
		if err := w.a.PrintJSON(answer); err != nil {
			return err
		}
	}
	return nil
}
