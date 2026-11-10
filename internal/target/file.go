package target

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	pethost "github.com/pethost-dev/sdk/go"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/ui"
)

// FileName is the file the first deploy writes into a project's folder, to be committed with it.
const FileName = ".pethost"

// A File is a folder's .pethost: the facts that say whose project the folder is. One written by
// pethost 0.2 has the first two alone.
type File struct {
	// The project's id.
	ProjectID string `json:"project_id"`
	// The workspace's domain on Pethost when it was written, e.g. "sam.pethost.app": for a file
	// without WorkspaceID, another one now means another account's folder, or a domain the person
	// has changed since. It is kept right for pethost 0.2, which reads nothing else.
	AppsDomain string `json:"apps_domain"`
	// The workspace the project is in, by its id, which never changes: the calls for the
	// folder's project name it. Empty = not known, and the calls name none.
	WorkspaceID string `json:"workspace_id,omitempty"`
	// The project's own id, which a project of the same id made after it is deleted does not
	// have. Empty = not known.
	UID string `json:"uid,omitempty"`

	// The folder it lies in.
	Dir string `json:"-"`
}

// ReadFile reads the .pethost of one folder; nil = it has none.
func ReadFile(dir string) (*File, error) {
	path := filepath.Join(dir, FileName)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	file := File{Dir: dir}
	err = json.Unmarshal(data, &file)
	if err != nil || file.ProjectID == "" {
		return nil, errors.New(path + " is not a file pethost deploy wrote: remove it, or name the project with -p")
	}
	// The file may have come with someone else's repository: what it says is their text, and is
	// printed in questions and in the commands a refusal names.
	file.ProjectID, file.AppsDomain = ui.Clean(file.ProjectID), ui.Clean(file.AppsDomain)
	file.WorkspaceID, file.UID = ui.Clean(file.WorkspaceID), ui.Clean(file.UID)
	return &file, nil
}

// FindFile reads the .pethost of a folder or of the nearest folder above it; nil = none has one.
func FindFile(dir string) (*File, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	for {
		file, err := ReadFile(dir)
		if file != nil || err != nil {
			return file, err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, nil
		}
		dir = parent
	}
}

// WriteFile writes a folder's .pethost: file's facts, in its Dir.
func WriteFile(file File) error {
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(file.Dir, FileName), append(data, '\n'), 0o644)
}

// FileFrom is the .pethost of a project for the folder dir, from GetMachine's answer for the
// workspace that holds it: the project's ids, the workspace's and its domain. false = the
// machine has no project of that id.
func FileFrom(dir string, machine *pethost.GetMachineResponse, projectID string) (File, bool) {
	file := File{Dir: dir, ProjectID: projectID}
	project := file.summaryOn(machine)
	if project == nil {
		return File{}, false
	}

	file.UID = project.UID
	if machine.Machine != nil {
		file.AppsDomain = machine.Machine.AppsDomain
	}
	if machine.Workspace != nil {
		file.WorkspaceID = machine.Workspace.WorkspaceID
	}
	return file, true
}

// A State is how a .pethost stands to the machine of the account signed in.
type State int

const (
	// The machine has the project, under the domain or in the workspace the file names.
	Current State = iota
	// The file is this account's, and its project is no longer on the machine: deleted, or
	// moved to another workspace, since.
	Gone
	// The file is this account's, and a different project has its id now: the file's was
	// deleted, and another made under the same id.
	Replaced
	// The file names another domain, and this machine has a project of that id: another
	// account's folder, whose namesake here may be a different project.
	Namesake
	// The file names another domain, and this machine has no project of that id.
	Elsewhere
)

// On says how the file stands to a machine, as GetMachine answered.
func (f *File) On(machine *pethost.GetMachineResponse) State {
	project := f.summaryOn(machine)
	same := f.sameWorkspace(machine)

	switch {
	case same && project != nil && f.UID != "" && project.UID != "" && project.UID != f.UID:
		return Replaced
	case same && project != nil:
		return Current
	case same:
		return Gone
	case project != nil:
		return Namesake
	}
	return Elsewhere
}

// summaryOn is the machine's project of the file's id; nil = it has none.
func (f *File) summaryOn(machine *pethost.GetMachineResponse) *pethost.ProjectSummary {
	at := slices.IndexFunc(machine.Projects, func(project pethost.ProjectSummary) bool {
		return project.ProjectID == f.ProjectID
	})
	if at < 0 {
		return nil
	}
	return &machine.Projects[at]
}

// sameWorkspace says whether the machine is the one the file was written for: of the workspace
// it names, or, in a file that names none, under its domain.
func (f *File) sameWorkspace(machine *pethost.GetMachineResponse) bool {
	if f.WorkspaceID != "" && machine.Workspace != nil {
		return machine.Workspace.WorkspaceID == f.WorkspaceID
	}
	return machine.Machine != nil && machine.Machine.AppsDomain == f.AppsDomain
}

// Says is a state that is not Current as a sentence for the person.
func (f *File) Says(state State, machine *pethost.GetMachineResponse) string {
	id, was, now := ui.Clean(f.ProjectID), ui.Clean(f.AppsDomain), ""
	if machine.Machine != nil {
		now = ui.Clean(machine.Machine.AppsDomain)
	}

	switch state {
	case Gone:
		return fmt.Sprintf("This folder's %s names %s, which is not %s any more.", FileName, id, inWorkspace(machine))
	case Replaced:
		return fmt.Sprintf("This folder's %s was written for an earlier %s: a different project has that id %s now.", FileName, id, inWorkspace(machine))
	case Namesake:
		return fmt.Sprintf("This folder's %s was written for %s on %s. Your machine, %s, has a %s of its own.", FileName, id, was, now, id)
	}
	return fmt.Sprintf("This folder's %s was written for %s on %s, and your machine, %s, has no such project.", FileName, id, was, now)
}

// inWorkspace is where a machine's projects are, in a sentence: "in Personal", or "on your
// machine" when the answer names no workspace.
func inWorkspace(machine *pethost.GetMachineResponse) string {
	if machine.Workspace == nil || machine.Workspace.Name == "" {
		return "on your machine"
	}
	return "in " + ui.Clean(machine.Workspace.Name)
}

// notHere is the refusal of a .pethost whose project this machine does not have (Gone,
// Elsewhere), with the command that puts the folder online here.
func (f *File) notHere(state State, machine *pethost.GetMachineResponse) *fail.Error {
	return fail.Failed("%s", f.Says(state, machine)).WithLead("To put this folder online here, run:", "pethost deploy")
}

// SameProject is nil when the project an answer holds is the one a .pethost named it by, as far
// as both know its uid, and otherwise the refusal of the file (Replaced): a read that shows the
// project checks it at no cost.
func SameProject(a *app.App, project Project, got *pethost.Project, machine *pethost.GetMachineResponse) error {
	file := project.File
	if project.Source != FromFile || got == nil {
		return nil
	}
	if file.WorkspaceID == "" {
		_ = writeWorkspace(file, machine) // A read carries on without it, as completeOldFile's.
		return nil
	}
	if file.UID == "" || got.UID == "" || got.UID == file.UID {
		return nil
	}
	return file.replaced(a, machine)
}

// replaced is the refusal of a .pethost whose id a different project has now (Replaced), with the
// command that means that project: a change is never made to it by the folder's file.
func (f *File) replaced(a *app.App, machine *pethost.GetMachineResponse) *fail.Error {
	return fail.Failed("%s", f.Says(Replaced, machine)).WithLead("To act on the project that has the id now, run:", a.CommandLine("-p", f.ProjectID))
}
