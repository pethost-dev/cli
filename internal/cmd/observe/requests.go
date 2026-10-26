package observe

import (
	"context"
	"fmt"
	"iter"

	pethost "github.com/pethost-dev/sdk/go"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/stream"
	"github.com/pethost-dev/cli/internal/target"
)

// requestFlags are what `pethost requests` was asked for.
type requestFlags struct {
	follow bool
	status string
	path   string
	since  string
}

// requestsCommand is `pethost requests`.
func requestsCommand(a *app.App) *cobra.Command {
	var flags requestFlags
	cmd := &cobra.Command{
		Use:   "requests",
		Short: "HTTP traffic: totals, busiest paths, the latest requests",
		Long: `The requests that reached the project's addresses: how many in the last 24 hours and how
many of them failed (answered 5xx), the busiest paths, and the latest requests, one a line:
when it finished, its status, what was asked, how long it took, the service that answered and
who asked. -f shows only the requests, and goes on with each new one until Ctrl-C.`,
		Example: `  pethost requests
  pethost requests -f
  pethost requests --status 5xx
  pethost requests --path /api --since 1h`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRequests(cmd, a, flags)
		},
	}
	cmd.Flags().BoolVarP(&flags.follow, "follow", "f", false, "go on printing new requests as they finish")
	cmd.Flags().StringVar(&flags.status, "status", "", "only one class of status, 5xx, or one status, 404")
	cmd.Flags().StringVar(&flags.path, "path", "", "only paths that contain this text")
	cmd.Flags().StringVar(&flags.since, "since", "", sinceHelp+" (24h)")
	return cmd
}

// requests is one run of `pethost requests`.
type requests struct {
	ctx     context.Context
	a       *app.App
	client  *pethost.Client
	project target.Project
	flags   requestFlags
	since   since
	status  statusFilter
	printer *requestPrinter
}

func runRequests(cmd *cobra.Command, a *app.App, flags requestFlags) error {
	ctx := cmd.Context()
	from, err := sinceOf(cmd, flags.since)
	if err != nil {
		return err
	}
	status, err := statusOf(cmd, flags.status)
	if err != nil {
		return err
	}
	project, err := target.ForRead(ctx, a)
	if err != nil {
		return err
	}
	client, err := a.Client()
	if err != nil {
		return err
	}

	r := &requests{ctx: ctx, a: a, client: client, project: project, flags: flags, since: from, status: status, printer: &requestPrinter{out: a.UI.Out}}
	sequence, err := r.past()
	if err != nil || !flags.follow {
		return err
	}
	return r.follow(sequence)
}

// filter is which requests the machine is asked for, in the query and in the stream alike.
func (r *requests) filter() *pethost.HTTPTrafficFilter {
	return &pethost.HTTPTrafficFilter{PathContains: r.flags.path, StatusClass: r.status.class}
}

// latestRequests is how many of the latest requests are shown.
const latestRequests = 20

// past prints what the machine has logged: the totals, the busiest paths and the latest
// requests, or under -f the latest requests alone. It returns where a stream goes on from.
func (r *requests) past() (sequence uint64, err error) {
	request := &pethost.QueryHTTPTrafficRequest{
		ProjectID:    r.project.ID,
		Filter:       r.filter(),
		StartTime:    r.since.start,
		LastSeconds:  r.since.lastSeconds,
		RequestLimit: r.status.pageSize(latestRequests),
	}
	answer, err := r.client.QueryHTTPTraffic(r.ctx, request)
	if err != nil {
		return 0, err
	}

	// The stream goes on from the first page, whatever older pages are read after it.
	sequence = answer.TailSequence
	looked, all, err := r.status.pick(r.ctx, r.client, request, answer, latestRequests)
	if err != nil {
		return 0, err
	}
	if r.a.JSON {
		return sequence, r.a.PrintJSON(answer)
	}

	summarised := !r.flags.follow && !r.status.exact()
	if summarised {
		r.printTotals(answer)
		r.printPaths(answer.TopPaths)
	}
	r.printLatest(answer, summarised, looked, all)
	if r.status.exact() && !r.flags.follow {
		byClass := fmt.Sprintf("pethost requests --status %dxx%s", r.status.class, r.project.Flag())
		r.a.UI.Info("The machine counts by class of status. Totals and busiest paths:  %s", r.a.UI.Err.Bold(byClass))
	}
	return sequence, nil
}

// follow prints each new request as it finishes, from the one after sequence on, until the
// person leaves. A stream that breaks goes on after the last request it brought.
func (r *requests) follow(sequence uint64) error {
	open := func() iter.Seq2[*pethost.TailHTTPTrafficResponse, error] {
		return r.client.TailHTTPTraffic(r.ctx, &pethost.TailHTTPTrafficRequest{ProjectID: r.project.ID, Filter: r.filter(), AfterSequence: sequence})
	}
	return stream.Follow(r.ctx, r.a, open, func(response *pethost.TailHTTPTrafficResponse) error {
		request := response.Request
		if request == nil {
			return nil
		}
		sequence = request.Sequence
		switch {
		case !r.status.takes(*request):
			return nil
		case r.a.JSON:
			return r.a.PrintJSON(response)
		}
		r.printer.row(*request)
		return nil
	})
}
