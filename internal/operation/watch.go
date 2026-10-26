// Package operation runs an operation of the machine (a deploy, a backup, a restore, a delete)
// to its end on the screen: the log's last lines redrawn in place, then how it ended, in the
// machine's words. A project that is busy waits its turn, and Ctrl-C leaves the operation
// running on the machine and says how to follow it.
package operation

import (
	"context"
	"errors"
	"iter"
	"time"

	pethost "github.com/pethost-dev/sdk/go"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/stream"
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

	var log []string
	err = FollowLog(ctx, a, project.ID, started.OperationID, func(response *pethost.WatchOperationResponse) error {
		if message, ok := response.Message.(pethost.WatchOperationResponseLog); ok {
			line := ui.Clean(message.Text)
			log = append(log, line)
			live.Line(line)
		}
		return nil
	})
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

// errFinished ends the follow of a log whose stream has said its last word.
var errFinished = errors.New("the operation finished")

// FollowLog streams an operation's log until the operation ends: each takes every new line, then
// the finished operation. A stream always starts from the log's first line, so one opened again
// skips the lines each has had, which bring the follow no nearer its end (newLines). It returns
// nil when the operation ended or the person left with Ctrl-C, which ctx tells apart, and
// otherwise the follow's failure.
func FollowLog(ctx context.Context, a *app.App, projectID, operationID string, each func(*pethost.WatchOperationResponse) error) error {
	client, err := a.Client()
	if err != nil {
		return err
	}
	request := &pethost.WatchOperationRequest{ProjectID: projectID, OperationID: operationID}

	given := 0 // the lines each has had
	open := func() iter.Seq2[*pethost.WatchOperationResponse, error] {
		return newLines(client.WatchOperation(ctx, request), &given)
	}
	err = stream.Follow(ctx, a, open, func(response *pethost.WatchOperationResponse) error {
		if err := each(response); err != nil {
			return err
		}
		if _, finished := response.Message.(pethost.WatchOperationResponseFinishedOperation); finished {
			return errFinished
		}
		return nil
	})
	if errors.Is(err, errFinished) {
		return nil
	}
	return err
}

// newLines is a stream of an operation's log without the lines given counts, which it counts on,
// and without a response that is neither a line nor the end: the follower takes what it passes
// for the stream's progress, and a stream opened again that sends only what was had is none.
func newLines(responses iter.Seq2[*pethost.WatchOperationResponse, error], given *int) iter.Seq2[*pethost.WatchOperationResponse, error] {
	return func(yield func(*pethost.WatchOperationResponse, error) bool) {
		streamed := 0
		for response, err := range responses {
			if err == nil {
				switch response.Message.(type) {
				case pethost.WatchOperationResponseLog:
					streamed++
					if streamed <= *given {
						continue
					}
					*given = streamed
				case nil:
					continue
				}
			}
			if !yield(response, err) {
				return
			}
		}
	}
}

// left is the end of a command whose person pressed Ctrl-C: the operation goes on, and one line
// says how to follow it.
func left(a *app.App, project target.Project, started *pethost.Operation) error {
	follow := "pethost status" + project.Flag()
	if isDeploy(started) {
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
