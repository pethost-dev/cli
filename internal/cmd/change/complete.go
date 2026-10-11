package change

import (
	pethost "github.com/pethost-dev/sdk/go"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/target"
)

// What a Tab completes for this area's arguments that are not a project or a service: a
// repository, an SSH key's fingerprint, a session's id. Each asks the machine, gives up soon,
// and on any failure completes nothing.

// completeMachine completes a command's first argument from the machine's answer: values
// returns each candidate with a few words about it.
func completeMachine(a *app.App, values func(machine *pethost.GetMachineResponse) map[string]string) cobra.CompletionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		machine := target.MachineNow(cmd.Context(), a)
		if machine == nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		var completions []string
		for value, about := range values(machine) {
			completions = append(completions, cobra.CompletionWithDesc(value, target.OneLine(about)))
		}
		return completions, cobra.ShellCompDirectiveNoFileComp
	}
}

func completeRepositories(a *app.App) cobra.CompletionFunc {
	return completeMachine(a, func(machine *pethost.GetMachineResponse) map[string]string {
		values := map[string]string{}
		if machine.Github != nil {
			for _, repository := range machine.Github.Repositories {
				values[repository.Repository] = repository.DefaultBranch
			}
		}
		return values
	})
}

func completeKeys(a *app.App) cobra.CompletionFunc {
	return completeMachine(a, func(machine *pethost.GetMachineResponse) map[string]string {
		values := map[string]string{}
		if machine.Machine != nil {
			for _, key := range machine.Machine.SSHKeys {
				values[key.Fingerprint] = key.Label
			}
		}
		return values
	})
}

func completeSessions(a *app.App) cobra.CompletionFunc {
	return completeMachine(a, func(machine *pethost.GetMachineResponse) map[string]string {
		values := map[string]string{}
		if machine.Machine != nil {
			for _, session := range machine.Machine.Sessions {
				values[session.SessionID] = sessionKind(session) + " in " + session.Service + "." + session.ProjectID
			}
		}
		return values
	})
}
