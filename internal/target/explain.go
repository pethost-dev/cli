package target

import (
	"context"
	"errors"
	"slices"
	"strings"

	pethost "github.com/pethost-dev/sdk/go"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/say"
	"github.com/pethost-dev/cli/internal/ui"
)

// Explain is a command's failure, said as what it means for the project and the workspace the
// run meant, where the API's answer says it of the call alone: a .pethost whose project the
// machine has not got, a call that needed a workspace, a project that moved. Any other failure
// is returned as it is; a command that worded a workspace's refusal itself keeps its words.
func Explain(ctx context.Context, a *app.App, failure error) error {
	api, ok := errors.AsType[*pethost.Error](failure)
	_, worded := failure.(*fail.Error)
	switch {
	case !ok:
		return failure
	case api.WorkspaceChoice != nil && !worded:
		return explainChoice(a, api)
	case api.ProjectMoved != nil && !worded:
		return explainMoved(ctx, a, api)
	case api.Code == pethost.CodeNotFound && a.ProjectFromFile:
		return explainNotFound(ctx, a, api)
	}
	return failure
}

// explainNotFound says what a NOT_FOUND means when the command took its project from a
// .pethost, as Named noted, that this machine has no project for: the folder is another
// account's, or its project was deleted. The machine then answers that it has no such project,
// which is true and says nothing of the file. It is asked once more, for its projects, only
// after such a failure.
func explainNotFound(ctx context.Context, a *app.App, api *pethost.Error) error {
	project, named, err := Named(a)
	if err != nil || !named || project.Source != FromFile {
		return api
	}
	client := a.Client()
	machine, err := client.GetMachine(ctx, nil)
	if err != nil {
		return api
	}

	state := project.File.On(machine)
	if state != Gone && state != Elsewhere {
		return api // The machine has the project: something else was not found.
	}
	e := project.File.notHere(state, machine)
	e.Code = api.Code.String()
	return e
}

// explainChoice ends a call that needed a workspace with the commands that name each one the
// API listed: only those that hold the project, when the call named one. A workspace the folder's
// .pethost names that is none of this account's is said so.
func explainChoice(a *app.App, api *pethost.Error) error {
	e := fail.From(api)
	choice := api.WorkspaceChoice
	old := oldFile(a) // Before fileWorkspace, whose Named notes a file as the run's.
	if file := fileWorkspace(a); file != nil && api.Code == pethost.CodeInvalidArgument {
		e.Message = "This folder's " + FileName + " names the workspace " + file.WorkspaceID +
			", which is not one of this account's: the folder may be another account's, or the workspace has ended."
		e.Exit, e.Lead, e.Next = fail.ExitFailed, "To put this folder online in one of yours, run:", "pethost deploy"
		return e
	}

	workspaces := choice.Workspaces
	e.Lead = "Name one with -w, or run the command in its project's folder:"
	if old {
		e.Lead = "This folder's " + FileName + " names no workspace: name one with -w (that run writes it into the file)."
	}
	if len(choice.HoldingProject) > 0 {
		workspaces = slices.DeleteFunc(slices.Clone(workspaces), func(workspace pethost.Workspace) bool {
			return !slices.Contains(choice.HoldingProject, workspace.WorkspaceID)
		})
		e.Lead = "The project is in these: name one with -w."
		if old {
			e.Lead = "This folder's " + FileName + " names no workspace, and its project is in these: name one with -w (that run writes it into the file)."
		}
	}

	var commands []string
	for _, workspace := range workspaces {
		commands = append(commands, a.InWorkspace(workspace.WorkspaceID))
	}
	if int(choice.Total) > len(choice.Workspaces) {
		commands = append(commands, "pethost workspaces")
	}
	e.Next = strings.Join(commands, "\n")
	return e
}

// fileWorkspace is the folder's .pethost when the run's calls name the workspace it names; nil
// otherwise.
func fileWorkspace(a *app.App) *File {
	project, named, err := Named(a)
	if err != nil || !named || project.Source != FromFile || project.File.WorkspaceID == "" {
		return nil
	}
	if a.CallsWorkspace() != project.File.WorkspaceID {
		return nil
	}
	return project.File
}

// oldFile says whether the run's project came from a .pethost that names no workspace, as
// pethost 0.2 wrote it: a run whose command means no project writes none.
func oldFile(a *app.App) bool {
	if !a.ProjectFromFile {
		return false
	}
	project, named, err := Named(a)
	return err == nil && named && project.Source == FromFile && project.File.WorkspaceID == ""
}

// PickWorkspace asks which of the workspaces a refusal listed the run goes to. Off a terminal it
// is the commands that name each.
func PickWorkspace(a *app.App, choice *pethost.WorkspaceChoice) (string, error) {
	choices := make([]ui.Choice, len(choice.Workspaces))
	for i, workspace := range choice.Workspaces {
		detail := say.Workspace(&workspace)
		if slices.Contains(choice.HoldingProject, workspace.WorkspaceID) {
			detail += " · has the project"
		}
		choices[i] = ui.Choice{
			Label:   ui.Clean(workspace.Name),
			Detail:  detail,
			Command: a.InWorkspace(workspace.WorkspaceID),
		}
	}

	picked, err := a.UI.Pick("Which workspace?", choices)
	if err != nil {
		return "", err
	}
	return choice.Workspaces[picked].WorkspaceID, nil
}
