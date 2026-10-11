// Package kernel is the commands the others are measured against: the status, which is also
// the bare command, ls, and deploy. status and ls are the files that begin with status, deploy
// the ones that begin with deploy.
package kernel

import (
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
)

// Commands are the area's commands, which the root adds. The root runs the one named "status"
// as the bare command too.
func Commands(a *app.App) []*cobra.Command {
	return []*cobra.Command{deployCommand(a), statusCommand(a), lsCommand(a)}
}
