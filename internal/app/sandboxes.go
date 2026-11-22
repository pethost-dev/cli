package app

import (
	"github.com/pethost-dev/sdk/go/sandboxes"
)

// Sandboxes is the SDK's client of Agent Sandboxes, made on first use with the run's token, its
// base URL and --debug's HTTP client, as Client is. It keeps each sandbox's connection itself:
// a call into a sandbox goes to the sandbox's own server, never through the panel.
//
// Its workspace is only the one the run was given outright (-w, PETHOST_WORKSPACE, or one picked
// during the run): a folder's .pethost names an App Platform workspace, never a sandboxes one. A
// call that names a sandbox acts in the sandbox's own workspace, whatever the run was given.
func (a *App) Sandboxes() *sandboxes.Client {
	if a.sandboxes == nil {
		current, _ := a.SignIn()
		given, _ := a.GivenWorkspace()
		a.sandboxes = sandboxes.NewClient(sandboxes.Config{
			Token: current.Token, BaseURL: current.URL, HTTPClient: a.debugClient(), Workspace: given,
		})
	}
	return a.sandboxes
}
