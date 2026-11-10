// Package workspace is the commands of workspaces: workspaces, which lists the account's, and
// move, which moves a project to another. Each plan a person pays for is a workspace, with a
// machine of the plan's size; every other command acts in one (internal/target says which).
package workspace

import (
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
)

// Commands are the area's commands, which the root adds.
func Commands(a *app.App) []*cobra.Command {
	return []*cobra.Command{listCommand(a), moveCommand(a)}
}
