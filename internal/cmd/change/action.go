package change

import (
	"context"

	pethost "github.com/pethost-dev/sdk/go"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/operation"
	"github.com/pethost-dev/cli/internal/target"
)

// A change that is one action of a project (RunProjectAction): restart, stop, start, backup,
// volume, github and delete. Some are done when the call returns; a backup, a restore, a
// recreation, a delete and the deploy a new source starts run as operations.

// act runs one action in the project's turn. doing is what the spinner says while the call
// lasts, which for a stop is as long as the services take to stop.
func act(ctx context.Context, a *app.App, project target.Project, noQueue bool, doing string, action pethost.RunProjectActionAction) (*pethost.RunProjectActionResponse, error) {
	client, err := a.Client()
	if err != nil {
		return nil, err
	}

	answer, err := operation.Queue(ctx, a, project, noQueue, func() (*pethost.RunProjectActionResponse, error) {
		live := a.UI.Live(doing)
		defer live.Stop()

		return client.RunProjectAction(ctx, &pethost.RunProjectActionRequest{
			ProjectID:   project.ID,
			Action:      action,
			WaitSeconds: new(uint32(0)), // An operation answers once it started, and is watched.
		})
	})
	if err != nil {
		sayMayRunOn(ctx, a, project)
	}
	return answer, err
}

// operate runs an action that is an operation and takes the operation to its end.
func operate(ctx context.Context, a *app.App, project target.Project, noQueue bool, action pethost.RunProjectActionAction, words operation.Words) (*outcome, error) {
	answer, err := act(ctx, a, project, noQueue, words.Doing, action)
	if err != nil {
		return nil, err
	}

	call := started{operation: answer.Operation, project: answer.Project, log: answer.Log, answer: answer}
	return finish(ctx, a, project, call, words)
}
