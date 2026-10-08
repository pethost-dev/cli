// Package observe is the commands that show what a project does: why, logs, requests and open.
// Each has the files that begin with its name; follow.go and since.go are what several share:
// the stream that is opened again where it broke, and the --since flag.
package observe

import (
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
)

// Commands are the area's commands, which the root adds.
func Commands(a *app.App) []*cobra.Command {
	return []*cobra.Command{whyCommand(a), logsCommand(a), requestsCommand(a), openCommand(a)}
}
