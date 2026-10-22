package kernel

import (
	"fmt"
	"strconv"
	"strings"

	pethost "github.com/pethost-dev/sdk/go"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/say"
	"github.com/pethost-dev/cli/internal/target"
	"github.com/pethost-dev/cli/internal/ui"
)

// One project's screen: its name and address, a row for each service, its problems, the last
// deploy, then what to type next.

func printProject(a *app.App, named target.Project, machine *pethost.Machine, project *pethost.Project) {
	out := a.UI.Out
	fmt.Fprintln(out, projectLine(out, project))

	table := out.Table()
	for _, service := range project.Services {
		table.Row(serviceCells(out, service)...)
	}
	table.Flush()

	for _, problem := range project.Problems {
		fmt.Fprintln(out, out.Cross()+say.Problem(out, problem))
	}
	fmt.Fprintln(out, say.ProjectNow(out, project))

	switch {
	case len(project.Problems) > 0:
		a.UI.Next("pethost why" + named.Flag())
	case project.URL == "":
		sayHowToGiveAddress(a, named, machine, project)
	}
}

// sayHowToGiveAddress names, under a project that has no address and whose services listen, the
// command that gives it one: the machine gives none to a project that publishes no web port and
// listens on several, and a person may have taken it away.
func sayHowToGiveAddress(a *app.App, named target.Project, machine *pethost.Machine, project *pethost.Project) {
	commands := named.AddressCommands(project, appsDomain(machine))
	if len(commands) == 0 {
		return
	}

	a.UI.Info("To give it an address:")
	for _, command := range commands {
		a.UI.Info("  %s", a.UI.Err.Bold(command))
	}
}

// projectLine is the project in one line: "🥗 Recipe Box · https://recipes.sam.pethost.app ·
// 212 requests, last 24 h", with what else a visitor or a deploy meets.
func projectLine(out *ui.Stream, project *pethost.Project) string {
	title := ui.Clean(target.Title(project.Metadata))
	if title == "" {
		title = ui.Clean(project.ProjectID)
	}

	parts := []string{out.Bold(title)}
	if project.URL != "" {
		parts = append(parts, ui.Clean(project.URL))
	} else {
		parts = append(parts, out.Dim("no address"))
	}
	if project.PasswordProtected {
		parts = append(parts, "behind a password")
	}
	if said := traffic(project.HTTPTrafficLastDay); said != "" {
		parts = append(parts, said)
	}
	if from := sourceWords(project.Source); from != "" {
		parts = append(parts, from)
	}
	return strings.Join(parts, " · ")
}

// sourceWords says where a project's files come from when that is a GitHub repository.
func sourceWords(source *pethost.ProjectSource) string {
	if source == nil {
		return ""
	}
	github, ok := source.Kind.(pethost.ProjectSourceGithub)
	if !ok || github.Repository == nil {
		return ""
	}

	said := "from github.com/" + ui.Clean(*github.Repository)
	if github.Pinned {
		said += ", pinned to a commit"
	}
	return said
}

// serviceCells are a service's row: its name, what it does and since when, the ports it
// listens on and the memory it uses.
func serviceCells(out *ui.Stream, service pethost.Service) []string {
	return []string{
		out.Bold(ui.Clean(service.Service)),
		serviceState(out, service),
		serviceSince(out, service),
		ports(service.ListeningPorts),
		memory(service),
	}
}

func serviceState(out *ui.Stream, service pethost.Service) string {
	word := say.State(service.State)
	if service.State == pethost.ServiceStateExited && service.ExitCode != nil {
		word = fmt.Sprintf("exited with code %d", *service.ExitCode)
	}
	if say.Runs(service.State) {
		return out.Green(word)
	}
	return stateColor(out, word, service.State)
}

// serviceSince is when the service's run started, or ended once it did.
func serviceSince(out *ui.Stream, service pethost.Service) string {
	switch {
	case !say.Runs(service.State) && !service.FinishTime.IsZero():
		return "at " + out.At(service.FinishTime)
	case !service.StartTime.IsZero():
		return "since " + out.At(service.StartTime)
	}
	return ""
}

func ports(listening []uint32) string {
	if len(listening) == 0 {
		return ""
	}
	words := make([]string, len(listening))
	for i, port := range listening {
		words[i] = strconv.FormatUint(uint64(port), 10)
	}
	return "listens on " + strings.Join(words, ", ")
}

func memory(service pethost.Service) string {
	if !service.ContainerRunning {
		return ""
	}
	if service.MemoryLimitBytes > 0 {
		return "memory " + ui.SizeOf(service.MemoryUsedBytes, service.MemoryLimitBytes)
	}
	return "memory " + ui.Size(service.MemoryUsedBytes)
}
