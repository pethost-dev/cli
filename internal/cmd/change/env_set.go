package change

import (
	"context"
	"strings"

	pethost "github.com/pethost-dev/sdk/go"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/operation"
	"github.com/pethost-dev/cli/internal/target"
	"github.com/pethost-dev/cli/internal/ui"
)

// env set and env rm: an edit of the project's /.env, deployed. The file is read, the one
// statement is changed in its text, and the whole text is written back on the deploy it was
// read from: if the project was deployed in between, it is read and edited once more.

// envSetCommand is `pethost env set KEY[=VALUE]...`.
func envSetCommand(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set KEY[=VALUE]...",
		Short: "Set variables; a bare KEY reads its value hidden, or from stdin",
		Long: `Set variables in the project's /.env and deploy it.

KEY alone asks for the value without showing it, so that it stays out of the
shell's history; where nobody can be asked, the value is all of stdin. KEY=VALUE
takes the value as typed. Either way the service gets exactly that value: the CLI
quotes it in the file where the file needs it.`,
		Example: `  pethost env set STRIPE_KEY
  pethost env set PORT=3000 DEBUG=1
  pethost env set STRIPE_KEY < key.txt
  pethost env set DEBUG=          an empty value`,
		Args: cobra.MinimumNArgs(1),
	}
	noQueue := operation.NoQueueFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		variables, err := namedVariables(cmd, args)
		if err != nil {
			return err
		}
		project, err := target.ForChange(cmd.Context(), a)
		if err != nil {
			return err
		}
		if err := askValues(a, variables, project); err != nil {
			return err
		}
		return editEnv(cmd.Context(), a, project, *noQueue, func(text string) (string, string) {
			return setVariables(text, variables)
		})
	}
	return cmd
}

// envRmCommand is `pethost env rm KEY...`.
func envRmCommand(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rm KEY...",
		Short: "Remove variables",
		Example: `  pethost env rm DEBUG
  pethost env rm DEBUG OLD_FLAG`,
		Args: cobra.MinimumNArgs(1),
	}
	noQueue := operation.NoQueueFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		project, err := target.ForChange(cmd.Context(), a)
		if err != nil {
			return err
		}
		return editEnv(cmd.Context(), a, project, *noQueue, func(text string) (string, string) {
			return removeVariables(text, args)
		})
	}
	return cmd
}

// A variable is a name with the value to give it.
type variable struct {
	name, value string
	// The command line gave no value: it is asked for.
	asked bool
}

// namedVariables reads the command line: KEY=VALUE holds its value, a bare KEY has it asked for.
func namedVariables(cmd *cobra.Command, args []string) ([]variable, error) {
	variables := make([]variable, len(args))
	for i, arg := range args {
		name, value, given := strings.Cut(arg, "=")
		if name == "" || strings.ContainsAny(name, "\r\n") {
			return nil, fail.Usage(cmd.CommandPath()+": "+ui.Clean(arg)+" names no variable", cmd.UseLine())
		}
		variables[i] = variable{name: name, value: value, asked: !given}
	}
	return variables, nil
}

// askValues reads the value of each bare KEY: typed hidden, or all of stdin where nobody can
// be asked, which holds one value and no more. Never from an argument.
func askValues(a *app.App, variables []variable, project target.Project) error {
	read := ""
	for i, v := range variables {
		if !v.asked {
			continue
		}
		if read != "" && !a.UI.CanAsk() {
			return fail.Choice("Stdin holds one value, and both "+read+" and "+v.name+" ask for it.").
				WithLead("Set them one at a time:", "pethost env set "+word(v.name)+project.Flag()+" < file")
		}

		value, err := hiddenValue(a, v.name, project)
		if err != nil {
			return err
		}
		variables[i].value, read = value, v.name
	}
	return nil
}

// hiddenValue reads a variable's value without showing it. No value is a refusal: an empty
// one is set on purpose, with KEY=.
func hiddenValue(a *app.App, name string, project target.Project) (string, error) {
	canAsk := a.UI.CanAsk()
	value, err := a.UI.Secret(name)
	if err != nil {
		return "", err
	}
	if value != "" {
		return value, nil
	}

	empty := "pethost env set " + word(name+"=") + project.Flag()
	if canAsk {
		return "", fail.Declined("No value was typed for "+name+": nothing changed.").WithLead("To set it empty:", empty)
	}
	return "", fail.Choice("No value came for "+name+": it is read from stdin here.",
		"pethost env set "+word(name)+project.Flag()+" < file", empty)
}

// setVariables is the text with the variables set, and what that makes of it in words.
func setVariables(text string, variables []variable) (string, string) {
	names := make([]string, len(variables))
	changed := text
	for i, v := range variables {
		names[i] = v.name
		changed = withVariable(changed, v.name, v.value)
	}

	if changed == text {
		return text, envPath + " already sets " + listed(names) + " this way: nothing changed."
	}
	return changed, envPath + " has " + listed(names) + " now."
}

// removeVariables is the text without the variables, and what that makes of it in words.
func removeVariables(text string, names []string) (string, string) {
	var gone, absent []string
	changed := text
	for _, name := range names {
		if _, set := statementOf(changed, name); !set {
			absent = append(absent, name)
			continue
		}
		gone = append(gone, name)
		changed = withoutVariable(changed, name)
	}

	if len(gone) == 0 {
		return text, envPath + " has no " + listed(absent) + ": nothing changed."
	}
	says := envPath + " no longer has " + listed(gone) + "."
	if len(absent) > 0 {
		says += " It had no " + listed(absent) + "."
	}
	return changed, says
}

// sayEnvStays says, under a deploy that failed after it made the files the project's, that the
// new /.env is among them: a rollback, which the failure's lines name, leaves /.env as it is.
func sayEnvStays(a *app.App, failed *outcome) {
	if failed != nil && failed.Operation.MadeCurrent && !a.JSON {
		a.UI.ErrLine("  " + envPath + " keeps this change: a rollback does not bring the earlier " + envPath + " back.")
	}
}

var envWords = operation.Words{Doing: "Deploying…", Failed: "Deploy failed"}

// editEnv reads the project's /.env, edits its text and deploys it. edit returns the text as
// it is to be and what to say of it; the same text back means there is nothing to deploy.
func editEnv(ctx context.Context, a *app.App, project target.Project, noQueue bool, edit func(text string) (changed, says string)) error {
	client, err := a.Client()
	if err != nil {
		return err
	}

	var done *outcome
	var says string
	err = againIfChanged(func() error {
		env, err := readEnv(ctx, client, project.ID)
		if err != nil {
			return err
		}

		var text string
		if text, says = edit(env.text); text == env.text {
			return nil
		}
		done, err = deploy(ctx, a, project, noQueue, &pethost.DeployProjectRequest{
			BaseDeployID: env.deployID,
			Files:        []pethost.FileChange{{Path: envPath, Change: pethost.FileChangeText(text)}},
		}, envWords)
		return err
	})
	if err != nil {
		sayEnvStays(a, done)
		return err
	}
	if done == nil {
		a.UI.Info("%s", says)
		return nil
	}

	a.UI.Done("%s Deployed in %s.", says, done.took())
	if line := nobodyReadsLine(done.Project); line != "" {
		a.UI.Info("%s", line)
	}
	return done.said(a)
}
