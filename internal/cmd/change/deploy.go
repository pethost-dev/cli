package change

import (
	"context"
	"errors"

	pethost "github.com/pethost-dev/sdk/go"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/operation"
	"github.com/pethost-dev/cli/internal/say"
	"github.com/pethost-dev/cli/internal/target"
	"github.com/pethost-dev/cli/internal/ui"
)

// A change that is a deploy: env, domain, password, rename and rollback each send one
// DeployProject. This file is what they share: the call in the project's turn, the machine's
// refusal of what was sent, and the second try when the project was deployed between a
// command's read and its write.

// deployWait is how many seconds the call itself waits for the deploy's end. A change the
// machine applies at once (an address, the password, the name) ends within them, and the
// call's answer is then all there is to it: one round trip. A longer deploy is watched from
// its log's first line.
const deployWait = 2

// deploy sends a change of the project and takes its deploy to the end. request says what
// changes; the project and the wait are filled in here.
func deploy(ctx context.Context, a *app.App, project target.Project, noQueue bool, request *pethost.DeployProjectRequest, words operation.Words) (*outcome, error) {
	client, err := a.Client()
	if err != nil {
		return nil, err
	}

	request.ProjectID = project.ID
	request.WaitSeconds = new(uint32(deployWait))
	answer, err := operation.Queue(ctx, a, project, noQueue, func() (*pethost.DeployProjectResponse, error) {
		live := a.UI.Live("Asking your machine…")
		defer live.Stop()
		return client.DeployProject(ctx, request)
	})
	if err != nil {
		sayMayRunOn(ctx, a, project)
		return nil, err
	}
	if len(answer.Violations) > 0 {
		return nil, refused(a, answer)
	}

	call := started{operation: answer.Operation, project: answer.Project, log: answer.Log, answer: answer}
	return finish(ctx, a, project, call, words)
}

// sayMayRunOn says, when Ctrl-C ended a call that changes the project, that the change may
// have reached the machine all the same: what the machine has started it finishes.
func sayMayRunOn(ctx context.Context, a *app.App, project target.Project) {
	if ctx.Err() != nil {
		a.UI.Info("The change may have reached your machine, where it runs on. See:  %s", a.UI.Err.Bold("pethost status"+project.Flag()))
	}
}

// refused is the end of a change the machine cannot deploy: nothing changed. Each violation is
// printed where it is and as the machine says it, after what the machine had changed on its
// way there.
func refused(a *app.App, answer *pethost.DeployProjectResponse) error {
	e := fail.Failed("The machine cannot deploy this: %s", say.Violation(answer.Violations[0]))
	e.Code = "VIOLATIONS"
	if a.JSON {
		return withAnswer(a, answer, e)
	}

	for _, adjustment := range answer.Adjustments {
		a.UI.Note("%s", ui.Clean(adjustment))
	}
	for _, violation := range answer.Violations {
		a.UI.ErrLine(a.UI.Err.Cross() + say.Violation(violation))
	}
	a.UI.ErrLine("  Nothing changed.")
	return e.AlreadyShown()
}

// againIfChanged runs a change that reads the project and then deploys on what it read. When
// the machine refuses the deploy because the project was deployed in between, the read is old:
// the change runs once more, on a new read.
func againIfChanged(change func() error) error {
	err := change()
	if api, ok := errors.AsType[*pethost.Error](err); ok && api.ProjectChanged != nil {
		return change()
	}
	return err
}
