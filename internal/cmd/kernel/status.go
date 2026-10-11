package kernel

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/target"
)

// statusCommand is `pethost status`, and the bare `pethost`: the project of this folder, else
// the machine and every project. It only reads.
func statusCommand(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "What runs: this folder's project, else the machine and every project",
		Example: `  pethost
  pethost status -p recipes
  pethost --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			project, named, err := target.Named(a)
			if err != nil {
				return err
			}
			if !named {
				return showMachine(cmd.Context(), a)
			}
			return showProject(cmd.Context(), a, project)
		},
	}
}

// lsCommand is `pethost ls`: the machine and every project, in a project's folder too.
func lsCommand(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "The machine and every project on it",
		Example: `  pethost ls
  pethost ls --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return showMachine(cmd.Context(), a)
		},
	}
}

func showMachine(ctx context.Context, a *app.App) error {
	client, err := a.Client()
	if err != nil {
		return err
	}
	machine, err := client.GetMachine(ctx, nil)
	if err != nil {
		return err
	}

	if a.JSON {
		return a.PrintJSON(machine)
	}
	printMachine(a, machine)
	return nil
}

// showProject shows one project under the machine's line. The two answers are asked for at
// once.
func showProject(ctx context.Context, a *app.App, project target.Project) error {
	machine, answer, err := a.MachineAndProject(ctx, project.ID)
	if err != nil {
		return err
	}

	if a.JSON {
		return a.PrintJSON(answer)
	}
	target.Announce(a, project)
	printMachineLine(a, machine.Machine)
	printProject(a, project, machine.Machine, answer.Project)
	return nil
}
