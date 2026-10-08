package say

import (
	"fmt"
	"strings"

	pethost "github.com/pethost-dev/sdk/go"

	"github.com/pethost-dev/cli/internal/ui"
)

// ProjectNow is the last line of a project's screen: how many of its services run, what runs
// on it now (a backup left with Ctrl-C is followed here), and how its newest deploy went,
// "3 services run · last deploy succeeded 2 h ago".
func ProjectNow(stream *ui.Stream, project *pethost.Project) string {
	running := 0
	for _, service := range project.Services {
		if Runs(service.State) {
			running++
		}
	}
	parts := []string{servicesRun(running, len(project.Services))}

	deploying := false
	for _, operation := range project.Operations {
		if operation.FinishTime.IsZero() {
			parts = append(parts, Operation(operation.Kind)+" runs since "+stream.At(operation.StartTime))
			deploying = deploying || operation.Kind == pethost.OperationKindDeploy
		}
	}
	if deploying {
		return strings.Join(parts, " · ")
	}

	for _, operation := range project.Operations {
		if operation.Kind == pethost.OperationKindDeploy {
			parts = append(parts, "last deploy "+Ended(operation.Status)+" "+stream.Ago(operation.FinishTime))
			break
		}
	}
	return strings.Join(parts, " · ")
}

func servicesRun(running, all int) string {
	switch {
	case all == 0:
		return "no services"
	case running == all && all == 1:
		return "1 service runs"
	case running == all:
		return fmt.Sprintf("%d services run", all)
	case all == 1:
		return "its service does not run"
	}
	return fmt.Sprintf("%d of %d services run", running, all)
}
