// Package target is which project and which service a command means.
//
// A change never guesses its project: it is -p, else PETHOST_PROJECT, else the .pethost of
// this folder or of one above it; without any, a picker on a terminal and, off one, a refusal
// that names the full commands. A read may also use the machine's only project. The first line
// on stderr says where the answer came from. The project's workspace is the .pethost's, else the
// one the run was given, else the account's only one (workspace.go); a project that moved to
// another is followed there by a read and not by a change (moved.go).
package target

import (
	"context"
	"os"
	"path/filepath"

	pethost "github.com/pethost-dev/sdk/go"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/ui"
)

// A Source is where a command's project came from.
type Source int

const (
	FromFlag        Source = iota // -p
	FromEnvironment               // PETHOST_PROJECT
	FromFile                      // .pethost
	OnlyProject                   // The machine's only project, for a read.
	Picked                        // Chosen in the picker.
)

// A Project is the project a command means.
type Project struct {
	ID     string
	Source Source
	// FromFile: the .pethost that names it.
	File *File
	// The workspace it is in, as -w gave it or the person picked it; empty when a next command
	// finds it without -w: from the .pethost, PETHOST_WORKSPACE, or the account's only one.
	Workspace string
}

// Flag is what a next command needs to mean the same project: " -p recipes", with " -w Work"
// after it where the workspace was given so, or nothing when it finds the project as this one
// did, in .pethost or PETHOST_PROJECT.
func (p Project) Flag() string {
	flag := ""
	if p.Source != FromFile && p.Source != FromEnvironment {
		flag = " -p " + app.ShellWord(p.ID)
	}
	if p.Workspace != "" {
		flag += " -w " + app.ShellWord(p.Workspace)
	}
	return flag
}

// FlagWorkspace is the workspace a next command names with -w to act where this run did: -w's,
// or the one picked during the run; empty for none.
func FlagWorkspace(a *app.App) string {
	if a.Workspace != "" {
		return a.Workspace
	}
	return a.Chosen
}

// Named is the project that -p, PETHOST_PROJECT or a .pethost names, in that order; false =
// none does. It asks nobody, calls nothing and prints nothing, and notes on the App that the
// project came from a file.
func Named(a *app.App) (Project, bool, error) {
	if project, ok := NamedOutright(a); ok {
		return project, true, nil
	}

	file, err := FindFile(".")
	if file == nil || err != nil {
		return Project{}, false, err
	}
	a.ProjectFromFile = true
	return Project{ID: file.ProjectID, Source: FromFile, File: file}, true, nil
}

// NamedOutright is the project that -p or PETHOST_PROJECT names, whatever a folder says; false =
// neither does.
func NamedOutright(a *app.App) (Project, bool) {
	if a.Project != "" {
		return Project{ID: a.Project, Source: FromFlag, Workspace: FlagWorkspace(a)}, true
	}
	if id := os.Getenv("PETHOST_PROJECT"); id != "" {
		return Project{ID: id, Source: FromEnvironment, Workspace: FlagWorkspace(a)}, true
	}
	return Project{}, false
}

// ForChange is the project a command that changes something means. A .pethost is checked
// against the machine first: one written for another account, or for a project that is gone,
// is asked about and never acted on by itself. A project is a machine's, so a command that means
// one says "not signed in" here, before it asks for anything: a password is not typed for a
// command that cannot use it.
func ForChange(ctx context.Context, a *app.App) (Project, error) {
	if err := a.NeedSignIn(); err != nil {
		return Project{}, err
	}
	a.Changes = true
	a.SayToken()
	project, named, err := Resolve(ctx, a)
	if err != nil {
		return Project{}, err
	}
	if !named {
		return choose(ctx, a, false)
	}

	if project.Source == FromFile {
		if err := checkFile(ctx, a, project.File); err != nil {
			return Project{}, err
		}
	}
	Announce(a, project)
	return project, nil
}

// ForRead is the project a command that only reads means: as ForChange, and with nothing named
// the machine's only project.
func ForRead(ctx context.Context, a *app.App) (Project, error) {
	if err := a.NeedSignIn(); err != nil {
		return Project{}, err
	}
	project, named, err := Resolve(ctx, a)
	if err != nil {
		return Project{}, err
	}
	if !named {
		return choose(ctx, a, true)
	}
	if project.Source == FromFile && project.File.WorkspaceID == "" {
		completeOldFile(ctx, a, project.File)
	}
	Announce(a, project)
	return project, nil
}

