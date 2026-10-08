// Package account is the commands about the person and the tool itself: login, logout, whoami,
// update, api and the help. Each is a file of its own.
//
// Two things other commands lean on live here too: SignIn, which signs a person in from inside
// `pethost deploy`, and CheckForUpdate, the look for a newer release after a command's output.
package account

import (
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
)

// Commands are the area's commands, which the root adds.
func Commands(a *app.App) []*cobra.Command {
	return []*cobra.Command{loginCommand(a), logoutCommand(a), whoamiCommand(a), updateCommand(a), apiCommand(a)}
}
