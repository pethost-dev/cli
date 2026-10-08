package change

import (
	"context"

	pethost "github.com/pethost-dev/sdk/go"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/operation"
	"github.com/pethost-dev/cli/internal/target"
	"github.com/pethost-dev/cli/internal/ui"
)

// An outcome is an operation of a change at its end. Of one that ended well the command then
// says in its own words what is different now.
type outcome struct {
	*operation.Result
	// The answer of the command's last call, which --json prints.
	answer any
}

// started is what a call that starts an operation answered with: the operation, and once it
// has ended the project and the log's end.
type started struct {
	operation *pethost.Operation
	project   *pethost.Project
	log       []pethost.OperationLogLine
	// The call's whole answer.
	answer any
}

// finish takes an operation from the call that started it to its end. A change the machine
// applies at once has ended in that call's answer; any other is watched. One that did not
// succeed is reported in the machine's words and is the error returned, beside its outcome.
func finish(ctx context.Context, a *app.App, project target.Project, call started, words operation.Words) (*outcome, error) {
	done := endedInCall(call)
	if done == nil {
		result, err := operation.Watch(ctx, a, project, call.operation, words.Doing)
		if err != nil {
			return nil, err
		}
		done = &outcome{Result: result, answer: result.Answer}
	}

	if done.Operation.Status != pethost.OperationStatusSucceeded {
		return done, withAnswer(a, done.answer, done.Report(ctx, a, project, words))
	}
	return done, nil
}

// endedInCall is the outcome of an operation that its own call saw end; nil when it still runs.
func endedInCall(call started) *outcome {
	if call.operation == nil || call.operation.FinishTime.IsZero() {
		return nil
	}
	return &outcome{
		Result: &operation.Result{
			Operation: call.operation,
			Project:   call.project,
			Answer:    &pethost.GetOperationResponse{Operation: call.operation, Project: call.project, Log: call.log},
		},
		answer: call.answer,
	}
}

// withAnswer writes a failed call's answer for --json and returns err as it is: the answer says
// more than the error's one line.
func withAnswer(a *app.App, answer any, err error) error {
	if a.JSON {
		if printErr := a.PrintJSON(answer); printErr != nil {
			return printErr
		}
	}
	return err
}

// took is how long the operation ran, in words: "6 s".
func (o *outcome) took() string {
	return ui.Duration(operation.Took(o.Operation))
}

// said ends a change that went well: the machine's own lines and the project's problems under
// the command's line, or with --json the last call's answer.
func (o *outcome) said(a *app.App) error {
	if a.JSON {
		return a.PrintJSON(o.answer)
	}
	o.Notes(a)
	o.Problems(a)
	return nil
}
