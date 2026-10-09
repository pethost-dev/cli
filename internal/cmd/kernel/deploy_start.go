package kernel

import (
	"context"
	"slices"
	"strings"

	pethost "github.com/pethost-dev/sdk/go"

	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/operation"
	"github.com/pethost-dev/cli/internal/say"
	"github.com/pethost-dev/cli/internal/ui"
)

// The call that starts the deploy: CreateProject for a new project, DeployProject for a new
// version. It answers at once, and the operation it started is then watched. Files the machine
// cannot deploy come back as violations, each printed as the machine says it.

// atOnce asks the API to answer as soon as the deploy started, without waiting for its end.
func atOnce() *uint32 {
	return new(uint32(0))
}

// start starts the deploy of the uploaded folder and returns its operation.
func (d *deploy) start(ctx context.Context, uploadID string) (*pethost.Operation, error) {
	if d.create {
		answer, err := d.client.CreateProject(ctx, &pethost.CreateProjectRequest{
			ProjectID: d.project.ID, UploadID: uploadID, WaitSeconds: atOnce(),
		})
		if err != nil {
			return nil, err
		}
		d.answer = answer
		return answer.Operation, d.refused(answer.Violations, answer.Adjustments)
	}

	answer, err := operation.Queue(ctx, d.a, d.project, *d.noQueue, func() (*pethost.DeployProjectResponse, error) {
		return d.client.DeployProject(ctx, &pethost.DeployProjectRequest{
			ProjectID: d.project.ID, Version: pethost.DeployProjectUploadID(uploadID), WaitSeconds: atOnce(),
		})
	})
	if err != nil {
		return nil, err
	}
	d.answer = answer
	return answer.Operation, d.refused(answer.Violations, answer.Adjustments)
}

// refused is the end of a deploy whose files the machine cannot deploy: nil when it found
// nothing wrong. Each violation is printed where it is and as the machine says it, after what
// the machine had changed on its way there. A folder with nothing to run ends as a
// *nothingToRun, whose last words are its caller's: the folder may yet be made runnable.
func (d *deploy) refused(violations []pethost.SpecViolation, adjustments []string) error {
	if len(violations) == 0 {
		return nil
	}

	e := fail.Failed("The machine cannot deploy these files: %s", say.Violation(violations[0]))
	e.Code = "VIOLATIONS"
	if d.a.JSON {
		return d.printAnswer(e)
	}

	screen := d.a.UI
	for _, adjustment := range adjustments {
		screen.Note("%s", ui.Clean(adjustment))
	}
	for _, violation := range violations {
		screen.ErrLine(screen.Err.Cross() + say.Violation(violation))
	}
	if runsNothing(d.packed.rootNames) {
		return &nothingToRun{refusal: e.AlreadyShown(), violations: violations}
	}
	screen.ErrLine("  Nothing was deployed. Change that in the folder, then run " + screen.Err.Bold(d.a.CommandLine()) + " again.")
	return e.AlreadyShown()
}

// nothingToRun is the machine's refusal of a folder that holds neither a Dockerfile nor a
// compose file: where most newcomers stand.
type nothingToRun struct {
	refusal    *fail.Error
	violations []pethost.SpecViolation
}

func (e *nothingToRun) Error() string { return e.refusal.Error() }

func (e *nothingToRun) Unwrap() error { return e.refusal }

// folderDocs is where the rules for a folder are written: the API's reference of CreateProject.
const folderDocs = "https://pethost.dev/docs/api/#CreateProject"

// runsNothing says whether a folder's root holds neither a Dockerfile nor a file that reads as
// a compose file. It only chooses what follows the machine's own refusal; whether the files can
// run is the machine's to say.
func runsNothing(rootNames []string) bool {
	return !slices.ContainsFunc(rootNames, func(name string) bool {
		lower := strings.ToLower(name)
		isYAML := strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml")
		return lower == "dockerfile" || (isYAML && strings.Contains(lower, "compose"))
	})
}

// printAnswer writes the last call's answer for --json and returns err as it is.
func (d *deploy) printAnswer(err error) error {
	if printErr := d.a.PrintJSON(d.answer); printErr != nil {
		return printErr
	}
	return err
}
