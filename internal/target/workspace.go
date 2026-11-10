package target

import (
	"context"
	"errors"
	"fmt"

	pethost "github.com/pethost-dev/sdk/go"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/ui"
)

// The workspace of a folder's project. A .pethost names the workspace its project is in, and
// the calls for that project go there; a workspace given outright that is another one is
// refused, never preferred to the file by itself. A .pethost of pethost 0.2 names none, and the
// calls then name what the run was given, else none: the token's only workspace answers.

// UseFolderWorkspace makes the run's calls go to the workspace this folder's .pethost names,
// when the run was given none outright: a command that means no project acts there too. A file
// that cannot be read is said by the command that reads it for its project.
func UseFolderWorkspace(a *app.App) {
	if given, _ := a.GivenWorkspace(); given != "" {
		return
	}
	if file, err := FindFile("."); err == nil && file != nil && file.WorkspaceID != "" {
		a.UseWorkspace(file.WorkspaceID)
	}
}

// Resolve is Named, with the calls for a project named by a .pethost going to the file's
// workspace (UseFileWorkspace).
func Resolve(ctx context.Context, a *app.App) (Project, bool, error) {
	project, named, err := Named(a)
	if err != nil || !named || project.Source != FromFile {
		return project, named, err
	}
	if err := UseFileWorkspace(ctx, a, project.File); err != nil {
		return Project{}, false, err
	}
	return project, true, nil
}

// UseFileWorkspace makes the run's calls go to the workspace a .pethost names. A workspace the
// run was given outright (-w, PETHOST_WORKSPACE) that is another one is refused, with what to
// do; whether it is another is the API's to say, since one may be given by its name.
func UseFileWorkspace(ctx context.Context, a *app.App, file *File) error {
	if file.WorkspaceID == "" {
		return nil
	}

	given, where := a.GivenWorkspace()
	if given != "" && given != file.WorkspaceID {
		same, err := isWorkspace(ctx, a, given, file.WorkspaceID)
		if err != nil {
			return err
		}
		if !same {
			return otherWorkspace(a, file, given, where)
		}
	}
	a.UseWorkspace(file.WorkspaceID)
	return nil
}

// isWorkspace says whether the workspace given, by its id or its name, is the one of id, as the
// API takes it: the machine of the workspace given says which it is, or, while it is being
// prepared, its refusal does.
func isWorkspace(ctx context.Context, a *app.App, given, id string) (bool, error) {
	answer, err := a.Client().GetMachine(ctx, &pethost.GetMachineRequest{Workspace: given})
	if api, ok := errors.AsType[*pethost.Error](err); ok && api.NoMachine != nil && api.NoMachine.WorkspaceID != "" {
		return api.NoMachine.WorkspaceID == id, nil
	}
	if err != nil {
		return false, err
	}
	return answer.Workspace != nil && answer.Workspace.WorkspaceID == id, nil
}

// otherWorkspace is the refusal of a workspace given outright that is not the one the folder's
// .pethost names.
func otherWorkspace(a *app.App, file *File, given, where string) *fail.Error {
	e := fail.Choice(fmt.Sprintf("This folder's %s is for %s in the workspace %s, and %s names another: %s.",
		FileName, ui.Clean(file.ProjectID), file.WorkspaceID, where, ui.Clean(given)))
	e.Code = "OTHER_WORKSPACE"
	e.Lead = "For the folder's project, leave " + where + " out. For a project of that id in " + ui.Clean(given) + ", run:"
	e.Next = a.CommandLine("-p", file.ProjectID)
	return e
}
