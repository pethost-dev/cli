package target

import (
	"errors"
	"strings"

	"github.com/pethost-dev/sdk/go/sandboxes"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/ui"
)

// A call of Agent Sandboxes that names no sandbox acts in the workspace -w names, else the
// account's only workspace of sandboxes; with several it is refused with the list
// (sandboxes.WorkspaceChoice), as App Platform's calls are. A person at a terminal picks one and
// the command runs again there; anyone else is given the command for each.

// sandboxesAgain is Again for a refusal of Agent Sandboxes: the workspace the person picked, and
// true, where a call needed one and a person can be asked.
func sandboxesAgain(a *app.App, api *sandboxes.Error) (string, bool, error) {
	choice := api.WorkspaceChoice
	if choice == nil || api.Code != sandboxes.CodeFailedPrecondition || !a.UI.CanAsk() || len(choice.Workspaces) == 0 {
		return "", false, nil
	}

	choices := make([]ui.Choice, len(choice.Workspaces))
	for i, workspace := range choice.Workspaces {
		choices[i] = ui.Choice{
			Label:   ui.Clean(workspace.Name),
			Detail:  ui.Clean(workspace.WorkspaceID),
			Command: a.InWorkspace(workspace.WorkspaceID),
		}
	}
	picked, err := a.UI.Pick("Which workspace of sandboxes?", choices)
	if err != nil {
		return "", false, err
	}
	return choice.Workspaces[picked].WorkspaceID, true, nil
}

// explainSandboxesChoice ends a call that needed a workspace of sandboxes with the command that
// names each one the API listed.
func explainSandboxesChoice(a *app.App, api *sandboxes.Error) error {
	e := fail.From(api)
	e.Lead = "Name one with -w:"
	var commands []string
	for _, workspace := range api.WorkspaceChoice.Workspaces {
		commands = append(commands, a.InWorkspace(workspace.WorkspaceID))
	}
	e.Next = strings.Join(commands, "\n")
	return e
}

// asSandboxesChoice is the error as a refusal of Agent Sandboxes that lists workspaces, and
// whether it is one that a command left as the API said it.
func asSandboxesChoice(failure error) (*sandboxes.Error, bool) {
	api, ok := errors.AsType[*sandboxes.Error](failure)
	_, worded := failure.(*fail.Error)
	return api, ok && api.WorkspaceChoice != nil && !worded
}