// Announce writes where a command's project came from, as its first line on stderr; nothing
// for one the person has just typed or picked.
func Announce(a *app.App, project Project) {
	switch project.Source {
	case FromFile:
		a.UI.Info("%s %s", a.UI.Err.Bold(ui.Clean(project.ID)), a.UI.Err.Dim("("+fileFromHere(project.File)+")"))
	case FromEnvironment:
		a.UI.Info("%s %s", a.UI.Err.Bold(project.ID), a.UI.Err.Dim("(PETHOST_PROJECT)"))
	case OnlyProject:
		a.UI.Info("Using %s, your only project", a.UI.Err.Bold(ui.Clean(project.ID)))
	}
}

// fileFromHere is a .pethost's path as seen from the working directory: ".pethost", or
// "../.pethost" for one in the folder above.
func fileFromHere(file *File) string {
	here, err := os.Getwd()
	if err != nil {
		return FileName
	}
	relative, err := filepath.Rel(here, filepath.Join(file.Dir, FileName))
	if err != nil {
		return FileName
	}
	return filepath.ToSlash(relative)
}

// choose finds the project when nothing names one: the machine's only one for a read, else the
// person's pick, else a refusal that names the command for each.
func choose(ctx context.Context, a *app.App, read bool) (Project, error) {
	client := a.Client()
	machine, err := client.GetMachine(ctx, nil)
	if err != nil {
		return Project{}, err
	}

	switch {
	case len(machine.Projects) == 0:
		return Project{}, fail.Failed("There is no project on your machine yet.").WithLead("In your app's folder, run:", "pethost deploy")
	case read && len(machine.Projects) == 1:
		project := Project{ID: machine.Projects[0].ProjectID, Source: OnlyProject, Workspace: FlagWorkspace(a)}
		Announce(a, project)
		return project, nil
	}

	picked, err := a.UI.Pick("Which project?", projectChoices(a, machine.Projects))
	if err != nil {
		return Project{}, err
	}
	return Project{ID: machine.Projects[picked].ProjectID, Source: Picked, Workspace: FlagWorkspace(a)}, nil
}

func projectChoices(a *app.App, projects []pethost.ProjectSummary) []ui.Choice {
	choices := make([]ui.Choice, len(projects))
	for i, project := range projects {
		choices[i] = ui.Choice{
			Label:   ui.Clean(project.ProjectID),
			Detail:  ui.Clean(Title(project.Metadata)),
			Command: a.CommandLine("-p", project.ProjectID),
		}
	}
	return choices
}

// Title is the name people gave a project, with its emoji: "🥗 Recipe Box"; empty when it has
// neither.
func Title(metadata *pethost.ProjectMetadata) string {
	if metadata == nil {
		return ""
	}
	title := ""
	if metadata.Emoji != nil {
		title = *metadata.Emoji
	}
	if metadata.Name != nil && *metadata.Name != "" {
		if title != "" {
			title += " "
		}
		title += *metadata.Name
	}
	return title
}

// checkFile holds a .pethost against the machine of the workspace it names, else of the account
// signed in. A project that is not there may have moved: the API's refusal says so then.
func checkFile(ctx context.Context, a *app.App, file *File) error {
	client := a.Client()
	machine, err := client.GetMachine(ctx, nil)
	if err != nil {
		return err
	}

	switch state := file.On(machine); state {
	case Current:
		return writeWorkspace(file, machine)
	case Gone:
		if err := MovedAway(ctx, a, file.ProjectID); err != nil {
			return err
		}
		return file.notHere(state, machine)
	case Elsewhere:
		return file.notHere(state, machine)
	case Replaced:
		return file.replaced(a, machine)
	case Namesake:
		yes, err := a.UI.Confirm(file.Says(state, machine)+" Act on it?", a.CommandLine("-p", file.ProjectID))
		if err != nil {
			return err
		}
		if !yes {
			return fail.Declined("Nothing was changed.")
		}
	}
	return nil
}

// completeOldFile writes a .pethost of pethost 0.2, which names no workspace, anew with the
// workspace and the uid of its project, when the machine the run's calls reach has it: else every
// run in the folder of an account with several workspaces would have to be given one. It is a
// read's, which carries on without it: the run's own call says what is wrong.
func completeOldFile(ctx context.Context, a *app.App, file *File) {
	machine, err := a.Client().GetMachine(ctx, nil)
	if err == nil && file.On(machine) == Current {
		_ = writeWorkspace(file, machine)
	}
}

// writeWorkspace writes a .pethost that names no workspace anew for the one whose machine has its
// project (Current), as the first deploy writes it; a file that names one is left as it is.
func writeWorkspace(file *File, machine *pethost.GetMachineResponse) error {
	if file.WorkspaceID != "" || machine.Workspace == nil {
		return nil
	}
	there, ok := FileFrom(file.Dir, machine, file.ProjectID)
	if !ok {
		return nil
	}
	*file = there
	return WriteFile(there)
}
