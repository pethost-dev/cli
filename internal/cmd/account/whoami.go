package account

import (
	"context"
	"strings"

	pethost "github.com/pethost-dev/sdk/go"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/signin"
	"github.com/pethost-dev/cli/internal/ui"
)

// whoamiCommand is `pethost whoami`: the account as the API states it, which panel the calls go
// to, and how this run is signed in. The API names no person: an account is known by its plan
// and its domain.
func whoamiCommand(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Who is signed in, and where",
		Long: `Which account's machine the commands act on: its plan and its domain, the panel
they call, and whether they are signed in by PETHOST_TOKEN or by this computer's own
sign-in. The token itself is never printed.`,
		Example: `  pethost whoami`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return whoami(cmd.Context(), a)
		},
	}
}

func whoami(ctx context.Context, a *app.App) error {
	client, err := a.Client()
	if err != nil {
		return err
	}
	current, err := a.SignIn()
	if err != nil {
		return err
	}

	// An account without a machine is signed in all the same: how is said before the API's
	// refusal ends the command.
	answer, err := client.GetMachine(ctx, nil)
	if a.JSON {
		if err != nil {
			return err
		}
		return a.PrintJSON(answer)
	}

	table := a.UI.Out.Table()
	if err == nil && answer.Machine != nil {
		machineRows(table, answer.Machine)
	}
	table.Row("Panel", current.URL)
	table.Row("Sign-in", signInWords(a.UI.Out, current))
	table.Flush()
	return err
}

func machineRows(table *ui.Table, machine *pethost.Machine) {
	table.Row("Plan", ui.Clean(machine.Name))
	if machine.AppsDomain != "" {
		table.Row("Domain", ui.Clean(machine.AppsDomain))
	}

	where := []string{ui.Clean(machine.Hostname)}
	if machine.Location != "" {
		where = append(where, ui.Clean(machine.Location))
	}
	table.Row("Machine", strings.Join(where, " · "))
}

// signInWords says what the calls are signed with, never the token: PETHOST_TOKEN, or the
// sign-in stored on this computer, by which of `pethost login`'s two ways and since when.
func signInWords(out *ui.Stream, current signin.SignIn) string {
	if current.FromEnvironment {
		if current.OverStored {
			return "PETHOST_TOKEN, over this computer's own sign-in"
		}
		return "PETHOST_TOKEN"
	}

	stored, err := signin.Load()
	if err != nil || stored == nil {
		return "this computer's"
	}
	words := "an API token kept on this computer"
	if stored.ClientID != "" {
		words = "in the browser"
	}
	if stored.SignInTime.IsZero() {
		return words
	}
	return words + ", " + out.When(stored.SignInTime)
}
