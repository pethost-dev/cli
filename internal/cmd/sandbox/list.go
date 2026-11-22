package sandbox

import (
	"context"

	"github.com/pethost-dev/sdk/go/sandboxes"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/ui"
)

// listCommand is `pethost sandbox ls`, which the bare `pethost sandbox` runs too.
func listCommand(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list"},
		Short:   "Your sandboxes, newest first",
		Long: `Lists the sandboxes of the workspace, newest first: each one's id, name, state, size,
the template it started from, when it was made and its labels. Without --state every
sandbox but the deleted ones is listed; --label lists only those with every label given.`,
		Example: `  pethost sandbox ls
  pethost sandbox ls --state running,paused
  pethost sandbox ls --label user=42 --json`,
		Args: cobra.NoArgs,
	}
	var stateWords, labels []string
	cmd.Flags().StringSliceVar(&stateWords, "state", nil, "only sandboxes in these states: running, paused, stopped, deleted")
	cmd.Flags().StringArrayVar(&labels, "label", nil, "only sandboxes with this label, key=value (again for more)")

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		request := &sandboxes.ListSandboxesRequest{Labels: labelsOf(labels), PageSize: 500}
		for _, typed := range stateWords {
			state, err := fromWord("state", typed, states...)
			if err != nil {
				return err
			}
			request.States = append(request.States, state)
		}
		return list(cmd.Context(), a, request)
	}
	return cmd
}

func list(ctx context.Context, a *app.App, request *sandboxes.ListSandboxesRequest) error {
	c, err := client(a)
	if err != nil {
		return err
	}
	all, err := allSandboxes(ctx, c, request)
	if err != nil {
		return err
	}

	if a.JSON {
		return a.PrintJSON(all)
	}
	switch {
	case len(all.Sandboxes) == 0 && (len(request.States) > 0 || len(request.Labels) > 0):
		a.UI.Info("No sandbox matches --state and --label.")
		return nil
	case len(all.Sandboxes) == 0:
		a.UI.Info("No sandbox here yet. Make one:  %s", a.UI.Err.Bold("pethost sandbox new"))
		return nil
	}
	printSandboxes(a, all.Sandboxes)
	return nil
}

// allSandboxes is every page of ListSandboxes as one answer.
func allSandboxes(ctx context.Context, c *sandboxes.Client, request *sandboxes.ListSandboxesRequest) (*sandboxes.ListSandboxesResponse, error) {
	all := &sandboxes.ListSandboxesResponse{}
	for {
		page, err := c.ListSandboxes(ctx, request)
		if err != nil {
			return nil, err
		}
		all.Sandboxes = append(all.Sandboxes, page.Sandboxes...)
		if page.NextPageToken == "" || len(page.Sandboxes) == 0 {
			return all, nil
		}
		request.PageToken = page.NextPageToken
	}
}

// printSandboxes writes a row a sandbox, under a heading on a terminal: off one the rows alone,
// their cells split by tabs.
func printSandboxes(a *app.App, list []sandboxes.Sandbox) {
	out := a.UI.Out
	table := out.Table()
	if out.Terminal {
		table.Row(out.Dim("ID"), out.Dim("NAME"), out.Dim("STATE"), out.Dim("SIZE"), out.Dim("TEMPLATE"), out.Dim("CREATED"), out.Dim("LABELS"))
	}
	for i := range list {
		sandbox := &list[i]
		table.Row(
			out.Bold(ui.Clean(sandbox.SandboxID)), ui.Clean(sandbox.Name), stateWord(out, sandbox.State), word(sizeOf(sandbox)),
			ui.Clean(sandbox.Template), out.Ago(sandbox.CreateTime), labelsWords(sandbox.Labels),
		)
	}
	table.Flush()
}
