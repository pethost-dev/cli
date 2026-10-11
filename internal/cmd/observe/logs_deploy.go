package observe

import (
	"context"
	"errors"
	"iter"

	pethost "github.com/pethost-dev/sdk/go"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/operation"
	"github.com/pethost-dev/cli/internal/target"
	"github.com/pethost-dev/cli/internal/ui"
)

// `pethost logs --deploy`: the log of the project's newest deploy that sent files, or of a
// service's recreation if that came later: what the build, Compose and the machine wrote. It
// is printed whole. With -f, one that still runs is followed to its end, and the command then
// ends as the deploy did.

// deployLog is one run of `pethost logs --deploy`.
type deployLog struct {
	ctx     context.Context
	a       *app.App
	client  *pethost.Client
	project target.Project
	// The deploy, as the project's list of operations had it.
	newest pethost.Operation
}

func runDeployLog(cmd *cobra.Command, a *app.App, flags logFlags) error {
	ctx := cmd.Context()
	if err := noLogOptions(cmd, flags); err != nil {
		return err
	}
	project, err := target.ForRead(ctx, a)
	if err != nil {
		return err
	}
	client, err := a.Client()
	if err != nil {
		return err
	}

	answer, err := client.GetProject(ctx, &pethost.GetProjectRequest{ProjectID: project.ID})
	if err != nil {
		return err
	}
	newest, found := newestDeploy(answer.Project)
	if !found {
		return fail.Failed("The machine keeps no deploy of %s.", ui.Clean(project.ID))
	}

	d := &deployLog{ctx: ctx, a: a, client: client, project: project, newest: newest}
	if flags.follow && newest.Status == pethost.OperationStatusInProgress {
		return d.follow()
	}
	return d.print()
}

// newestDeploy is the deploy whose log says how the project's code came to run: the newest
// deploy, or a service's recreation, which the CLI's own hints also send here. A deploy that
// succeeded without sending files is passed over: it changed the address, the name or the
// password, as the one that gives a new project its address does, or went back to an earlier
// version, and its log is one line or a repeat. When the machine keeps nothing else, the
// newest of those is the one.
func newestDeploy(project *pethost.Project) (pethost.Operation, bool) {
	if project == nil {
		return pethost.Operation{}, false
	}
	var passed *pethost.Operation
	for _, candidate := range project.Operations {
		if candidate.Kind != pethost.OperationKindDeploy && candidate.Kind != pethost.OperationKindRecreateService {
			continue
		}
		if !sentNoFiles(candidate) {
			return candidate, true
		}
		if passed == nil {
			passed = &candidate
		}
	}
	if passed != nil {
		return *passed, true
	}
	return pethost.Operation{}, false
}

// sentNoFiles says whether a deploy succeeded with no archive, no commit and no changed path:
// what it deployed were files the project already had.
func sentNoFiles(deploy pethost.Operation) bool {
	return deploy.Kind == pethost.OperationKindDeploy && deploy.Status == pethost.OperationStatusSucceeded &&
		deploy.ArchiveName == "" && deploy.GithubCommit == nil && deploy.ChangedPathCount == 0
}

// print writes the log as it is now, page by page, then how the deploy ended or that it runs.
func (d *deployLog) print() error {
	request := &pethost.GetOperationRequest{
		ProjectID:    d.project.ID,
		OperationID:  d.newest.OperationID,
		WaitSeconds:  new(uint32(0)),
		AfterLogLine: new(uint32(0)),
		LogLineLimit: new(uint32(pageLines)),
	}
	for {
		page, err := d.client.GetOperation(d.ctx, request)
		if err != nil {
			return err
		}
		if err := d.showPage(page); err != nil {
			return err
		}
		if len(page.Log) == 0 || page.NextAfterLogLine >= page.LogLineCount {
			d.sayEnd(page.Operation)
			return nil
		}
		request.AfterLogLine = new(page.NextAfterLogLine)
	}
}

