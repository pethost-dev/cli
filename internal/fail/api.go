package fail

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"time"

	pethost "github.com/pethost-dev/sdk/go"

	"github.com/pethost-dev/cli/internal/say"
	"github.com/pethost-dev/cli/internal/ui"
)

// From is any error as the end of a command. The API's error keeps its code and its message,
// the machine's sentence; a typed detail of it, read as fields and never from the text, decides
// the exit code and the CLI's own words where the API's are written for a program.
func From(err error) *Error {
	if e, ok := errors.AsType[*Error](err); ok {
		return e
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, ui.ErrInterrupted) {
		return Interrupted()
	}
	if choice, ok := errors.AsType[*ui.NeedsChoice](err); ok {
		e := Choice(choice.Message, choice.Commands...)
		e.Lead = choice.Lead
		return e
	}
	if api, ok := errors.AsType[*pethost.Error](err); ok {
		return fromAPI(api)
	}
	return &Error{Exit: ExitFailed, Code: "FAILED", Message: err.Error(), cause: err}
}

func fromAPI(api *pethost.Error) *Error {
	e := &Error{Exit: ExitFailed, Code: api.Code.String(), Message: ui.Clean(api.Message), cause: api}

	switch {
	case api.NoMachine != nil:
		e.Exit = ExitPerson
		e.Message = NoMachineMessage(api.NoMachine)
		e.Detail = map[string]any{"no_machine": api.NoMachine}
	case api.WorkspaceChoice != nil:
		// The API's message is written for an agent ("ask the person"); a person reads the
		// workspaces from the detail, and the commands that name one are the run's to write
		// (target.Explain).
		e.Exit = ExitChoice
		e.Message = ChoiceMessage(api.Code, api.WorkspaceChoice)
		e.Detail = map[string]any{"workspace_choice": api.WorkspaceChoice}
	case api.WorkspaceNotGranted != nil:
		e.Exit = ExitPerson
		e.Message = "This sign-in may not reach the workspace " + ui.Clean(api.WorkspaceNotGranted.WorkspaceID) + ": its token was not let into it."
		e.Lead, e.Next = "Let it in the panel:", ui.Clean(api.WorkspaceNotGranted.URL)
		e.Detail = map[string]any{"workspace_not_granted": api.WorkspaceNotGranted}
	case api.ProjectMoved != nil:
		e.Message = MovedMessage(api.ProjectMoved)
		e.Detail = map[string]any{"project_moved": api.ProjectMoved}
	case api.ProjectBusy != nil:
		e.Exit = ExitLater
		e.Message = BusyMessage("The project", api.ProjectBusy)
		e.Detail = map[string]any{"project_busy": api.ProjectBusy}
	case api.MachineUnreachable != nil:
		e.Exit = ExitLater
		e.Message = "Your machine does not answer now, as while it restarts. Try again in a minute."
		e.Detail = map[string]any{"machine_unreachable": api.MachineUnreachable}
	case api.ProjectChanged != nil:
		e.Message = "The project was deployed while this command worked on it. Run the command again."
		e.Detail = map[string]any{"project_changed": api.ProjectChanged}
	case api.Code == pethost.CodeUnauthenticated:
		e.Exit = ExitPerson
		e.Message, e.Next = "This computer's sign-in no longer works.", "pethost login"
		if os.Getenv("PETHOST_TOKEN") != "" {
			e.Message, e.Next = "PETHOST_TOKEN is not a token the panel takes: make a new one in the panel's Settings, under API tokens.", ""
		}
	case api.Code == pethost.CodeUnavailable && api.Unwrap() != nil:
		e.Exit = ExitLater
		e.Message = "Pethost could not be reached: " + Innermost(api).Error() + ". Try again in a minute."
	case api.Unwrap() != nil:
		// A failure below the API under any other code: the connection broke in the middle of
		// an answer, which the SDK reports under the code of a refused request.
		e.Exit = ExitLater
		e.Message = "The connection to Pethost broke: " + Innermost(api).Error() + ". Try again in a minute."
	case api.Code == pethost.CodeUnavailable || api.Code == pethost.CodeDeadlineExceeded:
		// What answered may not be the API at all, as a proxy's "HTTP status 502" while the
		// panel restarts: its words alone say nothing to a person.
		e.Exit = ExitLater
		e.Message = "Pethost does not answer now (" + strings.TrimSuffix(e.Message, ".") + "). Try again in a minute."
	}
	return e
}

// NoMachineMessage says that the account has no machine, with the API's link to where the
// person goes on.
func NoMachineMessage(detail *pethost.NoMachine) string {
	if detail.Reason == pethost.NoMachineReasonBeingPrepared {
		if detail.WorkspaceID != "" {
			return "The machine of the workspace " + ui.Clean(detail.WorkspaceID) + " is being prepared: an email comes when it is ready."
		}
		return "Your machine is being prepared: an email comes when it is ready."
	}
	return "This account has no machine yet. Choose a plan: " + detail.URL
}

// ChoiceMessage says that a call needs a workspace, and which there are: a line for each, its
// name and what say.Workspace says of it, marked where it holds the project the call named.
func ChoiceMessage(code pethost.Code, choice *pethost.WorkspaceChoice) string {
	lines := []string{"This sign-in may act in " + ui.Plural(uint64(choice.Total), "workspace") + ", and the command names none:"}
	if code == pethost.CodeInvalidArgument {
		lines[0] = "The workspace named is none of the ones this sign-in may act in:"
	}
	for _, workspace := range choice.Workspaces {
		line := "  " + ui.Clean(workspace.Name) + ": " + say.Workspace(&workspace)
		if slices.Contains(choice.HoldingProject, workspace.WorkspaceID) {
			line += " · has the project"
		}
		lines = append(lines, line)
	}
	if more := int(choice.Total) - len(choice.Workspaces); more > 0 {
		lines = append(lines, "  and "+ui.Count(uint64(more))+" more: pethost workspaces")
	}
	return strings.Join(lines, "\n")
}

// MovedMessage says where a project moved: "recipes moved to the workspace Work (k7f3q2xy) 2 h
// ago."
func MovedMessage(moved *pethost.ProjectMoved) string {
	return ui.Clean(moved.ProjectID) + " moved to the workspace " + ui.Clean(moved.WorkspaceName) +
		" (" + ui.Clean(moved.WorkspaceID) + ") " + ui.Rough(time.Since(moved.MoveTime)) + " ago."
}

// BusyMessage says what holds a project: "recipes is busy: a deploy started 40 s ago."
func BusyMessage(project string, busy *pethost.ProjectBusy) string {
	since := ui.Rough(time.Since(busy.StartTime))
	if busy.OperationID == "" {
		return project + " is busy with a short action, started " + since + " ago."
	}
	return project + " is busy: " + say.Operation(busy.Kind) + " started " + since + " ago."
}
