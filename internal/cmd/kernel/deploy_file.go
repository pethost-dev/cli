package kernel

import (
	"context"

	pethost "github.com/pethost-dev/sdk/go"

	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/target"
)

// The folder's .pethost: the workspace it names is where the deploy goes, and the deploy writes
// it once the machine has taken the deploy, completed by the deploy's answer with what only the
// answer knows of the project, its uid and its workspace.

// useFolderWorkspace sends the deploy to the workspace the folder's .pethost names, when the file
// names the project.
func (d *deploy) useFolderWorkspace(ctx context.Context) error {
	if d.file == nil || d.namedOutside() {
		return nil
	}
	if err := target.UseFileWorkspace(ctx, d.a, d.file); err != nil {
		return err
	}
	d.client = d.a.Client()
	return nil
}

// useWorkspace sends the deploy's calls from now on to a workspace; empty = to none named.
func (d *deploy) useWorkspace(workspace string) {
	d.a.UseWorkspace(workspace)
	d.client = d.a.Client()
}

// rememberFolder writes the folder's .pethost once the machine has taken the deploy: from then
// on the project exists, however its first deploy ends. The project's uid is not known before
// its deploy ends: completeFile adds it.
func (d *deploy) rememberFolder() error {
	if !d.writeFile {
		if d.file != nil && d.project.Source == target.FromFile {
			d.kept = d.file
		}
		return nil
	}

	file := target.File{Dir: d.dir, ProjectID: d.project.ID}
	if d.machine.Machine != nil {
		file.AppsDomain = d.machine.Machine.AppsDomain
	}
	if d.machine.Workspace != nil {
		file.WorkspaceID = d.machine.Workspace.WorkspaceID
	}
	if err := target.WriteFile(file); err != nil {
		return fail.Failed("The deploy runs, but %s could not be written here: %s", target.FileName, err).
			WithLead("Follow it:", "pethost logs --deploy -f"+d.project.Flag())
	}
	d.kept = &file
	return nil
}

// completeFile writes into the .pethost the deploy keeps what its answer says of the project
// and the file lacks: the uid of a project it made, and the uid and the workspace that a file
// of pethost 0.2 has not. A file the answer adds nothing to is left as it is.
func (d *deploy) completeFile(project *pethost.Project) error {
	kept := d.kept
	if kept == nil || project == nil || project.ProjectID != kept.ProjectID {
		return nil
	}
	file := *kept
	if file.UID == "" {
		file.UID = project.UID
	}
	if file.WorkspaceID == "" {
		file.WorkspaceID = project.WorkspaceID
	}
	if file == *kept {
		return nil
	}

	if err := target.WriteFile(file); err != nil {
		d.a.UI.Problem("%s could not be written here: %s", target.FileName, err)
		return nil
	}
	d.kept = &file
	return nil
}
