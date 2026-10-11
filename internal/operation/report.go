package operation

import (
	"context"
	"time"

	pethost "github.com/pethost-dev/sdk/go"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/say"
	"github.com/pethost-dev/cli/internal/target"
	"github.com/pethost-dev/cli/internal/ui"
)

// Words are an operation in the words of the command that started it.
type Words struct {
	// Beside the spinner while it runs: "Building…".
	Doing string
	// A good end, before " in 41 s": "Built and started".
	Done string
	// A bad end, before ": " and the machine's reason: "Deploy failed".
	Failed string
}

// Report writes how the operation ended. A good end is "✓ Done in 41 s", the machine's
// adjustments as "·" lines and the project's problems, and nil: a deploy that succeeded with
// problems succeeded. A bad end is the machine's reason, the lines that show it and what the
// project is left with, and the error the command returns.
func (r *Result) Report(ctx context.Context, a *app.App, project target.Project, words Words) error {
	if r.Operation.Status != pethost.OperationStatusSucceeded {
		return r.failed(ctx, a, project, words)
	}

	a.UI.Done("%s in %s", words.Done, ui.Duration(Took(r.Operation)))
	r.Notes(a)
	r.Problems(a)
	return nil
}

// Notes writes what the machine changed that the request did not write, each line its own.
func (r *Result) Notes(a *app.App) {
	for _, adjustment := range r.Operation.Adjustments {
		a.UI.Note("%s", ui.Clean(adjustment))
	}
}

// Problems writes the project's problems, the machine's sentences.
func (r *Result) Problems(a *app.App) {
	if r.Project == nil {
		return
	}
	for _, problem := range r.Project.Problems {
		a.UI.Problem("%s", say.Problem(a.UI.Err, problem))
	}
}

func (r *Result) failed(ctx context.Context, a *app.App, project target.Project, words Words) error {
	operation := r.Operation
	headline := words.Failed + "."
	switch {
	case operation.Status == pethost.OperationStatusCancelled:
		headline = words.Failed + ": it was cancelled."
	case operation.FailureMessage != "":
		headline = words.Failed + ": " + ui.Clean(operation.FailureMessage)
	}

	e := &fail.Error{Exit: fail.ExitFailed, Code: failureCode(operation), Message: headline, Shown: true}
	if isDeploy(operation) && !r.LogShown {
		e.Next = "pethost logs --deploy" + project.Flag()
	}
	if a.JSON {
		return e
	}

	screen, stderr := a.UI, a.UI.Err
	screen.ErrLine(stderr.Cross() + headline)
	r.Failure = []string{headline}
	wholeLog := false
	if operation.Status == pethost.OperationStatusFailed {
		var lines []string
		lines, wholeLog = r.evidence(ctx, a, project)
		for _, line := range lines {
			screen.ErrLine("    " + stderr.Dim(line))
		}
		r.Failure = append(r.Failure, lines...)
	}
	if after := r.aftermath(); after != "" {
		screen.ErrLine("  " + after)
	}
	if e.Next != "" && !wholeLog {
		screen.ErrLine("  Whole log: " + stderr.Bold(e.Next))
	}
	return e
}

func isDeploy(operation *pethost.Operation) bool {
	return operation.Kind == pethost.OperationKindDeploy || operation.Kind == pethost.OperationKindRecreateService
}

// failureCode is a failed operation's kind of failure for --json: the API's reason when it
// gives one.
func failureCode(operation *pethost.Operation) string {
	switch {
	case operation.Status == pethost.OperationStatusCancelled:
		return "CANCELLED"
	case operation.DeployFailureReason != pethost.DeployFailureReasonUnspecified:
		return operation.DeployFailureReason.String()
	}
	return "FAILED"
}

// aftermath says what a deploy that did not succeed left the project with, by the API's own
// field for it.
func (r *Result) aftermath() string {
	switch {
	case r.Operation.Kind != pethost.OperationKindDeploy:
		return ""
	case !r.Operation.MadeCurrent && r.runs():
		return "Nothing changed: the version before still runs."
	case !r.Operation.MadeCurrent:
		return "Nothing changed: the project's files are as they were."
	case r.hasVersionBefore():
		return "This version is the project's now: fix it, or pethost rollback"
	}
	return "These files are the project's now: fix them and run pethost deploy again."
}

// runs says whether a service of the project runs now.
func (r *Result) runs() bool {
	if r.Project == nil {
		return false
	}
	for _, service := range r.Project.Services {
		if say.Runs(service.State) {
			return true
		}
	}
	return false
}

// hasVersionBefore says whether the machine keeps an earlier deploy's files to roll back to.
func (r *Result) hasVersionBefore() bool {
	if r.Project == nil {
		return false
	}
	for _, operation := range r.Project.Operations {
		if operation.FilesKept {
			return true
		}
	}
	return false
}

// evidenceLines is how many lines of a failed service's last run are shown, its newest.
const evidenceLines = 20

// evidence is the lines that show why it failed: what the failed service printed, when a
// container of the new version started and did not stay up; else the operation's own log, whole
// on a terminal and its end, as the API cuts it, elsewhere. wholeLog = the lines are the whole
// log: nothing more of it is left to point to.
func (r *Result) evidence(ctx context.Context, a *app.App, project target.Project) (lines []string, wholeLog bool) {
	if lines := r.serviceOutput(ctx, a, project); len(lines) > 0 {
		return lines, false
	}
	if r.LogShown {
		return nil, false
	}
	if a.UI.Err.Terminal && len(r.Log) > 0 {
		return r.Log, true
	}

	for _, line := range r.Answer.Log {
		lines = append(lines, ui.Clean(line.Text))
	}
	return lines, uint32(len(lines)) == r.Answer.LogLineCount
}

// serviceOutput is what the failed service's container printed in its last run, each line as
// "service | line"; none when the failure is not a container's, or it printed nothing.
func (r *Result) serviceOutput(ctx context.Context, a *app.App, project target.Project) []string {
	operation := r.Operation
	switch operation.DeployFailureReason {
	case pethost.DeployFailureReasonContainerExited, pethost.DeployFailureReasonContainerUnhealthy:
	default:
		return nil
	}
	lastRun := r.lastRunStart(operation.Service)
	client, err := a.Client()
	if err != nil || lastRun.IsZero() {
		return nil
	}

	logs, err := client.QueryContainerLogs(ctx, &pethost.QueryContainerLogsRequest{
		ProjectID: project.ID,
		Filter:    &pethost.ContainerLogFilter{Service: operation.Service},
		StartTime: lastRun,
		Limit:     evidenceLines,
	})
	if err != nil {
		return nil
	}

	var lines []string
	for _, line := range logs.Lines {
		lines = append(lines, ui.Clean(line.Service)+" | "+a.UI.Err.Text(line.Text))
	}
	return lines
}

// lastRunStart is when the service's current or last run started; zero when it is not known.
func (r *Result) lastRunStart(service string) time.Time {
	if r.Project == nil {
		return time.Time{}
	}
	for _, candidate := range r.Project.Services {
		if candidate.Service == service {
			return candidate.StartTime
		}
	}
	return time.Time{}
}
