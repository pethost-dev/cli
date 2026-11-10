package target

import (
	"context"
	"errors"

	pethost "github.com/pethost-dev/sdk/go"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/ui"
)

// A project that moved to another workspace (pethost move, or the panel) is refused in the one it
// left, with where it went (ProjectMoved). A read follows it there; a change stops, and names
// the command that makes it there. Either way a .pethost that named it is written anew for the
// workspace it is in now, when it is the same project: its uid says so.

// Again says whether a run that ended with err runs once more, and with which workspace as if
// given by -w: a call that needed one, where the person can pick it, and a read of a project
// that moved, which follows it. An error is the end in place of err.
func Again(ctx context.Context, a *app.App, err error) (string, bool, error) {
	api, ok := errors.AsType[*pethost.Error](err)
	switch {
	case !ok:
		return "", false, nil
	case api.WorkspaceChoice != nil && api.Code == pethost.CodeFailedPrecondition && a.UI.CanAsk():
		workspace, err := PickWorkspace(a, api.WorkspaceChoice)
		return workspace, err == nil, err
	case api.ProjectMoved != nil && !a.Changes:
		return follow(ctx, a, api.ProjectMoved)
	}
	return "", false, nil
}

// follow takes a read to the workspace its project moved to, unless the run was given its
// workspace outright: then Explain names the command. A folder's .pethost that named it is
// written anew first, for where the project is now; one that named an earlier project of the
// id, or a project that is nowhere now, stops the read.
func follow(ctx context.Context, a *app.App, moved *pethost.ProjectMoved) (string, bool, error) {
	if given, _ := a.GivenWorkspace(); given != "" {
		return "", false, nil
	}
	file := movedFile(a, moved)
	if file == nil {
		a.UI.Info("%s Showing it there.", fail.MovedMessage(moved))
		return moved.WorkspaceID, true, nil
	}

	now, err := rewriteMoved(ctx, a, file, moved)
	if err != nil {
		return "", false, err
	}
	a.UI.Info("%s This folder's %s says so now.", fail.MovedMessage(now), FileName)
	return now.WorkspaceID, true, nil
}

// explainMoved ends a run whose project moved and was not followed: a change, or a read given
// its workspace outright. It names the command that acts there.
func explainMoved(ctx context.Context, a *app.App, api *pethost.Error) error {
	moved := api.ProjectMoved
	e := fail.From(api)
	file := movedFile(a, moved)
	if file != nil {
		now, err := rewriteMoved(ctx, a, file, moved)
		if err != nil {
			return err
		}
		moved = now
		e.Message = fail.MovedMessage(now) + " This folder's " + FileName + " says so now."
		e.Detail = map[string]any{"project_moved": now}
	}

	again := a.InWorkspace(moved.WorkspaceID)
	if given, _ := a.GivenWorkspace(); given == "" && file != nil {
		again = a.CommandLine()
	}
	if a.Changes {
		e.Lead = "Nothing was changed. To change it there, run:"
	} else {
		e.Lead = "To see it there, run:"
	}
	e.Next = again
	return e
}

// movedFile is the folder's .pethost when it names the project that moved; nil otherwise.
func movedFile(a *app.App, moved *pethost.ProjectMoved) *File {
	project, named, err := Named(a)
	if err != nil || !named || project.Source != FromFile || project.ID != moved.ProjectID {
		return nil
	}
	return project.File
}

// movesFollowed bounds the moves a .pethost is followed through: moves that lead back to where
// they started end the search.
const movesFollowed = 4

// rewriteMoved writes a .pethost anew for the workspace its project is in now, as that
// workspace's machine has it, and returns the move that took it there. The project may have moved
// on from the workspace it moved to: the API's refusal there says where, and is followed. A project
// of another uid there is refused: the folder's project was another, deleted before a namesake
// moved. One that is in none of them is said so, and the file is left as it was.
func rewriteMoved(ctx context.Context, a *app.App, file *File, moved *pethost.ProjectMoved) (*pethost.ProjectMoved, error) {
	first := moved
	for range movesFollowed {
		machine, err := a.Client().GetMachine(ctx, &pethost.GetMachineRequest{Workspace: moved.WorkspaceID})
		if err != nil {
			return nil, err
		}

		there, ok := FileFrom(file.Dir, machine, file.ProjectID)
		if ok && file.UID != "" && there.UID != "" && there.UID != file.UID {
			return nil, fail.Failed("This folder's %s was written for an earlier %s: the one that moved is a different project.", FileName, ui.Clean(file.ProjectID)).
				WithLead("To put this folder online again, remove "+FileName+" and run:", "pethost deploy")
		}
		if ok {
			return moved, WriteFile(there)
		}

		moved, err = movedOn(ctx, a, moved)
		if err != nil {
			return nil, err
		}
	}
	return nil, fail.Failed("%s It moved on from there more times than pethost follows.", fail.MovedMessage(first)).
		WithLead("To see which workspace has it, run:", "pethost workspaces -p "+app.ShellWord(moved.ProjectID))
}

// movedOn is where a project went from the workspace it moved to, whose machine has it no more,
// as the API's refusal there says. One that did not move on was deleted there.
func movedOn(ctx context.Context, a *app.App, moved *pethost.ProjectMoved) (*pethost.ProjectMoved, error) {
	_, err := a.Client().GetProject(ctx, &pethost.GetProjectRequest{ProjectID: moved.ProjectID, Workspace: moved.WorkspaceID})
	api, ok := errors.AsType[*pethost.Error](err)
	switch {
	case err == nil:
		return moved, nil // There after all: the machine is asked again.
	case ok && api.ProjectMoved != nil:
		return api.ProjectMoved, nil
	case ok && api.Code == pethost.CodeNotFound:
		e := fail.Failed("%s It is not there now: it was deleted there since.", fail.MovedMessage(moved))
		e.Code = api.Code.String()
		return nil, e.WithLead("To put this folder online again, remove "+FileName+" and run:", "pethost deploy")
	}
	return nil, err
}

// MovedAway is the API's refusal of the project of id when it moved away from the workspace
// the run's calls go to, which a machine's list of projects does not say; nil otherwise.
func MovedAway(ctx context.Context, a *app.App, id string) error {
	_, err := a.Client().GetProject(ctx, &pethost.GetProjectRequest{ProjectID: id})
	if api, ok := errors.AsType[*pethost.Error](err); ok && api.ProjectMoved != nil {
		return err
	}
	return nil
}
