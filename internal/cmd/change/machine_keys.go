package change

import (
	"context"
	"fmt"
	"strings"

	pethost "github.com/pethost-dev/sdk/go"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/ui"
)

// The machine's SSH keys and the sessions open in its containers: who can get in, and who is.
// Removing a key ends its sessions; ending a session leaves its key.

// machineKeysCommand is `pethost machine keys`.
func machineKeysCommand(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "keys",
		Short: "The SSH keys that open the containers",
		Long: `Your public keys on the machine: each opens a shell, SFTP and tunnels into every
container of every project. "pethost shell" adds this computer's key by itself.`,
		Example: `  pethost machine keys
  pethost machine keys rm SHA256:Yx3…`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return showMachine(cmd.Context(), a, func(machine *pethost.Machine) { printKeys(a, machine) })
		},
	}
	cmd.AddCommand(machineKeysRmCommand(a))
	return cmd
}

// machineKeysRmCommand is `pethost machine keys rm <fingerprint>`.
func machineKeysRmCommand(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "rm <fingerprint>",
		Short: "Remove an SSH key; its sessions end",
		Long: `Remove a key from the machine: the sessions it opened end, and it opens no more.
Name it by its fingerprint, by the fingerprint's start, or by its label.`,
		Example: `  pethost machine keys rm SHA256:Yx3…
  pethost machine keys rm 'MacBook Pro'`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeKeys(a),
		RunE: func(cmd *cobra.Command, args []string) error {
			return removeKey(cmd.Context(), a, args[0])
		},
	}
}

func removeKey(ctx context.Context, a *app.App, named string) error {
	a.SayToken()
	client, err := a.Client()
	if err != nil {
		return err
	}
	before, err := client.GetMachine(ctx, nil)
	if err != nil {
		return err
	}

	key := keyNamed(before.Machine, named)
	answer, err := client.RunMachineAction(ctx, &pethost.RunMachineActionRequest{
		Action: pethost.RunMachineActionRemoveSSHKeyFingerprint(key.Fingerprint),
	})
	if err != nil {
		return err
	}
	if a.JSON {
		return a.PrintJSON(answer)
	}

	which := ui.Clean(key.Fingerprint)
	if key.Label != "" {
		which = ui.Clean(key.Label) + " (" + which + ")"
	}
	a.UI.Done("Removed the key %s: its sessions ended, and it opens no more.", which)
	if answer.Machine != nil {
		a.UI.Info("  %s", keysLeft(len(answer.Machine.SSHKeys)))
	}
	return nil
}

// keyNamed is the machine's key a person means: by its fingerprint, with or without "SHA256:";
// else the only key whose fingerprint starts so or whose label is that. A name that fits none,
// or several, goes to the machine as a fingerprint, and the machine says what it has.
func keyNamed(machine *pethost.Machine, named string) pethost.SSHKey {
	if machine == nil {
		return pethost.SSHKey{Fingerprint: named}
	}

	var fits []pethost.SSHKey
	for _, key := range machine.SSHKeys {
		bare := strings.TrimPrefix(key.Fingerprint, "SHA256:")
		switch {
		case key.Fingerprint == named || bare == named:
			return key
		case key.Label == named || strings.HasPrefix(key.Fingerprint, named) || strings.HasPrefix(bare, named):
			fits = append(fits, key)
		}
	}
	if len(fits) == 1 {
		return fits[0]
	}
	return pethost.SSHKey{Fingerprint: named}
}

func keysLeft(count int) string {
	switch count {
	case 0:
		return "No key is left: nobody can log in to a container until one is added, as pethost shell does."
	case 1:
		return "1 key is left."
	}
	return fmt.Sprintf("%d keys are left.", count)
}

