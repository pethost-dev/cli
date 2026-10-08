package change

import (
	"context"
	"fmt"
	"strings"

	pethost "github.com/pethost-dev/sdk/go"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/target"
	"github.com/pethost-dev/cli/internal/ui"
)

// envCommand is `pethost env`: the variables of the project's /.env, names only, each with the
// services that read it. --show prints the file itself, values and all.
func envCommand(a *app.App) *cobra.Command {
	var show bool
	cmd := &cobra.Command{
		Use:   "env",
		Short: "The project's variables, names only",
		Long: `The variables of the project's /.env file, names only, each with the services
that read it. The file carries over from version to version: a deploy does not
replace it.

  pethost env --show     prints the file itself, with its values
  pethost env set KEY    asks for the value without showing it`,
		Example: `  pethost env
  pethost env set STRIPE_KEY
  pethost env set PORT=3000 DEBUG=1
  pethost env rm DEBUG
  pethost env push
  pethost env --show > .env`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if show {
				return showEnvFile(cmd.Context(), a)
			}
			return listEnv(cmd.Context(), a)
		},
	}
	cmd.Flags().BoolVar(&show, "show", false, "print the file itself, with its values")
	cmd.AddCommand(envSetCommand(a), envRmCommand(a), envPushCommand(a))
	return cmd
}

// listEnv prints the names /.env sets, each with the services the machine says read it. Its
// two answers are asked for at once; --json prints the project, whose secret values the API
// leaves out.
func listEnv(ctx context.Context, a *app.App) error {
	project, err := target.ForRead(ctx, a)
	if err != nil {
		return err
	}
	client, err := a.Client()
	if err != nil {
		return err
	}

	var env *projectEnv
	var answer *pethost.GetProjectResponse
	err = app.Together(
		func() (err error) {
			env, err = readEnv(ctx, client, project.ID)
			return err
		},
		func() (err error) {
			answer, err = client.GetProject(ctx, &pethost.GetProjectRequest{ProjectID: project.ID})
			return err
		},
	)
	if err != nil {
		return err
	}
	if a.JSON {
		return a.PrintJSON(answer)
	}

	names := variableNames(env.text)
	if len(names) == 0 {
		sayNoVariables(a, project, env)
		return nil
	}

	out := a.UI.Out
	table := out.Table()
	for _, name := range names {
		table.Row(out.Bold(ui.Clean(name)), readBy(out, answer.Project, name))
	}
	table.Flush()

	if line := nobodyReadsLine(answer.Project); line != "" {
		a.UI.Info("%s", strings.TrimSpace(line))
	}
	a.UI.Info("Their values:  %s", a.UI.Err.Bold("pethost env --show"+project.Flag()))
	return nil
}

// sayNoVariables says that the project's /.env sets nothing, or is not there, and how to set a
// first variable.
func sayNoVariables(a *app.App, project target.Project, env *projectEnv) {
	what := "The " + envPath + " of " + ui.Clean(project.ID) + " sets no variable yet."
	if !env.exists {
		what = ui.Clean(project.ID) + " has no " + envPath + " yet."
	}
	a.UI.Info("%s Set a variable:  %s", what, a.UI.Err.Bold("pethost env set KEY"+project.Flag()))
}

// readBy says which services read a variable, as far as the machine can tell.
func readBy(out *ui.Stream, project *pethost.Project, name string) string {
	services, known := readers(project, name)
	switch {
	case len(services) > 0:
		return listed(services)
	case known:
		return out.Dim("no service reads it")
	}
	return ""
}

// showEnvFile prints the project's /.env as it is: on a terminal without what a terminal would
// obey, elsewhere byte for byte, so that it can be saved as a file. --json prints the API's
// answer of the read, which holds the text.
func showEnvFile(ctx context.Context, a *app.App) error {
	project, err := target.ForRead(ctx, a)
	if err != nil {
		return err
	}
	client, err := a.Client()
	if err != nil {
		return err
	}

	if a.JSON {
		answer, err := client.ReadPath(ctx, &pethost.ReadPathRequest{ProjectID: project.ID, Path: envPath, LengthBytes: readChunk})
		if err != nil {
			return err
		}
		return a.PrintJSON(answer)
	}

	env, err := readEnv(ctx, client, project.ID)
	if err != nil {
		return err
	}
	if !env.exists {
		sayNoVariables(a, project, env)
		return nil
	}

	text := env.text
	if a.UI.Out.Terminal {
		text = ui.Clean(text)
		if text != "" && !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
	}
	_, err = fmt.Fprint(a.UI.Out, text)
	return err
}
