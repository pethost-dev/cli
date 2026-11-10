package target

import (
	"context"
	"strings"
	"time"

	pethost "github.com/pethost-dev/sdk/go"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/ui"
)

// Completion asks the machine what exists: project ids for -p, a project's service names for a
// command that takes one, and the account's workspaces for -w. A shell waits for it at each Tab,
// so it gives up soon, and any failure completes nothing.

// completionWait bounds the call a Tab makes.
const completionWait = 3 * time.Second

// CompleteProjects completes a project's id, with the name people gave it as its description.
func CompleteProjects(a *app.App) cobra.CompletionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		var ids []string
		for _, project := range projectsNow(cmd.Context(), a) {
			ids = append(ids, cobra.CompletionWithDesc(project.ProjectID, OneLine(Title(project.Metadata))))
		}
		return ids, cobra.ShellCompDirectiveNoFileComp
	}
}

// CompleteServices completes a service's name as a command's first argument: of the project
// that -p, PETHOST_PROJECT or .pethost names, else of the machine's only one. Give it to a
// command as its ValidArgsFunction.
func CompleteServices(a *app.App) cobra.CompletionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		projects := projectsNow(cmd.Context(), a)
		named, isNamed, _ := Named(a)
		var names []string
		for _, project := range projects {
			if (isNamed && project.ProjectID == named.ID) || (!isNamed && len(projects) == 1) {
				for _, service := range project.Services {
					names = append(names, service.Service)
				}
			}
		}
		return names, cobra.ShellCompDirectiveNoFileComp
	}
}

// CompleteWorkspaces completes a workspace's id, with its name as its description.
func CompleteWorkspaces(a *app.App) cobra.CompletionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if a.NeedSignIn() != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), completionWait)
		defer cancel()

		answer, err := a.Client().ListWorkspaces(ctx, nil)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		var ids []string
		for _, workspace := range answer.Workspaces {
			ids = append(ids, cobra.CompletionWithDesc(workspace.WorkspaceID, OneLine(workspace.Name)))
		}
		return ids, cobra.ShellCompDirectiveNoFileComp
	}
}

// projectsNow is the machine's projects, or none when they cannot be had in time.
func projectsNow(ctx context.Context, a *app.App) []pethost.ProjectSummary {
	machine := MachineNow(ctx, a)
	if machine == nil {
		return nil
	}
	return machine.Projects
}

// MachineNow is the machine's answer for a Tab, nil when it cannot be had in time.
func MachineNow(ctx context.Context, a *app.App) *pethost.GetMachineResponse {
	if a.NeedSignIn() != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, completionWait)
	defer cancel()

	// A Tab runs no command, so nothing has settled the workspace: the one given, else the
	// folder's.
	workspace, _ := a.GivenWorkspace()
	if project, named, _ := Named(a); workspace == "" && named && project.Source == FromFile {
		workspace = project.File.WorkspaceID
	}
	machine, err := a.Client().GetMachine(ctx, &pethost.GetMachineRequest{Workspace: workspace})
	if err != nil {
		return nil
	}
	return machine
}

// OneLine is a name as a shell's completion can hold it: cleaned, and without the tab and the
// line break that separate completions.
func OneLine(text string) string {
	return strings.Join(strings.Fields(ui.Clean(text)), " ")
}
