// Package change is the commands that change a project or the machine: env, domain, password,
// rename, rollback, restart, stop, start, backup, volume, github, delete and machine. A bare
// noun lists, a word after it changes, and rm removes.
//
// Each command is a file of its own, the larger ones several. What they share is three files:
// deploy.go, a change that is a deploy (DeployProject); action.go, one that is an action of the
// project (RunProjectAction); and outcome.go, the operation either starts, taken to its end.
// The machine owns the facts and the rules: a command reads what it shows, sends what was
// asked, and prints the machine's refusal as the machine wrote it.
package change

import (
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
)

// Commands are the area's commands, which the root adds.
func Commands(a *app.App) []*cobra.Command {
	return []*cobra.Command{envCommand(a), domainCommand(a), passwordCommand(a), renameCommand(a), rollbackCommand(a), restartCommand(a), stopCommand(a), startCommand(a), backupCommand(a), volumeCommand(a), githubCommand(a), deleteCommand(a), machineCommand(a)}
}
