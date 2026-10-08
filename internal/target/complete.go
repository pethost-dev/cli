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

// Completion asks the machine what exists: project ids for -p, and a project's service names
// for a command that takes one. A shell waits for it at each Tab, so it gives up soon, and any
// failure completes nothing.

// A Completion is a function cobra calls to complete a flag's value or an argument.
type Completion = func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective)

// completionWait bounds the call a Tab makes.
const completionWait = 3 * time.Second

// CompleteProjects completes a project's id, with the name people gave it as its description.
func CompleteProjects(a *app.App) Completion {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		var ids []string
		for _, project := range projectsNow(cmd.Context(), a) {
			ids = append(ids, cobra.CompletionWithDesc(project.ProjectID, oneLine(Title(project.Metadata))))
		}
		return ids, cobra.ShellCompDirectiveNoFileComp
	}
}

// CompleteServices completes a service's name as a command's first argument: of the project
// that -p, PETHOST_PROJECT or .pethost names, else of the machine's only one. Give it to a
// command as its ValidArgsFunction.
func CompleteServices(a *app.App) Completion {
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

// projectsNow is the machine's projects, or none when they cannot be had in time.
func projectsNow(ctx context.Context, a *app.App) []pethost.ProjectSummary {
	client, err := a.Client()
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, completionWait)
	defer cancel()

	machine, err := client.GetMachine(ctx, nil)
	if err != nil {
		return nil
	}
	return machine.Projects
}

// oneLine is a name as a shell's completion can hold it: cleaned, and without the tab and the
// line break that separate completions.
func oneLine(text string) string {
	return strings.Join(strings.Fields(ui.Clean(text)), " ")
}
