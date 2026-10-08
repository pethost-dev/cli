package target

import (
	"context"
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

// A File is a folder's .pethost: two facts, which say whose project the folder is.
type File struct {
	// The project's id.
	ProjectID string `json:"project_id"`
	// The account's domain on Pethost when it was written, e.g. "sam.pethost.app": another one
	// now means another account's folder, or a domain the person has changed since.
	AppsDomain string `json:"apps_domain"`

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
	if err := json.Unmarshal(data, &file); err != nil || file.ProjectID == "" {
		return nil, errors.New(path + " is not a file pethost deploy wrote: remove it, or name the project with -p")
	}
	// The file may have come with someone else's repository: what it says is their text, and is
	// printed in questions and in the commands a refusal names.
	file.ProjectID, file.AppsDomain = ui.Clean(file.ProjectID), ui.Clean(file.AppsDomain)
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

// WriteFile writes a folder's .pethost.
func WriteFile(dir, projectID, appsDomain string) error {
	data, err := json.MarshalIndent(File{ProjectID: projectID, AppsDomain: appsDomain}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, FileName), append(data, '\n'), 0o644)
}

// A State is how a .pethost stands to the machine of the account signed in.
type State int

const (
	// The machine has the project, under the domain the file names.
	Current State = iota
	// The file is this account's, and its project is no longer on the machine: deleted since.
	Gone
	// The file names another domain, and this machine has a project of that id: another
	// account's folder, whose namesake here may be a different project.
	Namesake
	// The file names another domain, and this machine has no project of that id.
	Elsewhere
)

// On says how the file stands to a machine, as GetMachine answered.
func (f *File) On(machine *pethost.GetMachineResponse) State {
	has := slices.ContainsFunc(machine.Projects, func(project pethost.ProjectSummary) bool {
		return project.ProjectID == f.ProjectID
	})
	same := machine.Machine != nil && machine.Machine.AppsDomain == f.AppsDomain

	switch {
	case same && has:
		return Current
	case same:
		return Gone
	case has:
		return Namesake
	}
	return Elsewhere
}

// Says is a state that is not Current as a sentence for the person.
func (f *File) Says(state State, machine *pethost.GetMachineResponse) string {
	id, was, now := ui.Clean(f.ProjectID), ui.Clean(f.AppsDomain), ""
	if machine.Machine != nil {
		now = ui.Clean(machine.Machine.AppsDomain)
	}

	switch state {
	case Gone:
		return fmt.Sprintf("This folder's %s names %s, which is not on your machine any more.", FileName, id)
	case Namesake:
		return fmt.Sprintf("This folder's %s was written for %s on %s. Your machine, %s, has a %s of its own.", FileName, id, was, now, id)
	}
	return fmt.Sprintf("This folder's %s was written for %s on %s, and your machine, %s, has no such project.", FileName, id, was, now)
}

// Explain is a command's failure, said as what it means when the command took its project from
// a .pethost that this machine has no project for: the folder is another account's, or its
// project was deleted. The machine then answers that it has no such project, which is true and
// says nothing of the file. It is asked once more, for its projects, only after such a failure;
// any other failure is returned as it is.
func Explain(ctx context.Context, a *app.App, failure error) error {
	api, ok := errors.AsType[*pethost.Error](failure)
	if !ok || api.Code != pethost.CodeNotFound {
		return failure
	}
	project, named, err := Named(a)
	if err != nil || !named || project.Source != FromFile {
		return failure
	}
	client, err := a.Client()
	if err != nil {
		return failure
	}
	machine, err := client.GetMachine(ctx, nil)
	if err != nil {
		return failure
	}

	state := project.File.On(machine)
	if state != Gone && state != Elsewhere {
		return failure // The machine has the project: something else was not found.
	}
	e := fail.Failed("%s", project.File.Says(state, machine)).WithLead("To put this folder online here, run:", "pethost deploy")
	e.Code = api.Code.String()
	return e
}
