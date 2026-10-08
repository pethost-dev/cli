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
	"github.com/pethost-dev/cli/internal/say"
	"github.com/pethost-dev/cli/internal/ui"
)

// The first address. A project whose files declare no route, and which has never been deployed
// well before, gets `<id>.<the account's domain>` without being asked, when the machine reports
// one service listening on one port: there is then one thing the address can mean. The same on
// a terminal and off one; --private skips it.
//
// ponytail: this rule is the product's, and its lasting home is the machine's CreateProject,
// where agents get it too. Until it is there the CLI holds it, in this file alone: delete the
// file's first half when CreateProject routes by itself.

// A listener is a service with a port its processes listen on.
type listener struct {
	service string
	port    uint32
}

// giveAddress routes the project's first address, and returns the project as it then is; nil =
// nothing was changed, and a project left without an address is told how to give it one.
func (d *deploy) giveAddress(ctx context.Context, result *operation.Result) (*pethost.Project, error) {
	project := result.Project
	if project == nil || len(project.Routes) > 0 {
		return nil, nil
	}

	listening := listeners(project)
	switch {
	case len(listening) == 0:
		d.a.UI.Info("  No service of it listens on a port, so it has no address.")
	case d.private:
		d.noAddress(project, "It has no public address, as --private asked. To give it one later:")
	case deployedWellBefore(project, result.Operation) || d.appsDomain() == "":
		d.noAddress(project, "It has no address. To give it one:")
	case len(listening) > 1:
		d.noAddress(project, "More than one port listens, so no address was given. Give it to the one that is the site:")
	default:
		return d.route(ctx, project, listening[0])
	}
	return nil, nil
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

func (l listener) String() string {
	return fmt.Sprintf("%s:%d", l.service, l.port)
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

// firstHost is the address a project gets first: its id under the account's domain.
func (d *deploy) firstHost(project *pethost.Project) string {
	return project.ProjectID + "." + d.machine.Machine.AppsDomain
}

// route gives the project its address: a deploy that changes x-pethost alone, which the machine
// applies at once.
func (d *deploy) route(ctx context.Context, project *pethost.Project, to listener) (*pethost.Project, error) {
	routes := []pethost.Route{{Host: d.firstHost(project), Service: to.service, Port: to.port}}
	answer, err := operation.Queue(ctx, d.a, d.project, *d.noQueue, func() (*pethost.DeployProjectResponse, error) {
		return d.client.DeployProject(ctx, &pethost.DeployProjectRequest{
			ProjectID: project.ProjectID,
			XPethost:  &pethost.ProjectExtension{Routes: routes},
		})
	})
	if err != nil {
		return nil, err
	}
	d.answer = answer

	screen := d.a.UI
	switch {
	case len(answer.Violations) > 0:
		screen.Problem("It got no address: %s", say.Violation(answer.Violations[0]))
		return nil, nil
	case answer.Project == nil || answer.Operation == nil || answer.Operation.Status != pethost.OperationStatusSucceeded:
		screen.Problem("It got no address yet:  pethost domain add %s %s", routes[0].Host, to)
		return nil, nil
	}
	screen.Done("%s listens on %d → %s", ui.Clean(to.service), to.port, ui.Clean(answer.Project.URL))
	return answer.Project, nil
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
