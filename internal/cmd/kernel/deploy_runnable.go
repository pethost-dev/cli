package kernel

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	pethost "github.com/pethost-dev/sdk/go"

	"github.com/pethost-dev/cli/internal/detect"
	"github.com/pethost-dev/cli/internal/fail"
)

// A folder the machine found nothing to run in: no compose file and no Dockerfile. Before the
// person is sent away to write one, the CLI makes the folder runnable where it can, in this
// order: the files a row of internal/detect writes for a project of a kind it knows; the
// files a coding agent of theirs writes, where no stack knows the folder or the one that
// does has no row for it (deploy_agent.go). Each says what it wrote, in a line, and the
// folder is then sent again as it is now. What an agent wrote is sent only on the person's
// word: they have not read it yet.
//
// It runs under the machine's refusal only, so what counts as runnable stays the machine's to
// say, and it never touches a file that is there: a folder with a Dockerfile is the person's.
// Nothing is written where that could not be said: with --json and -q the refusal stands.

// sendMadeRunnable makes the folder runnable and sends it again. Where nothing could be
// written the machine's refusal stands, with the way out of it.
func (d *deploy) sendMadeRunnable(ctx context.Context, nothing *nothingToRun) (*pethost.Operation, error) {
	wrote, left, err := d.makeRunnable(ctx, nothing.violations)
	if err != nil {
		return nil, err
	}
	if !wrote {
		d.sayWayOut(left)
		return nil, nothing.refusal
	}

	if err := d.readFolder(ctx); err != nil {
		return nil, err
	}
	started, err := d.sendAsItIs(ctx)
	if again, _ := errors.AsType[*nothingToRun](err); again != nil {
		d.sayWayOut(writeOne)
		return nil, again.refusal
	}
	return started, err
}

// A wayOut is what is left to say when nothing was written for the folder.
type wayOut int

const (
	// Nobody tried, or nobody knew the folder: the person writes the file, or has it written.
	writeOne wayOut = iota
	// What was said already names what to do: a stack's reason, an agent's failure.
	said
)

// sayWayOut ends the refusal of a folder nothing was written for.
func (d *deploy) sayWayOut(left wayOut) {
	screen := d.a.UI
	again := screen.Err.Bold(d.a.CommandLine())
	switch left {
	case said:
		screen.ErrLine("  Nothing was deployed.     How Pethost runs a folder: " + folderDocs)
	default:
		screen.ErrLine(`  Your coding agent writes one in a minute: "Write a Dockerfile for this app".`)
		screen.ErrLine("  Then run " + again + " again.     How Pethost runs a folder: " + folderDocs)
	}
}

// makeRunnable writes what the folder lacks to run; when it wrote nothing, left is what is
// still to say. A coding agent is for a folder no stack knows, and for one a stack leaves to
// be written. Where no file helps until the folder changes, the stack has said what to
// change, and nobody writes.
func (d *deploy) makeRunnable(ctx context.Context, violations []pethost.SpecViolation) (wrote bool, left wayOut, err error) {
	if d.a.UI.Quiet {
		return false, writeOne, nil
	}

	finding := d.find()
	switch {
	case len(finding.Files) > 0:
		return d.write(finding), said, nil
	case finding.Cannot != "":
		return false, said, nil
	}
	return d.haveAgentWrite(ctx, finding, refusedTask(violations, finding))
}

// find says what the folder is, and tells the person why no file is written for it where
// none is. What a row read is not said: it is the first comment of the file it writes.
func (d *deploy) find() detect.Finding {
	finding := detect.Look(os.DirFS(d.dir))
	if why := finding.Cannot + finding.Leaves; why != "" {
		d.a.UI.Info("  %s", why)
	}
	return finding
}

// write writes a finding's files into the folder and says so; false = the Dockerfile was not
// written. A file that is there stays the person's. What was written is this run's own until
// its first deploy has proven it.
func (d *deploy) write(finding detect.Finding) bool {
	screen := d.a.UI
	var wrote []detect.File
	failed := false
	for _, file := range finding.Files {
		err := writeNew(filepath.Join(d.dir, file.Name), file.Content)
		switch {
		case errors.Is(err, fs.ErrExist):
			screen.Info("  %s is here already: it stays as it is.", file.Name)
		case err != nil:
			screen.Problem("%s could not be written here: %s", file.Name, fail.Innermost(err))
			failed = true
		default:
			wrote = append(wrote, file)
		}
	}
	if len(wrote) == 0 {
		return false
	}
	d.own = &attempt{finding: finding, files: wrote}

	screen.Done("%s detected, wrote %s.", finding.Project, strings.Join(namesOf(wrote), " and "))
	for _, note := range finding.Notes {
		screen.Info("%s%s", screen.Err.Tilde(), note)
	}
	return !failed
}

// writeNew writes a file that is not there yet, whole or not at all; fs.ErrExist where one is.
func writeNew(path, content string) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, err = file.WriteString(content)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(path)
	}
	return err
}

func itOrThem(count int) string {
	if count == 1 {
		return "it"
	}
	return "them"
}
