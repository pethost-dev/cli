package fail

import (
	"os"
	"strings"

	"github.com/pethost-dev/sdk/go/sandboxes"

	"github.com/pethost-dev/cli/internal/ui"
)

// fromSandboxes is an error of the Agent Sandboxes API as the end of a command, as fromAPI is
// App Platform's: the API's code and sentence, and its typed detail, read as fields, for the
// exit code and the words a person reads where the API's are written for a program. What the
// command can add, as the sandbox's id in the next command, its own package adds.
func fromSandboxes(api *sandboxes.Error) *Error {
	e := &Error{Exit: ExitFailed, Code: api.Code.String(), Message: ui.Clean(api.Message), cause: api}

	switch {
	case api.WorkspaceChoice != nil:
		e.Exit = ExitChoice
		e.Message = SandboxesChoiceMessage(api.Code, api.WorkspaceChoice)
		e.Detail = map[string]any{"workspace_choice": api.WorkspaceChoice}
	case api.LimitReached != nil:
		e.Detail = map[string]any{"limit_reached": api.LimitReached}
	case api.Stopped != nil:
		e.Detail = map[string]any{"stopped": api.Stopped}
	case api.UsageUnpaid != nil:
		e.Exit = ExitPerson
		e.Detail = map[string]any{"usage_unpaid": api.UsageUnpaid}
	case api.AgentSignIn != nil:
		e.Exit = ExitPerson
		e.Detail = map[string]any{"agent_sign_in": api.AgentSignIn}
	case api.Restoring != nil:
		e.Exit = ExitLater
		e.Detail = map[string]any{"restoring": api.Restoring}
	case api.NoCapacity != nil:
		e.Exit = ExitLater
		e.Detail = map[string]any{"no_capacity": api.NoCapacity}
	default:
		sandboxesByCode(e, api)
	}
	return e
}

// sandboxesByCode words a refusal that came with no detail by its code alone.
func sandboxesByCode(e *Error, api *sandboxes.Error) {
	switch {
	case api.Code == sandboxes.CodeUnauthenticated && api.Unwrap() == nil:
		e.Exit = ExitPerson
		e.Message, e.Next = "This computer's sign-in no longer works.", "pethost login"
		if os.Getenv("PETHOST_TOKEN") != "" {
			e.Message, e.Next = "PETHOST_TOKEN is not a token the panel takes: make a new one in the panel's Settings, under API tokens.", ""
		}
	case api.Code == sandboxes.CodePermissionDenied && api.Unwrap() == nil:
		e.Exit = ExitPerson
	case api.Code == sandboxes.CodeUnavailable && api.Unwrap() != nil:
		e.Exit = ExitLater
		e.Message = "Pethost could not be reached: " + Innermost(api).Error() + ". Try again in a minute."
	case api.Unwrap() != nil:
		// A failure below the API under any other code: the connection broke in the middle of
		// an answer, which the SDK reports under the code of a refused request.
		e.Exit = ExitLater
		e.Message = "The connection to Pethost broke: " + Innermost(api).Error() + ". Try again in a minute."
	case api.Code == sandboxes.CodeUnavailable || api.Code == sandboxes.CodeDeadlineExceeded:
		e.Exit = ExitLater
	case api.Code == sandboxes.CodeResourceExhausted, api.Code == sandboxes.CodeAborted:
		// Without LimitReached, a bound that frees by itself: a call past what a sandbox serves
		// at once, starts past the plan's rate, or a change that met another.
		e.Exit = ExitLater
	}
}

// SandboxesChoiceMessage says that a call needs a sandboxes workspace, and which there are: a
// line for each, its name and id.
func SandboxesChoiceMessage(code sandboxes.Code, choice *sandboxes.WorkspaceChoice) string {
	lines := []string{"This sign-in may act in " + ui.Plural(uint64(choice.Total), "workspace") + " of sandboxes, and the command names none:"}
	if code == sandboxes.CodeInvalidArgument {
		lines[0] = "The workspace named is none of the workspaces of sandboxes this sign-in may act in:"
	}
	for _, workspace := range choice.Workspaces {
		lines = append(lines, "  "+ui.Clean(workspace.Name)+": "+ui.Clean(workspace.WorkspaceID))
	}
	if more := int(choice.Total) - len(choice.Workspaces); more > 0 {
		lines = append(lines, "  and "+ui.Count(uint64(more))+" more")
	}
	return strings.Join(lines, "\n")
}
