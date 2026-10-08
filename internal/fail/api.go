package fail

import (
	"context"
	"errors"
	"os"
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
		return "Your machine is being prepared: an email comes when it is ready."
	}
	return "This account has no machine yet. Choose a plan: " + detail.URL
}

// BusyMessage says what holds a project: "recipes is busy: a deploy started 40 s ago."
func BusyMessage(project string, busy *pethost.ProjectBusy) string {
	since := ui.Rough(time.Since(busy.StartTime))
	if busy.OperationID == "" {
		return project + " is busy with a short action, started " + since + " ago."
	}
	return project + " is busy: " + say.Operation(busy.Kind) + " started " + since + " ago."
}
