package kernel

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	pethost "github.com/pethost-dev/sdk/go"

	"github.com/pethost-dev/cli/internal/detect"
	"github.com/pethost-dev/cli/internal/operation"
)

// A Dockerfile that pethost wrote from its own reading of a folder is proven by its first
// deploy: the machine builds it and starts it. One that fails there is the worst thing this
// command could leave behind, since the person was told that their project is known. So it is
// taken back: the folder is as it was, what failed is on the screen, and the file is the
// person's to write, or their coding agent's, which is given the file that failed and the
// machine's reason. A file that was right, for a project that lacks something no file gives
// it (a variable, a table), costs nothing by this: the next deploy writes it again.
//
// A deploy that failed for the machine's own reasons (its disk, its memory, a restart) proves
// nothing of the file, which stays.

// An attempt is what a run wrote for the folder from its own reading: the finding, and the
// files as they were written.
type attempt struct {
	finding detect.Finding
	files   []detect.File
}

// disproves says whether a deploy's end shows that the files it ran are wrong: the image was
// not built, or its container did not start and stay up.
func disproves(ended *pethost.Operation) bool {
	if ended.Status != pethost.OperationStatusFailed {
		return false
	}
	switch ended.DeployFailureReason {
	case pethost.DeployFailureReasonImagePullFailed, pethost.DeployFailureReasonImageBuildFailed,
		pethost.DeployFailureReasonContainerStartFailed, pethost.DeployFailureReasonContainerExited,
		pethost.DeployFailureReasonContainerUnhealthy:
		return true
	}
	return false
}

// takeBack ends a deploy that disproved what this run wrote: the files go, and the person's
// coding agent is offered the folder with what failed. failure is the deploy's own error,
// which stands unless the agent's files are deployed in its place.
func (d *deploy) takeBack(ctx context.Context, result *operation.Result, failure error) error {
	tried := d.own
	d.own = nil

	screen := d.a.UI
	removed := d.remove(tried.files)
	if len(removed) == 0 {
		return failure
	}
	screen.Info("  The %s pethost's own reading of this folder, and the machine could not run it: %s taken back out of the folder.",
		strings.Join(removed, " and ")+wasOrWere(len(removed)), itIsOrTheyAre(len(removed)))

	wrote, left, err := d.haveAgentWrite(ctx, tried.finding, failedTask(tried, result.Failure))
	switch {
	case err != nil:
		return err
	case !wrote:
		if left == writeOne {
			d.sayWayOut(writeOne)
		}
		return failure
	}
	return d.again(ctx)
}

// remove deletes the files a run wrote, each only as it wrote it: one that has changed since is
// somebody's work.
func (d *deploy) remove(files []detect.File) (removed []string) {
	for _, file := range files {
		path := filepath.Join(d.dir, file.Name)
		now, err := os.ReadFile(path)
		if err != nil || string(now) != file.Content {
			continue
		}
		if os.Remove(path) == nil {
			removed = append(removed, file.Name)
		}
	}
	return removed
}

// again deploys the folder as it is now, after a first deploy that failed: the project exists
// since that one, however it ended, and the folder has the .pethost that run wrote and said.
func (d *deploy) again(ctx context.Context) error {
	d.create, d.writeFile = false, false
	if err := d.readFolder(ctx); err != nil {
		return err
	}
	started, err := d.sendAsItIs(ctx)
	if err != nil {
		return err
	}
	return d.watch(ctx, started)
}

// failureLines bounds how much of a failed deploy an agent is told: its newest lines.
const failureLines = 60

// failedTask is what the agent is asked after pethost's own files failed: what pethost read,
// what it wrote, and how the machine's deploy of it ended.
func failedTask(tried *attempt, failure []string) string {
	if len(failure) > failureLines {
		failure = append([]string{failure[0], "…"}, failure[len(failure)-failureLines:]...)
	}

	var task strings.Builder
	task.WriteString("This folder is a project that is being deployed to Pethost, a hosting that builds and runs a folder with Docker. It had no Dockerfile, so the deploy tool wrote one from what the folder declares: ")
	task.WriteString(tried.finding.Project + " (" + strings.Join(tried.finding.Evidence, ", ") + ").\n")
	task.WriteString("\nThe machine could not run it:\n\n")
	for _, line := range failure {
		task.WriteString("  " + line + "\n")
	}
	for _, file := range tried.files {
		task.WriteString("\nThe " + file.Name + " it wrote, which is no longer in the folder:\n\n" + file.Content)
	}
	task.WriteString(whatToWrite)
	task.WriteString("\nIf what failed is the project's own to mend and no Dockerfile can (a variable it needs, an error in its code), write the Dockerfile that is right and say in your last lines what the person has to do.\n")
	return task.String()
}

func wasOrWere(count int) string {
	if count == 1 {
		return " was"
	}
	return " were"
}

func itIsOrTheyAre(count int) string {
	if count == 1 {
		return "it is"
	}
	return "they are"
}
