package app

import (
	"os"
	"slices"
	"strings"

	"github.com/spf13/pflag"
)

// Which workspace a run's calls go to. An account has a workspace for each plan it pays for,
// and every call but ListWorkspaces may name one; one that names none goes to the account's only
// workspace, and is refused, with the list, when there are several. The workspace is -w, else
// PETHOST_WORKSPACE, else the one the folder's .pethost names (internal/target), else none.

// workspaceFlag adds -w, which every command takes.
func (a *App) workspaceFlag(flags *pflag.FlagSet) {
	flags.StringVarP(&a.Workspace, "workspace", "w", "", "the workspace to act in, by its id or name")
}

// GivenWorkspace is the workspace this run was given outright, whatever a folder says, and
// where it was given: -w, else PETHOST_WORKSPACE, else one chosen during the run (Chosen).
// Empty = none was.
func (a *App) GivenWorkspace() (workspace, where string) {
	switch {
	case a.Workspace != "":
		return a.Workspace, "-w"
	case os.Getenv("PETHOST_WORKSPACE") != "":
		return os.Getenv("PETHOST_WORKSPACE"), "PETHOST_WORKSPACE"
	case a.Chosen != "":
		return a.Chosen, "-w"
	}
	return "", ""
}

// UseWorkspace makes the run's calls from now on go to a workspace, by its id or name; empty =
// to none named. A client made before is made again: a command that kept one asks for it anew.
func (a *App) UseWorkspace(workspace string) {
	if workspace != a.workspace {
		a.workspace, a.client = workspace, nil
	}
}

// CallsWorkspace is the workspace the run's calls name now; empty = none.
func (a *App) CallsWorkspace() string {
	return a.workspace
}

// InWorkspace is this run's command line, as CommandLine writes it, in another workspace: the
// -w it had, typed or chosen, gives way to -w workspace, followed by more.
func (a *App) InWorkspace(workspace string, more ...string) string {
	other := *a
	other.Chosen, other.Workspace = "", ""
	other.Args = withoutWorkspace(a.Args)
	return other.CommandLine(append([]string{"-w", workspace}, more...)...)
}

// withoutWorkspace is a command line without its -w and the value given to it, in any of the
// ways pflag reads them; what follows a "--" is another program's, and stays.
func withoutWorkspace(args []string) []string {
	var kept []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--":
			return append(kept, args[i:]...)
		case arg == "-w" || arg == "--workspace":
			i++
		case strings.HasPrefix(arg, "--workspace="), strings.HasPrefix(arg, "-w"):
		default:
			kept = append(kept, arg)
		}
	}
	return slices.Clip(kept)
}
