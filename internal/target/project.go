// Package target is which project and which service a command means.
//
// A change never guesses its project: it is -p, else PETHOST_PROJECT, else the .pethost of
// this folder or of one above it; without any, a picker on a terminal and, off one, a refusal
// that names the full commands. A read may also use the machine's only project. The first line
// on stderr says where the answer came from.
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
}

// Flag is what a next command needs to mean the same project: " -p recipes", or nothing when
// it finds the project as this one did, in .pethost or PETHOST_PROJECT.
func (p Project) Flag() string {
	if p.Source == FromFile || p.Source == FromEnvironment {
		return ""
	}
	return " -p " + app.ShellWord(p.ID)
}

// Named is the project that -p, PETHOST_PROJECT or a .pethost names, in that order; false =
// none does. It asks nobody, calls nothing and prints nothing.
func Named(a *app.App) (Project, bool, error) {
	if a.Project != "" {
		return Project{ID: a.Project, Source: FromFlag}, true, nil
	}
	if id := os.Getenv("PETHOST_PROJECT"); id != "" {
		return Project{ID: id, Source: FromEnvironment}, true, nil
	}

	file, err := FindFile(".")
	if file == nil || err != nil {
		return Project{}, false, err
	}
	return Project{ID: file.ProjectID, Source: FromFile, File: file}, true, nil
}

// ForChange is the project a command that changes something means. A .pethost is checked
// against the machine first: one written for another account, or for a project that is gone,
// is asked about and never acted on by itself.
func ForChange(ctx context.Context, a *app.App) (Project, error) {
	if err := signedIn(a); err != nil {
		return Project{}, err
	}
	a.SayToken()
	project, named, err := Named(a)
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
	if err := signedIn(a); err != nil {
		return Project{}, err
	}
	project, named, err := Named(a)
	if err != nil {
		return Project{}, err
	}
	if !named {
		return choose(ctx, a, true)
	}
	Announce(a, project)
	return project, nil
}

// signedIn is nil when the run has a sign-in to call with. A project is a machine's, so a
// command that means one says "not signed in" here, before it asks for anything: a password is
// not typed for a command that cannot use it.
func signedIn(a *app.App) error {
	_, err := a.Client()
	return err
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
	client, err := a.Client()
	if err != nil {
		return Project{}, err
	}
	machine, err := client.GetMachine(ctx, nil)
	if err != nil {
		return Project{}, err
	}

	switch {
	case len(machine.Projects) == 0:
		return Project{}, fail.Failed("There is no project on your machine yet.").WithLead("In your app's folder, run:", "pethost deploy")
	case read && len(machine.Projects) == 1:
		project := Project{ID: machine.Projects[0].ProjectID, Source: OnlyProject}
		Announce(a, project)
		return project, nil
	case !a.UI.CanAsk():
		return Project{}, fail.Choice("Which project?", projectCommands(a, machine.Projects)...)
	}

	picked, err := a.UI.Pick("Which project?", projectChoices(machine.Projects))
	if err != nil {
		return Project{}, err
	}
	return Project{ID: machine.Projects[picked].ProjectID, Source: Picked}, nil
}

func projectCommands(a *app.App, projects []pethost.ProjectSummary) []string {
	commands := make([]string, len(projects))
	for i, project := range projects {
		commands[i] = a.CommandLine("-p", project.ProjectID)
	}
	return commands
}

func projectChoices(projects []pethost.ProjectSummary) []ui.Choice {
	choices := make([]ui.Choice, len(projects))
	for i, project := range projects {
		choices[i] = ui.Choice{Label: ui.Clean(project.ProjectID), Detail: ui.Clean(Title(project.Metadata))}
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

// checkFile holds a .pethost against the machine of the account signed in.
func checkFile(ctx context.Context, a *app.App, file *File) error {
	client, err := a.Client()
	if err != nil {
		return err
	}
	machine, err := client.GetMachine(ctx, nil)
	if err != nil {
		return err
	}

	switch state := file.On(machine); state {
	case Gone, Elsewhere:
		return fail.Failed("%s", file.Says(state, machine)).WithLead("To put this folder online here, run:", "pethost deploy")
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