// printKeys writes the keys, each with its fingerprint.
func printKeys(a *app.App, machine *pethost.Machine) {
	out := a.UI.Out
	if len(machine.SSHKeys) == 0 {
		a.UI.Info("No SSH key yet: %s adds this computer's.", a.UI.Err.Bold("pethost shell"))
		return
	}

	a.UI.Info("SSH keys: each opens every container.")
	table := out.Table()
	for _, key := range machine.SSHKeys {
		table.Row(keyLabel(out, key), ui.Clean(key.Fingerprint))
	}
	table.Flush()
}

func keyLabel(out *ui.Stream, key pethost.SSHKey) string {
	if key.Label == "" {
		return out.Dim("no label")
	}
	return out.Bold(ui.Clean(key.Label))
}

// machineSessionsCommand is `pethost machine sessions`.
func machineSessionsCommand(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "What is open in the containers now",
		Long: `What people have open in the containers now: shells, commands, file transfers,
tunnels and the panel's terminals, oldest first.`,
		Example: `  pethost machine sessions
  pethost machine sessions end k3f9`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return showMachine(cmd.Context(), a, func(machine *pethost.Machine) { printSessions(a, machine) })
		},
	}
	cmd.AddCommand(machineSessionsEndCommand(a))
	return cmd
}

// machineSessionsEndCommand is `pethost machine sessions end <id>`.
func machineSessionsEndCommand(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "end <id>",
		Short: "End a session",
		Long: `End a session now. Its key can open new ones: "pethost machine keys rm" takes
the key away.`,
		Example:           `  pethost machine sessions end k3f9`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeSessions(a),
		RunE: func(cmd *cobra.Command, args []string) error {
			return endSession(cmd.Context(), a, args[0])
		},
	}
}

func endSession(ctx context.Context, a *app.App, id string) error {
	a.SayToken()
	client, err := a.Client()
	if err != nil {
		return err
	}
	answer, err := client.RunMachineAction(ctx, &pethost.RunMachineActionRequest{Action: pethost.RunMachineActionEndSessionID(id)})
	if err != nil {
		return err
	}
	if a.JSON {
		return a.PrintJSON(answer)
	}
	a.UI.Done("Ended the session %s.", ui.Clean(id))
	return nil
}

// printSessions writes what is open in the containers: a row for each session.
func printSessions(a *app.App, machine *pethost.Machine) {
	out := a.UI.Out
	if len(machine.Sessions) == 0 {
		a.UI.Info("No session is open in a container.")
		return
	}

	a.UI.Info("Open sessions:")
	table := out.Table()
	for _, session := range machine.Sessions {
		table.Row(
			out.Bold(ui.Clean(session.SessionID)),
			sessionKind(session),
			ui.Clean(session.Service)+"."+ui.Clean(session.ProjectID),
			"since "+out.At(session.StartTime),
			"from "+ui.Clean(session.ClientAddress),
			sessionKey(session),
		)
	}
	table.Flush()
	a.UI.Info("End one:  %s", a.UI.Err.Bold("pethost machine sessions end "+word(machine.Sessions[0].SessionID)))
}

// sessionKind is what a session is, in a word or two.
func sessionKind(session pethost.MachineSession) string {
	switch session.Kind {
	case pethost.MachineSessionKindWebTerminal:
		return "the panel's terminal"
	case pethost.MachineSessionKindSSHTerminal:
		return "shell"
	case pethost.MachineSessionKindSSHCommand:
		return "command"
	case pethost.MachineSessionKindSftp:
		return "file transfer"
	case pethost.MachineSessionKindTunnel:
		return fmt.Sprintf("tunnel to port %d", session.Port)
	}
	return strings.ToLower(strings.ReplaceAll(session.Kind.String(), "_", " "))
}

// sessionKey is the key a session came in with, by its label, else its fingerprint.
func sessionKey(session pethost.MachineSession) string {
	switch {
	case session.SSHKeyLabel != "":
		return "key " + ui.Clean(session.SSHKeyLabel)
	case session.SSHKeyFingerprint != "":
		return "key " + ui.Clean(session.SSHKeyFingerprint)
	}
	return ""
}
