package workspace

import (
	"context"
	"slices"
	"strings"

	pethost "github.com/pethost-dev/sdk/go"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/say"
	"github.com/pethost-dev/cli/internal/target"
	"github.com/pethost-dev/cli/internal/ui"
)

// listCommand is `pethost workspaces`: every workspace the sign-in reaches, as ListWorkspaces
// answers, a row each.
func listCommand(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "workspaces",
		Short: "Your workspaces: each plan's machine, its size and what is free",
		Long: `Every workspace you can act in, by name: its id, its size, what its projects leave
free of memory and disk, where it runs and how many projects it has. Each plan you pay
for is a workspace, with a machine of the plan's size.

A command acts in the workspace that -w names, by its id or name, else PETHOST_WORKSPACE,
else the one the folder's .pethost names, else your only one. With -p, the list also
says which workspaces have a project of that id.`,
		Example: `  pethost workspaces
  pethost workspaces -p recipes
  pethost workspaces --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return list(cmd.Context(), a)
		},
	}
}

func list(ctx context.Context, a *app.App) error {
	if err := a.NeedSignIn(); err != nil {
		return err
	}
	answer, err := allWorkspaces(ctx, a)
	if err != nil {
		return err
	}

	if a.JSON {
		return a.PrintJSON(answer)
	}
	printWorkspaces(a, answer)
	return nil
}

// allWorkspaces is every page of ListWorkspaces as one answer: the pages' workspaces in their
// order, and the rest of the last page's.
func allWorkspaces(ctx context.Context, a *app.App) (*pethost.ListWorkspacesResponse, error) {
	client := a.Client()
	request := &pethost.ListWorkspacesRequest{ProjectID: a.Project}
	all := &pethost.ListWorkspacesResponse{}
	for {
		page, err := client.ListWorkspaces(ctx, request)
		if err != nil {
			return nil, err
		}

		workspaces := append(all.Workspaces, page.Workspaces...)
		incomplete := all.Incomplete || page.Incomplete
		*all = *page
		all.Workspaces, all.Incomplete = workspaces, incomplete
		if page.NextPageToken == "" || len(page.Workspaces) == 0 {
			all.NextPageToken = ""
			return all, nil
		}
		request.PageToken = page.NextPageToken
	}
}

// printWorkspaces writes a row for each workspace, then what the rows could not say and what
// to type next.
func printWorkspaces(a *app.App, answer *pethost.ListWorkspacesResponse) {
	if len(answer.Workspaces) == 0 {
		a.UI.Info("No workspace yet: a plan you choose is one. In your app's folder, run:   %s", a.UI.Err.Bold("pethost deploy"))
		return
	}

	out := a.UI.Out
	here := folderWorkspace()
	table := out.Table()
	for _, workspace := range answer.Workspaces {
		table.Row(workspaceCells(out, answer, &workspace, here)...)
	}
	table.Flush()

	if answer.Incomplete {
		a.UI.Info("A machine did not answer in time: what it has free is not known now.")
	}
	if a.Project != "" && len(answer.HoldingProject) == 0 {
		a.UI.Info("None of them has a project %s.", a.UI.Err.Bold(ui.Clean(a.Project)))
	}
	a.UI.Next("pethost ls -w " + app.ShellWord(answer.Workspaces[0].WorkspaceID))
}

// workspaceCells are a workspace's row: its name and id, its size, what is free, where it runs,
// its projects, and a note of what ties it to this folder or to -p's project.
func workspaceCells(out *ui.Stream, answer *pethost.ListWorkspacesResponse, workspace *pethost.Workspace, here string) []string {
	var notes []string
	if workspace.WorkspaceID == here {
		notes = append(notes, "this folder's")
	}
	if slices.Contains(answer.HoldingProject, workspace.WorkspaceID) {
		notes = append(notes, "has the project")
	}
	if !workspace.EndTime.IsZero() {
		notes = append(notes, out.Yellow("ends "+out.When(workspace.EndTime)))
	}

	return []string{
		out.Bold(ui.Clean(workspace.Name)),
		ui.Clean(workspace.WorkspaceID),
		say.WorkspaceSize(workspace),
		say.WorkspaceFree(workspace),
		ui.Clean(workspace.Location),
		say.WorkspaceProjects(workspace),
		strings.Join(notes, ", "),
	}
}

// folderWorkspace is the workspace this folder's .pethost names; empty when there is none.
func folderWorkspace() string {
	file, err := target.FindFile(".")
	if err != nil || file == nil {
		return ""
	}
	return file.WorkspaceID
}
