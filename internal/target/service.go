package target

import (
	pethost "github.com/pethost-dev/sdk/go"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/ui"
)

// Service is the service a command means among a project's: the one named, which the machine
// checks; else the project's only one; else the person's pick, or off a terminal a refusal with
// one runnable line for each service, which line writes: "pethost shell web".
func Service(a *app.App, named string, services []string, line func(service string) string) (string, error) {
	switch {
	case named != "":
		return named, nil
	case len(services) == 1:
		return services[0], nil
	case len(services) == 0:
		return "", fail.Failed("The project has no service.")
	}

	choices := make([]ui.Choice, len(services))
	for i, service := range services {
		choices[i] = ui.Choice{Label: ui.Clean(service), Command: line(service)}
	}
	picked, err := a.UI.Pick("Which service?", choices)
	if err != nil {
		return "", err
	}
	return services[picked], nil
}

// ServiceNames are the names of a project's services, as GetProject answered them.
func ServiceNames(project *pethost.Project) []string {
	names := make([]string, len(project.Services))
	for i, service := range project.Services {
		names[i] = service.Service
	}
	return names
}