func (d *deployLog) showPage(page *pethost.GetOperationResponse) error {
	if d.a.JSON {
		return d.a.PrintJSON(page)
	}
	for _, line := range page.Log {
		operationLine(d.a.UI.Out, line.Time, line.Text)
	}
	return nil
}

// sayEnd says, under a log that was only read, how its deploy ended, or that it still runs.
func (d *deployLog) sayEnd(ended *pethost.Operation) {
	if ended == nil {
		return
	}
	screen := d.a.UI
	which := deployWords(ended) + " of " + screen.Err.When(ended.StartTime)

	switch status := ended.Status; {
	case status == pethost.OperationStatusSucceeded:
		screen.Done("%s succeeded in %s.", which, ui.Duration(operation.Took(ended)))
	case status == pethost.OperationStatusFailed && ended.FailureMessage != "":
		screen.Problem("%s failed: %s", which, ui.Clean(ended.FailureMessage))
	case status == pethost.OperationStatusFailed:
		screen.Problem("%s failed.", which)
	case status == pethost.OperationStatusCancelled:
		screen.Info("%s was cancelled.", which)
	case status == pethost.OperationStatusInProgress:
		screen.Info("%s still runs. Follow it:  %s", which, screen.Err.Bold("pethost logs --deploy -f"+d.project.Flag()))
	}
}

// deployWords name the operation whose log this is: "The deploy", "The recreation of web".
func deployWords(deploy *pethost.Operation) string {
	if deploy.Kind == pethost.OperationKindRecreateService {
		return "The recreation of " + ui.Clean(deploy.Service)
	}
	return "The deploy"
}

// errEnded ends the follow of a stream that has said its last word.
var errEnded = errors.New("the stream ended")

// follow prints the log of a deploy that runs, each line as it is written, to the deploy's
// end, then reports the end as the command that started the deploy would.
func (d *deployLog) follow() error {
	// The API sends the log from its first line each time, so a stream that is opened again
	// brings again the lines already shown: shown counts those, streamed the stream's own.
	shown, streamed := 0, 0
	open := func() iter.Seq2[*pethost.WatchOperationResponse, error] {
		streamed = 0
		return d.client.WatchOperation(d.ctx, &pethost.WatchOperationRequest{ProjectID: d.project.ID, OperationID: d.newest.OperationID})
	}

	err := follow(d.ctx, d.a, open, func(response *pethost.WatchOperationResponse) error {
		switch message := response.Message.(type) {
		case pethost.WatchOperationResponseLog:
			streamed++
			if streamed <= shown {
				return nil
			}
			shown = streamed
			return d.showNew(response, pethost.OperationLogLine(message))
		case pethost.WatchOperationResponseFinishedOperation:
			return d.showEnd(response)
		}
		return nil
	})
	if !errors.Is(err, errEnded) {
		return err
	}
	return d.report()
}

func (d *deployLog) showNew(response *pethost.WatchOperationResponse, line pethost.OperationLogLine) error {
	if d.a.JSON {
		return d.a.PrintJSON(response)
	}
	operationLine(d.a.UI.Out, line.Time, line.Text)
	return nil
}

// showEnd takes the stream's last message, the finished operation.
func (d *deployLog) showEnd(response *pethost.WatchOperationResponse) error {
	if d.a.JSON {
		if err := d.a.PrintJSON(response); err != nil {
			return err
		}
	}
	return errEnded
}

// report says how the followed deploy ended, in the words and with the exit code of the
// command that started it: the project as it then is comes with the operation.
func (d *deployLog) report() error {
	answer, err := d.client.GetOperation(d.ctx, &pethost.GetOperationRequest{
		ProjectID: d.project.ID, OperationID: d.newest.OperationID, WaitSeconds: new(uint32(0)),
	})
	if err != nil {
		return err
	}
	if answer.Operation == nil {
		return nil
	}

	which := deployWords(answer.Operation)
	result := &operation.Result{Operation: answer.Operation, Project: answer.Project, Answer: answer, LogShown: true}
	return result.Report(d.ctx, d.a, d.project, operation.Words{Done: which + " succeeded", Failed: which + " failed"})
}
