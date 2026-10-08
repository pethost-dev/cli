package change

import (
	"context"
	"strings"

	pethost "github.com/pethost-dev/sdk/go"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/say"
	"github.com/pethost-dev/cli/internal/target"
	"github.com/pethost-dev/cli/internal/ui"
)

// restart, stop and start: one action on the services named, or on all the action applies to.
// It is done when its call returns, with the project as it then is.

// serviceWords are one of the three actions in words.
type serviceWords struct {
	// Beside the spinner while the call lasts: "Restarting…".
	doing string
	// What was done to the services, before their names: "Restarted".
	done string
	// The services it acts on when none is named, as the API says: "the running services".
	all string
}

// runServices runs the action and says what the services do now.
func runServices(ctx context.Context, a *app.App, project target.Project, noQueue bool, services []string, action pethost.RunProjectActionAction, words serviceWords) (*pethost.Project, error) {
	answer, err := act(ctx, a, project, noQueue, words.doing, action)
	if err != nil {
		return nil, err
	}
	if a.JSON {
		return answer.Project, a.PrintJSON(answer)
	}

	which := words.all + " of " + ui.Clean(project.ID)
	if len(services) > 0 {
		which = listed(services)
	}
	a.UI.Done("%s %s.", words.done, which)
	sayServices(a, answer.Project)
	return answer.Project, nil
}

// sayServices writes what each service does now, and the project's problems.
func sayServices(a *app.App, project *pethost.Project) {
	if project == nil {
		return
	}
	err := a.UI.Err
	states := make([]string, len(project.Services))
	for i, service := range project.Services {
		state := say.State(service.State)
		if say.Runs(service.State) {
			state = err.Green(state)
		}
		states[i] = ui.Clean(service.Service) + " " + state
	}
	if len(states) > 0 {
		a.UI.Info("  %s", strings.Join(states, " · "))
	}
	for _, problem := range project.Problems {
		a.UI.Problem("%s", say.Problem(err, problem))
	}
}

// again is a command with the services it was given: "pethost start web".
func again(command string, services []string, project target.Project) string {
	for _, service := range services {
		command += " " + word(service)
	}
	return command + project.Flag()
}
