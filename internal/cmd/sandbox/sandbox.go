// Package sandbox is `pethost sandbox` (or `pethost sbx`): Agent Sandboxes from the command line,
// microVMs to run code, coding agents and a desktop in. Every command is a few calls of the Go
// SDK's sandboxes client (app.Sandboxes), which calls a sandbox's own server for what runs in it:
// the CLI validates nothing the API validates and keeps no copy of a rule.
//
// A file a command: list, new (with settings.go, the flags of a sandbox's settings, which set
// shares), show, run (with run_follow.go), shell, cp (with cp_up.go, cp_down.go and tar.go), the
// state changes in state.go, open, agent (with agent_follow.go), ports, url, set, events,
// template, secret and workspace. words.go says what the API's values are in words,
// refusal.go adds the next command to a refusal of one sandbox, named.go finds a sandbox a person
// names by its name, and complete.go completes ids and names.
package sandbox

import (
	"github.com/pethost-dev/sdk/go/sandboxes"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
)

// Commands are the area's commands, which the root adds: `sandbox`, with every command under it.
func Commands(a *app.App) []*cobra.Command {
	return []*cobra.Command{sandboxCommand(a)}
}

// name is the area's command, which Under looks for.
const name = "sandbox"

func sandboxCommand(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:     name,
		Aliases: []string{"sbx", "sandboxes"},
		Short:   "Agent Sandboxes: microVMs for code, coding agents and a desktop",
		Long: `Agent Sandboxes are Linux microVMs, each a Firecracker VM with its own kernel, to run
code, coding agents and a desktop in. Alone, it lists your sandboxes.

A sandbox pauses when nothing has happened in it for a while, keeping its memory and
processes, and wakes at the next command, SSH login or request to one of its ports. A
sandbox is named by its id, in any of your workspaces of sandboxes, or by the name new or
set gave it, in the workspace -w names, else your only one; a command that makes or lists
something acts in that workspace too.`,
		Example: `  pethost sandbox new --name api-dev
  pethost sandbox run api-dev -- python3 -c 'print(6 * 7)'
  pethost sandbox shell k7f3q2xy0a1b2c3d
  pethost sbx ls --state running`,
		Args: cobra.NoArgs,
	}
	list := listCommand(a)
	cmd.RunE = list.RunE
	cmd.Flags().AddFlagSet(list.LocalFlags())

	cmd.AddCommand(
		list, newCommand(a), showCommand(a), runCommand(a), shellCommand(a), cpCommand(a),
		pauseCommand(a), resumeCommand(a), stopCommand(a), rmCommand(a),
		openCommand(a), agentCommand(a), portsCommand(a), urlCommand(a), setCommand(a),
		eventsCommand(a), templateCommand(a), secretCommand(a), workspaceCommand(a),
	)
	return cmd
}

// Under says whether a command is `pethost sandbox` or one under it: its -w names a workspace of
// sandboxes, which the root completes from this area.
func Under(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		if c.Name() == name && c.Parent() != nil && !c.Parent().HasParent() {
			return true
		}
	}
	return false
}

// client is the SDK's client of Agent Sandboxes, once the run has a sign-in to call with.
func client(a *app.App) (*sandboxes.Client, error) {
	if err := a.NeedSignIn(); err != nil {
		return nil, err
	}
	return a.Sandboxes(), nil
}

// clientForChange is client, for a command that changes something: it says first when it acts
// by PETHOST_TOKEN over this computer's own sign-in.
func clientForChange(a *app.App) (*sandboxes.Client, error) {
	c, err := client(a)
	if err == nil {
		a.SayToken()
	}
	return c, err
}
