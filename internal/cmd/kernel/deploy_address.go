package kernel

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	pethost "github.com/pethost-dev/sdk/go"

	"github.com/pethost-dev/cli/internal/operation"
	"github.com/pethost-dev/cli/internal/ui"
)

// The first address. The machine gives it during the deploy (CreateProject): to each web port
// the folder's compose file publishes, or to the one port a project that publishes none listens
// on. This file says what was given, and tells a project left without an address how to give
// it one; --private asks the machine to give none.

// A listener is a service with a port its processes listen on.
type listener struct {
	service string
	port    uint32
}

// sayAddress says, under a project's first good deploy, where each address the machine gave
// leads; and under any deploy of a project that has none, why and how to give it one.
func (d *deploy) sayAddress(result *operation.Result) {
	project := result.Project
	if project == nil {
		return
	}
	if len(project.Routes) > 0 {
		if !deployedWellBefore(project, result.Operation) {
			d.sayRoutes(project)
		}
		return
	}

	listening := listeners(project)
	switch {
	case len(listening) == 0:
		d.a.UI.Info("  No service of it listens on a port, so it has no address.")
	case d.private:
		d.noAddress(project, "It has no public address, as --private asked. To give it one later:")
		d.sayPublished(project)
	case len(listening) > 1 && !deployedWellBefore(project, result.Operation):
		d.noAddress(project, "More than one port listens, so no address was given. Give it to the one that is the site:")
	default:
		d.noAddress(project, "It has no address. To give it one:")
	}
}

// sayPublished says which machine ports the compose file opens, under a project that asked for
// no address: they answer anyone, whatever --private asked of the proxy.
func (d *deploy) sayPublished(project *pethost.Project) {
	for _, service := range project.Services {
		for _, port := range service.PublishedPorts {
			d.a.UI.Info("  The compose file publishes %s's port %d, so it is open on the machine all the same, at port %d.",
				ui.Clean(service.Service), port.ContainerPort, port.MachinePort)
		}
	}
}

// sayRoutes says where each host of the project leads: "web listens on 3000 → https://…".
func (d *deploy) sayRoutes(project *pethost.Project) {
	said := map[string]bool{}
	for _, route := range project.Routes {
		if said[route.Host] {
			continue
		}
		said[route.Host] = true
		d.a.UI.Done("%s listens on %d → %s", ui.Clean(route.Service), route.Port, ui.Clean(hostURL(project, route.Host)))
	}
}

// hostURL is the host as the project's hosts give its URL.
func hostURL(project *pethost.Project, host string) string {
	for _, h := range project.Hosts {
		if h.Host == host {
			return h.URL
		}
	}
	return host
}

// deployedWellBefore says whether a deploy other than this one has succeeded, among those the
// machine keeps: the project then had its chance at an address, and having none is a choice.
func deployedWellBefore(project *pethost.Project, this *pethost.Operation) bool {
	for _, operation := range project.Operations {
		if operation.Kind == pethost.OperationKindDeploy && operation.Status == pethost.OperationStatusSucceeded &&
			operation.OperationID != this.OperationID {
			return true
		}
	}
	return false
}

func listeners(project *pethost.Project) []listener {
	var listening []listener
	for _, service := range project.Services {
		for _, port := range service.ListeningPorts {
			listening = append(listening, listener{service: service.Service, port: port})
		}
	}
	return listening
}

// noAddress says why the project has no address, with one runnable line for each port that
// listens.
func (d *deploy) noAddress(project *pethost.Project, why string) {
	screen := d.a.UI
	screen.Info("  %s", why)
	for _, command := range d.project.AddressCommands(project, d.appsDomain()) {
		screen.Info("    %s", screen.Err.Bold(command))
	}
}

// answerWait bounds the one request that asks the address what it answers.
const answerWait = 10 * time.Second

// answers asks the project's address once and says what came back, as seen: "It answers 200."
func answers(ctx context.Context, project *pethost.Project) string {
	ctx, cancel := context.WithTimeout(ctx, answerWait)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, project.URL, nil)
	if err != nil {
		return "It was not asked: its address does not read as one."
	}
	request.Header.Set("User-Agent", "pethost-cli")

	// The first answer is the one to report: a redirect is an answer too.
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return "It does not answer yet: " + plainCause(err) + "."
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusUnauthorized && project.PasswordProtected {
		return "It answers 401: its password page."
	}
	return fmt.Sprintf("It answers %d.", response.StatusCode)
}

// plainCause is why a request got no answer, without its address.
func plainCause(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Sprintf("nothing came in %s", ui.Duration(answerWait))
	}
	if wrapped, ok := errors.AsType[*url.Error](err); ok {
		err = wrapped.Err
	}
	return strings.TrimSpace(err.Error())
}
