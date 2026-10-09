// Package operation runs an operation of the machine (a deploy, a backup, a restore, a delete)
// to its end on the screen: the log's last lines redrawn in place, then how it ended, in the
// machine's words. A project that is busy waits its turn, and Ctrl-C leaves the operation
// running on the machine and says how to follow it.
package operation

import (
	"context"
	"time"

	pethost "github.com/pethost-dev/sdk/go"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/target"
	"github.com/pethost-dev/cli/internal/ui"
)

// A Result is an operation that ended.
type Result struct {
	// How it ended.
	Operation *pethost.Operation
	// The project as it then is; nil once a delete has deleted it.
	Project *pethost.Project
	// The API's answer that both are from: what --json prints.
	Answer *pethost.GetOperationResponse
	// The operation's whole log, as it was streamed, cleaned for a terminal.
	Log []string
	// The command has printed the whole log itself, as `logs --deploy -f` does: a failure's
	// report repeats none of it, and does not say where the log is.
	LogShown bool
	// What a failure's report showed of why: the machine's reason, then the lines that show
	// it. None before Report, after a good end, and under --json.
	Failure []string
}

// Run shows a started operation to its end and reports how it ended: Watch, then Report. A
// command whose words depend on the result calls the two itself.
func Run(ctx context.Context, a *app.App, project target.Project, started *pethost.Operation, words Words) (*Result, error) {
	result, err := Watch(ctx, a, project, started, words.Doing)
	if err != nil {
		return nil, err
	}
	return result, result.Report(ctx, a, project, words)
}

// Watch shows an operation, as the call that started it answered, until it ends: doing is the
// title beside the spinner ("Building…"), and the log's last lines are redrawn under it. Ctrl-C
// leaves the operation running and ends the command with how to follow it.
func Watch(ctx context.Context, a *app.App, project target.Project, started *pethost.Operation, doing string) (*Result, error) {
	client, err := a.Client()
	if err != nil {
		return nil, err
	}
	if started == nil {
		return nil, fail.Failed("The machine started no operation.")
	}

	live := a.UI.Live(doing)
	defer live.Stop()

	log, err := follow(ctx, client, project.ID, started.OperationID, live)
	if ctx.Err() != nil {
		live.Stop()
		return nil, left(a, project, started)
	}
	if err != nil {
		return nil, err
	}

	answer, err := client.GetOperation(ctx, &pethost.GetOperationRequest{
		ProjectID: project.ID, OperationID: started.OperationID, WaitSeconds: new(uint32(0)),
	})
	if err != nil {
		return nil, err
	}
	return &Result{Operation: answer.Operation, Project: answer.Project, Answer: answer, Log: log}, nil
}

// streamRetries is how often in a row a stream that brought nothing new is opened again before
// the failure is the command's: a panel that restarts for its own deploy breaks every stream
// once.
const streamRetries = 5

// follow streams the operation's log until the operation ends. A stream always starts from the
// log's first line, so one opened again skips the lines already shown.
func follow(ctx context.Context, client *pethost.Client, projectID, operationID string, live *ui.Live) ([]string, error) {
	var log []string
	request := &pethost.WatchOperationRequest{ProjectID: projectID, OperationID: operationID}

	broken := 0
	for {
		held := len(log)
		finished, err := followOnce(ctx, client, request, &log, live)
		switch {
		case finished:
			return log, nil
		case ctx.Err() != nil:
			return log, ctx.Err()
		case err != nil && !fail.CallAgain(err):
			return log, orEnded(err)
		}

		// A stream that brought lines did its work before it broke: a long deploy on a poor
		// network may break as often as it likes.
		if len(log) > held {
			broken = 0
		}
		if broken == streamRetries {
			return log, orEnded(err)
		}
		broken++
		if err := pause(ctx); err != nil {
			return log, err
		}
	}
}

// followOnce reads one stream to its end, adding the lines that log does not hold yet.
func followOnce(ctx context.Context, client *pethost.Client, request *pethost.WatchOperationRequest, log *[]string, live *ui.Live) (finished bool, err error) {
	seen := 0
	for response, err := range client.WatchOperation(ctx, request) {
		if err != nil {
			return false, err
		}
		switch message := response.Message.(type) {
		case pethost.WatchOperationResponseLog:
			seen++
			if seen > len(*log) {
				line := ui.Clean(message.Text)
				*log = append(*log, line)
				live.Line(line)
			}
		case pethost.WatchOperationResponseFinishedOperation:
			return true, nil
		}
	}
	return false, nil
}

func orEnded(err error) error {
	if err != nil {
		return err
	}
	return fail.Failed("The operation's log ended before the operation did.").WithNext("pethost status")
}

// left is the end of a command whose person pressed Ctrl-C: the operation goes on, and one line
// says how to follow it.
func left(a *app.App, project target.Project, started *pethost.Operation) error {
	follow := "pethost status" + project.Flag()
	if started.Kind == pethost.OperationKindDeploy || started.Kind == pethost.OperationKindRecreateService {
		follow = "pethost logs --deploy -f" + project.Flag()
	}

	const said = "It keeps running on your machine."
	a.UI.Info("%s Follow it:  %s", said, a.UI.Err.Bold(follow))
	e := fail.Interrupted()
	e.Message, e.Next = said, follow
	return e
}

// Took is how long an operation ran.
func Took(operation *pethost.Operation) time.Duration {
	if operation.FinishTime.IsZero() {
		return time.Since(operation.StartTime)
	}
	return operation.FinishTime.Sub(operation.StartTime)
}
