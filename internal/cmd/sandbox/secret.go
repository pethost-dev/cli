package sandbox

import (
	"fmt"
	"strings"

	"github.com/pethost-dev/sdk/go/sandboxes"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/ui"
)

// secretCommand is `pethost sandbox secret`, which lists, and the commands under it.
func secretCommand(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "secret",
		Aliases: []string{"secrets"},
		Short:   "Secrets: keys a sandbox uses and never holds",
		Long: `Lists the workspace's secrets, by name, never their values. A sandbox that binds one to a
variable (pethost sandbox new --env ANTHROPIC_API_KEY=secret:anthropic) holds a stand-in,
which its egress swaps for the value in requests to the secret's hosts alone.`,
		Example: `  pethost sandbox secret
  pethost sandbox secret set anthropic --host api.anthropic.com
  pethost sandbox secret rm anthropic`,
		Args: cobra.NoArgs,
	}
	list := secretListCommand(a)
	cmd.RunE = list.RunE
	cmd.AddCommand(list, secretSetCommand(a), secretRmCommand(a))
	return cmd
}

func secretListCommand(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:     "ls",
		Short:   "The workspace's secrets, by name",
		Example: `  pethost sandbox secret ls`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client(a)
			if err != nil {
				return err
			}
			workspace, err := c.GetWorkspace(cmd.Context(), &sandboxes.GetWorkspaceRequest{})
			if err != nil {
				return err
			}
			if a.JSON {
				return a.PrintJSON(workspace)
			}
			if len(workspace.Secrets) == 0 {
				a.UI.Info("No secret here yet. Set one:  %s", a.UI.Err.Bold("pethost sandbox secret set <name> --host <host>"))
				return nil
			}
			out := a.UI.Out
			table := out.Table()
			for _, secret := range workspace.Secrets {
				table.Row(out.Bold(ui.Clean(secret.Name)), "for "+strings.Join(cleaned(secret.Hosts), ", "), "set "+out.Ago(secret.UpdateTime))
			}
			table.Flush()
			return nil
		},
	}
}

func secretSetCommand(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set <name>",
		Short: "Set a secret: its value, never on the command line, and its hosts",
		Long: `Sets a secret of the workspace: its value, which pethost asks for without showing it, or
reads from stdin, and the hosts it is sent to, a --host each. A secret already set takes
the new value and hosts together, and the sandboxes that bind it use them at once.`,
		Example: `  pethost sandbox secret set anthropic --host api.anthropic.com
  printf %s "$OPENAI_API_KEY" | pethost sandbox secret set openai --host api.openai.com`,
		Args: cobra.ExactArgs(1),
	}
	var hosts []string
	cmd.Flags().StringArrayVar(&hosts, "host", nil, "a host the value is sent to, as api.anthropic.com (again for more)")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		c, err := clientForChange(a)
		if err != nil {
			return err
		}
		value, err := a.UI.Secret(ui.Clean(args[0]))
		if err != nil {
			return err
		}
		if value == "" {
			return fail.Usage("pethost sandbox secret set: no value was given, typed or piped in.", cmd.UseLine())
		}
		secret, err := c.SetSecret(cmd.Context(), &sandboxes.SetSecretRequest{Name: args[0], Value: value, Hosts: hosts})
		if err != nil {
			return err
		}
		if a.JSON {
			return a.PrintJSON(secret)
		}
		a.UI.Done("Set the secret %s for %s. Bind it:  %s", ui.Clean(secret.Name), strings.Join(cleaned(secret.Hosts), ", "),
			a.UI.Err.Bold("--env <VARIABLE>=secret:"+ui.Clean(secret.Name)))
		_, err = fmt.Fprintln(a.UI.Out, ui.Clean(secret.Name))
		return err
	}
	return cmd
}

func secretRmCommand(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "rm <name>",
		Short: "Delete a secret",
		Long: `Deletes a secret of the workspace; one a sandbox still binds is refused, until that
sandbox is deleted. It asks first, and needs --yes where nobody can be asked.`,
		Example: `  pethost sandbox secret rm anthropic --yes`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := clientForChange(a)
			if err != nil {
				return err
			}
			question := fmt.Sprintf("Delete the secret %s?", ui.Clean(args[0]))
			if yes, err := a.UI.Confirm(question, a.CommandLine("--yes")); err != nil || !yes {
				return declined(err, "Nothing was deleted.")
			}
			secret, err := c.DeleteSecret(cmd.Context(), &sandboxes.DeleteSecretRequest{Name: args[0]})
			if err != nil {
				return err
			}
			if a.JSON {
				return a.PrintJSON(secret)
			}
			_, err = fmt.Fprintln(a.UI.Out, ui.Clean(secret.Name))
			return err
		},
	}
}
