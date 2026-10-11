package kernel

import (
	"fmt"
	"strings"
	"time"

	pethost "github.com/pethost-dev/sdk/go"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/say"
	"github.com/pethost-dev/cli/internal/ui"
)

// The machine's screen: one line of the machine, then a row for each project with its problems
// under it, then what to type next.

func printMachine(a *app.App, answer *pethost.GetMachineResponse) {
	printMachineLine(a, answer.Machine)
	printProjects(a, answer.Projects)
	sayDeleted(a, answer.DeletedProjects)
}

// printProjects writes a row for each project with its problems under it, then what to type next.
func printProjects(a *app.App, projects []pethost.ProjectSummary) {
	if len(projects) == 0 {
		fmt.Fprintln(a.UI.Out, "No projects yet.")
		a.UI.Info("In your app's folder, run:   %s", a.UI.Err.Bold("pethost deploy"))
		return
	}

	out := a.UI.Out
	table := out.Table()
	for _, project := range projects {
		table.Row(projectCells(out, projects, project)...)
		for _, problem := range project.Problems {
			table.Line(problemLine(out, problem))
		}
	}
	table.Flush()

	if troubled := firstWithProblems(projects); troubled != "" {
		a.UI.Next("pethost why -p " + app.ShellWord(troubled))
	}
}

// sayDeleted names the deleted projects whose snapshots the machine still keeps, and the
// command that shows one's: a deleted project comes back from there, and is listed nowhere else.
func sayDeleted(a *app.App, deleted []pethost.DeletedProject) {
	if len(deleted) == 0 {
		return
	}
	names := make([]string, len(deleted))
	for i, project := range deleted {
		names[i] = ui.Clean(project.ProjectID)
	}
	a.UI.Info("Deleted, with snapshots kept: %s. To bring one back:  %s",
		strings.Join(names, ", "), a.UI.Err.Bold("pethost backup -p "+app.ShellWord(deleted[0].ProjectID)))
}

// printMachineLine writes the machine in one line: "Starter · 2 CPU, 3 GB · sam.pethost.app ·
// memory 1.4 of 3 GB · disk 31% · backups on", and under it a restart that waits.
func printMachineLine(a *app.App, machine *pethost.Machine) {
	if machine == nil {
		return
	}
	out := a.UI.Out

	parts := []string{
		out.Bold(ui.Clean(machine.Name)),
		say.Resources(machine),
	}
	if machine.AppsDomain != "" {
		parts = append(parts, ui.Clean(machine.AppsDomain))
	}
	parts = append(parts,
		"memory "+ui.SizeOf(machine.MemoryUsedBytes, machine.MemoryTotalBytes),
		"disk "+ui.Percent(machine.DiskUsedBytes, machine.DiskTotalBytes),
		backupsWord(out, machine.BackupsEnabled),
	)
	fmt.Fprintln(out, strings.Join(parts, " · "))

	switch {
	case !machine.ScheduledRestartTime.IsZero():
		fmt.Fprintf(out, "%sThe machine restarts %s.\n", out.Tilde(), out.When(machine.ScheduledRestartTime))
	case !machine.RestartRequiredTime.IsZero():
		fmt.Fprintf(out, "%sAn update waits for a restart of the machine:  pethost machine restart\n", out.Tilde())
	}
}

func backupsWord(out *ui.Stream, enabled bool) string {
	if enabled {
		return "backups on"
	}
	return out.Yellow("backups off")
}

// projectCells are a project's row: its emoji and id, what its services do, its address and
// the last day's requests.
func projectCells(out *ui.Stream, all []pethost.ProjectSummary, project pethost.ProjectSummary) []string {
	address := ui.Clean(project.URL)
	if address == "" {
		address = out.Dim("no address")
	}
	return []string{
		emojiColumn(out, all, project) + out.Bold(ui.Clean(project.ProjectID)),
		doing(out, project),
		address,
		traffic(project.HTTPTrafficLastDay),
	}
}

// emojiColumn is the project's emoji before its id on a terminal, where any project of the list
// has one: the ids then start in one column. A program reads the id alone.
func emojiColumn(out *ui.Stream, all []pethost.ProjectSummary, project pethost.ProjectSummary) string {
	if !out.Terminal {
		return ""
	}
	anyHas := false
	for _, other := range all {
		anyHas = anyHas || emojiOf(other.Metadata) != ""
	}
	switch emoji := emojiOf(project.Metadata); {
	case !anyHas:
		return ""
	case emoji == "":
		return "   "
	default:
		return emoji + strings.Repeat(" ", max(1, 3-ui.Width(emoji)))
	}
}

func emojiOf(metadata *pethost.ProjectMetadata) string {
	if metadata == nil || metadata.Emoji == nil {
		return ""
	}
	return ui.Clean(*metadata.Emoji)
}

// doing says what a project's services do: the operation that runs on it now, else how many of
// them are in each state, "3 running", "2 running, 1 restarting".
func doing(out *ui.Stream, project pethost.ProjectSummary) string {
	if len(project.RunningOperations) > 0 {
		operation := project.RunningOperations[0]
		if !out.Terminal {
			return say.Doing(operation.Kind) + " since " + out.At(operation.StartTime)
		}
		return out.Yellow(say.Doing(operation.Kind) + ", " + ui.Rough(time.Since(operation.StartTime)))
	}

	states := make([]pethost.ServiceState, len(project.Services))
	for i, service := range project.Services {
		states[i] = service.State
	}
	return stateCounts(out, states)
}

// stateCounts counts services by what they do, the running ones first.
func stateCounts(out *ui.Stream, states []pethost.ServiceState) string {
	if len(states) == 0 {
		return out.Dim("no services")
	}

	running := 0
	var others []pethost.ServiceState
	counts := map[pethost.ServiceState]int{}
	for _, state := range states {
		if say.Runs(state) {
			running++
			continue
		}
		if counts[state] == 0 {
			others = append(others, state)
		}
		counts[state]++
	}

	var parts []string
	if running > 0 {
		parts = append(parts, out.Green(fmt.Sprintf("%d running", running)))
	}
	for _, state := range others {
		parts = append(parts, stateColor(out, fmt.Sprintf("%d %s", counts[state], say.State(state)), state))
	}
	return strings.Join(parts, ", ")
}

// stateColor paints a state's words: one that waits is yellow, one a person chose is plain,
// anything else that does not run is red.
func stateColor(out *ui.Stream, text string, state pethost.ServiceState) string {
	switch state {
	case pethost.ServiceStateStarting, pethost.ServiceStateNotCreated:
		return out.Yellow(text)
	case pethost.ServiceStateStopped:
		return out.Dim(text)
	}
	return out.Red(text)
}

// traffic is the last day's requests, "212 requests, last 24 h"; nothing when there were none.
func traffic(summary *pethost.HTTPTrafficSummary) string {
	if summary == nil || summary.RequestCount == 0 {
		return ""
	}
	said := ui.Plural(summary.RequestCount, "request")
	if summary.ServerErrorCount > 0 {
		said += ", " + ui.Count(summary.ServerErrorCount) + " failed"
	}
	return said + ", last 24 h"
}

// problemLine is a problem under its project: marked and indented on a terminal.
func problemLine(out *ui.Stream, problem pethost.ProjectProblem) string {
	if !out.Terminal {
		return "  " + say.Problem(out, problem)
	}
	return "     " + out.Cross() + say.Problem(out, problem)
}

func firstWithProblems(projects []pethost.ProjectSummary) string {
	for _, project := range projects {
		if len(project.Problems) > 0 {
			return project.ProjectID
		}
	}
	return ""
}
