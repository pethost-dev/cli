package kernel

import (
	"context"
	"fmt"
	"slices"
	"strings"

	pethost "github.com/pethost-dev/sdk/go"

	"github.com/pethost-dev/cli/internal/operation"
	"github.com/pethost-dev/cli/internal/target"
	"github.com/pethost-dev/cli/internal/ui"
)

// How a deploy ends on the screen: what the machine did, in its words; the address the machine
// gave a project that never had one; what the address answers; the URL alone on stdout; and
// the next thing to type.

func (d *deploy) finish(ctx context.Context, result *operation.Result) error {
	words := operation.Words{Done: d.doneWords(result), Failed: "Deploy failed"}
	if err := result.Report(ctx, d.a, d.project, words); err != nil {
		d.afterFailure()
		if d.a.JSON {
			return d.a.WithAnswer(d.answer, err)
		}
		if d.own != nil && disproves(result.Operation) {
			return d.takeBack(ctx, result, err)
		}
		return err
	}

	if d.a.JSON {
		return d.a.PrintJSON(d.answer)
	}
	d.sayAddress(result)
	d.showAddress(ctx, result.Project)
	d.lastLines(result.Project)
	return nil
}

// doneWords say what a deploy that succeeded did, before " in 41 s". A later deploy's line
// begins with what was sent: "Packed 38 files · uploaded · built and started".
func (d *deploy) doneWords(result *operation.Result) string {
	built := buildsAnImage(result.Project)
	switch {
	case d.create && built:
		return "Built and started"
	case d.create:
		return "Started"
	}

	sent := "Packed " + ui.Plural(uint64(d.packed.files), "file") + " · uploaded · "
	if built {
		return sent + "built and started"
	}
	return sent + "started"
}

// buildsAnImage says whether a service of the project runs an image the machine builds.
func buildsAnImage(project *pethost.Project) bool {
	return project != nil && slices.ContainsFunc(project.Services, func(service pethost.Service) bool {
		return service.BuildsImage
	})
}

// afterFailure says, under a first deploy that failed, that the project exists all the same.
func (d *deploy) afterFailure() {
	if d.writeFile {
		d.a.UI.Info("  Wrote %s: this folder is %s now, and %s sends the fix.",
			target.FileName, ui.Clean(d.project.ID), d.a.UI.Err.Bold("pethost deploy"))
	}
}

// showAddress writes what the project's address answers and, alone on stdout, the address.
func (d *deploy) showAddress(ctx context.Context, project *pethost.Project) {
	if project == nil || project.URL == "" {
		return
	}
	d.a.UI.Info("  %s", answers(ctx, project))

	// A first deploy's address stands apart, where a person reads it.
	apart := d.create && d.a.UI.Err.Terminal
	if apart {
		d.a.UI.Info("")
	}
	fmt.Fprintln(d.a.UI.Out, ui.Clean(project.URL))
	if apart {
		d.a.UI.Info("")
	}
}

// lastLines are the end of the screen: that .pethost was written, and what to type next.
func (d *deploy) lastLines(project *pethost.Project) {
	screen := d.a.UI
	next := d.nextCommands(project)
	for i, command := range next {
		next[i] = screen.Err.Bold(command)
	}

	switch {
	case d.writeFile:
		screen.Info("Wrote %s (commit it). Next:   %s", target.FileName, strings.Join(next, "   ·   "))
	case project != nil && len(project.Problems) > 0:
		screen.Info("Next:   %s", strings.Join(next, "   ·   "))
	}
	if !d.create && d.packed.hasEnv {
		screen.Info("To send this folder's .env:  %s", screen.Err.Bold("pethost env push"+d.project.Flag()))
	}
}

// nextCommands are what to type after a deploy, the likeliest first: what is wrong when
// something is, the logs, and the site when it has an address.
func (d *deploy) nextCommands(project *pethost.Project) []string {
	flag := d.project.Flag()
	var next []string
	if project != nil && len(project.Problems) > 0 {
		next = append(next, "pethost why"+flag)
	}
	next = append(next, "pethost logs -f"+flag)
	if project != nil && project.URL != "" {
		next = append(next, "pethost open"+flag)
	}
	return next
}
