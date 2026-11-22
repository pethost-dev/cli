package sandbox

import (
	"context"
	"errors"
	"time"

	"github.com/pethost-dev/sdk/go/sandboxes"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/target"
)

// Completion asks the API what exists: the sandboxes of the workspace for a command's id, by id
// and by name, and the workspaces of sandboxes for -w under `pethost sandbox`. A shell waits for
// it at each Tab, so it gives up soon, and any failure completes nothing.

// completionWait bounds the call a Tab makes.
const completionWait = 3 * time.Second

// completeSandboxes completes a sandbox's id or name, as a command's first argument, with its
// state and template as its description.
func completeSandboxes(a *app.App) cobra.CompletionFunc {
	return completeIDs(a, false)
}

// completeEachSandbox completes a sandbox's id as every argument of a command that takes several.
func completeEachSandbox(a *app.App) cobra.CompletionFunc {
	return completeIDs(a, true)
}

func completeIDs(a *app.App, several bool) cobra.CompletionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 && !several {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		if a.NeedSignIn() != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), completionWait)
		defer cancel()

		answer, err := a.Sandboxes().ListSandboxes(ctx, &sandboxes.ListSandboxesRequest{PageSize: 100})
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		var ids []string
		for _, sandbox := range answer.Sandboxes {
			about := target.OneLine(word(sandbox.State) + " · " + sandbox.Template)
			ids = append(ids, cobra.CompletionWithDesc(sandbox.SandboxID, about))
			if sandbox.Name != "" {
				ids = append(ids, cobra.CompletionWithDesc(sandbox.Name, target.OneLine(sandbox.SandboxID)+" · "+about))
			}
		}
		return ids, cobra.ShellCompDirectiveNoFileComp
	}
}

// CompleteWorkspaces completes -w under `pethost sandbox`: the workspaces of sandboxes, which a
// call that names none is refused with when there are several, each with its name; the only
// one, when there is one.
func CompleteWorkspaces(a *app.App) cobra.CompletionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if a.NeedSignIn() != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), completionWait)
		defer cancel()

		var listed []sandboxes.Workspace
		workspace, err := a.Sandboxes().GetWorkspace(ctx, &sandboxes.GetWorkspaceRequest{})
		if api, ok := errors.AsType[*sandboxes.Error](err); ok && api.WorkspaceChoice != nil {
			listed = api.WorkspaceChoice.Workspaces
		} else if err == nil {
			listed = []sandboxes.Workspace{*workspace}
		}

		var ids []string
		for _, workspace := range listed {
			ids = append(ids, cobra.CompletionWithDesc(workspace.WorkspaceID, target.OneLine(workspace.Name)))
		}
		return ids, cobra.ShellCompDirectiveNoFileComp
	}
}
