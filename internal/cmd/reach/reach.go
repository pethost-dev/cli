// Package reach is the commands that reach into a project's containers and files: run, shell,
// tunnel, files and cp. Each is a file of its own, cp three; connect.go is the SSH connection
// the shell and the tunnel share, and place.go the "[name:]path" that files and cp take.
package reach

import (
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
)

// Commands are the area's commands, which the root adds.
func Commands(a *app.App) []*cobra.Command {
	return []*cobra.Command{runCommand(a), shellCommand(a), tunnelCommand(a), filesCommand(a), cpCommand(a)}
}
