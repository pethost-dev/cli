package root

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/signin"
)

// Wrong guesses land: a word another tool uses for the same thing runs the command it meant.
// None is shown in the help.

// guesses are other tools' words for a command, by the command's name.
var guesses = map[string][]string{
	"shell":  {"ssh"},
	"run":    {"exec"},
	"env":    {"secrets", "vars", "config"},
	"status": {"ps"},
	"delete": {"destroy"},
	"deploy": {"up", "redeploy"},
	"files":  {"cat"},
}

// removalGuesses are other words for `rm` after a noun: removal is always rm.
var removalGuesses = []string{"unset", "remove"}

// landGuesses gives the root's commands their other names, and every `rm` under a noun its own.
// A bare `rm` or `remove` runs nothing, since it does not say what of: it is answered with
// `delete`, the one removal that has no noun.
func landGuesses(root *cobra.Command) {
	for _, command := range root.Commands() {
		command.Aliases = append(command.Aliases, guesses[command.Name()]...)
		if command.Name() == "delete" {
			command.SuggestFor = append(command.SuggestFor, "rm", "remove")
		}
		landRemovals(command)
	}
}

func landRemovals(noun *cobra.Command) {
	for _, command := range noun.Commands() {
		if command.Name() == "rm" {
			command.Aliases = append(command.Aliases, removalGuesses...)
		}
		landRemovals(command)
	}
}

// panelCommands are the words for what only a person signed in to the panel can do: the plan,
// the bill, the API tokens. A token cannot touch the account, so each answers with the panel's
// page.
func panelCommands(a *app.App) []*cobra.Command {
	return []*cobra.Command{
		panelCommand(a, "plan", "The plan is chosen and changed in the panel"),
		panelCommand(a, "billing", "The bill and the card are in the panel"),
		panelCommand(a, "token", "API tokens are made and revoked in the panel, under Settings"),
	}
}

func panelCommand(a *app.App, name, says string) *cobra.Command {
	return &cobra.Command{
		Use:     name,
		Short:   says,
		Example: "  pethost " + name,
		Hidden:  true,
		Args:    cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			current, err := a.SignIn()
			if err != nil {
				current.URL = signin.DefaultURL
			}
			a.UI.Info("%s: the CLI's sign-in cannot touch the account.", says)
			_, err = fmt.Fprintln(a.UI.Out, current.URL+"/settings")
			return err
		},
	}
}
