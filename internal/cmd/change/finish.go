package change

import (
	"context"

	pethost "github.com/pethost-dev/sdk/go"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/operation"
	"github.com/pethost-dev/cli/internal/target"
)

// finish takes an operation from the call that started it, whose answer call holds, to its end.
// A change the machine applies at once has ended in that answer; any other is watched. One that
// did not succeed is reported in the machine's words and is the error returned, beside its result.
// Of one that ended well the command then says in its own words what is different now.
func finish(ctx context.Context, a *app.App, project target.Project, call *operation.Result, words operation.Words) (*operation.Result, error) {
	done := call
	if call.Operation == nil || call.Operation.FinishTime.IsZero() {
		watched, err := operation.Watch(ctx, a, project, call.Operation, words.Doing)
		if err != nil {
			return nil, err
		}
		done = watched
	}

	if done.Operation.Status != pethost.OperationStatusSucceeded {
		return done, a.WithAnswer(done.Answer, done.Report(ctx, a, project, words))
	}
	return done, nil
}
