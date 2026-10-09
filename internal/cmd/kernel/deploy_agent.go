package kernel

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"

	pethost "github.com/pethost-dev/sdk/go"

	"github.com/pethost-dev/cli/internal/agent"
	"github.com/pethost-dev/cli/internal/detect"
	"github.com/pethost-dev/cli/internal/say"
	"github.com/pethost-dev/cli/internal/ui"
)

// The last way to make a folder runnable: a coding agent the person has on this computer reads
// the folder and writes its Dockerfile. It is offered on a terminal only. The person chooses
// the agent once, and from then on it writes without a question: the line that says so names
// the command it is run with and how to turn it off. One run, never a second. What it wrote
// is the person's to look at before it runs anywhere: the folder is sent on their yes.

// haveAgentWrite has the person's coding agent write what the folder lacks, with task as what
// it is asked. wrote = it did, and the person said to deploy it; else left is what is still
// to say.
func (d *deploy) haveAgentWrite(ctx context.Context, finding detect.Finding, task string) (wrote bool, left wayOut, err error) {
	writer, err := d.chooseAgent(finding)
	if err != nil || writer == nil {
		return false, writeOne, err
	}

	screen := d.a.UI
	before := rootNames(d.dir)
	live := screen.Live(writer.Name + " is reading the folder…")
	words, runErr := writer.Run(ctx, d.dir, task)
	live.Stop()
	if ctx.Err() != nil {
		return false, said, ctx.Err()
	}

	after := rootNames(d.dir)
	files := added(before, after)
	switch {
	case runErr != nil:
		screen.Problem("%s", ui.Clean(runErr.Error()))
		if len(files) > 0 {
			screen.Info("  It left %s in the folder: look at %s before the next deploy.", strings.Join(files, " and "), itOrThem(len(files)))
		}
		return false, said, nil
	case runsNothing(after):
		screen.Problem("%s wrote no Dockerfile and no compose file.", writer.Name)
		d.sayWords(words)
		return false, said, nil
	}

	screen.Done("%s wrote %s.", writer.Name, strings.Join(files, " and "))
	d.sayWords(words)
	yes, err := screen.Offer("Deploy it? You have not read " + itOrThem(len(files)) + " yet.")
	if err != nil {
		return false, said, err
	}
	if !yes {
		screen.Info("  Read %s, then run %s again.", strings.Join(files, " and "), screen.Err.Bold(d.a.CommandLine()))
		return false, said, nil
	}
	return true, said, nil
}

// chooseAgent is the agent that writes: the one chosen before, or the one the person chooses
// now among those this computer has. nil = none: nobody is there to choose, nothing is
// installed, or they said not now.
func (d *deploy) chooseAgent(finding detect.Finding) (*agent.Agent, error) {
	screen := d.a.UI
	chosen, ok, byVariable, off := agent.Chosen()
	if off || !screen.CanAsk() {
		return nil, nil
	}
	if ok {
		whose := "as you chose"
		kept := agent.Variable + "=none turns that off; your choice is kept in " + agent.ChoiceFile()
		if byVariable {
			whose, kept = "as "+agent.Variable+" says", ""
		}
		screen.Info("  %s writes what runs it, %s:   %s", chosen.Name, whose, screen.Err.Dim(chosen.CommandLine()))
		if kept != "" {
			screen.Info("  %s", screen.Err.Dim(kept))
		}
		return &chosen, nil
	}

	installed := agent.Installed()
	if len(installed) == 0 {
		return nil, nil
	}
	choices := make([]ui.Choice, 0, len(installed)+1)
	for _, one := range installed {
		choices = append(choices, ui.Choice{Label: one.Name, Detail: one.CommandLine()})
	}
	choices = append(choices, ui.Choice{Label: "Not now", Detail: "I write it myself"})

	if finding.Project == "" {
		screen.Info("  pethost does not know this kind of project.")
	}
	screen.Info("  A coding agent on this computer can write its Dockerfile, now and whenever a folder has none.")
	picked, err := screen.Pick("Which one?", choices)
	if err != nil || picked == len(installed) {
		return nil, err
	}

	chosen = installed[picked]
	if err := chosen.Remember(); err != nil {
		screen.Info("  The choice could not be kept (%s): you will be asked again.", err)
	}
	return &chosen, nil
}

// refusedTask is what the agent is asked for a folder the machine refused: the refusal, and
// what pethost made of the folder itself.
func refusedTask(violations []pethost.SpecViolation, finding detect.Finding) string {
	var task strings.Builder
	task.WriteString("This folder is a project that is being deployed to Pethost, a hosting that builds and runs a folder with Docker. The machine refused the folder:\n\n")
	for _, violation := range violations {
		fmt.Fprintf(&task, "  %s\n", say.Violation(violation))
	}
	if finding.Project != "" {
		fmt.Fprintf(&task, "\nWhat the deploy tool made of the folder: %s (%s).", finding.Project, strings.Join(finding.Evidence, ", "))
		if finding.Leaves != "" {
			fmt.Fprintf(&task, " It wrote no file itself, and said why: %s", finding.Leaves)
		}
		task.WriteString("\n")
	}
	task.WriteString(whatToWrite)
	return task.String()
}

// whatToWrite ends an agent's task: the file to write, and the few things a file for Pethost
// has to mind.
const whatToWrite = `
Write what the folder lacks so that it runs: a Dockerfile at its root. Write a compose.yaml as well only if the project cannot run without a second service, such as a database.

- Read the project's files first: how it is built and started is declared there.
- The program must listen at 0.0.0.0, not at 127.0.0.1: requests come from outside the container.
- The upload leaves out .git and node_modules: the image has neither unless it makes them.
- The project is served at the root of its own address. HTTPS ends at the machine's proxy, which reaches the container over plain HTTP.
- Change nothing else in the folder: the project's own files stay as they are.
- Keep the files short, with a comment where a step is not obvious. A person will read them and keep them.

End with two or three lines that say what you wrote and what, if anything, the person still has to do.
`

// agentWords is how many lines of an agent's own account are shown.
const agentWords = 6

// sayWords shows what the agent said of its work: its last lines, as text.
func (d *deploy) sayWords(said string) {
	lines := strings.Split(strings.TrimSpace(ui.Clean(said)), "\n")
	if len(lines) > agentWords {
		lines = lines[len(lines)-agentWords:]
	}
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			d.a.UI.Info("    %s", d.a.UI.Err.Dim(line))
		}
	}
}

// rootNames are the names in a folder's root; none when it cannot be read.
func rootNames(dir string) []string {
	entries, _ := os.ReadDir(dir)
	names := make([]string, len(entries))
	for i, entry := range entries {
		names[i] = entry.Name()
	}
	return names
}

// added are the names in after that before did not have.
func added(before, after []string) []string {
	var names []string
	for _, name := range after {
		if !slices.Contains(before, name) {
			names = append(names, name)
		}
	}
	return names
}
