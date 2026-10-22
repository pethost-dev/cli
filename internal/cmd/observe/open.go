package observe

import (
	"context"
	"errors"
	"fmt"
	"strings"

	pethost "github.com/pethost-dev/sdk/go"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/browser"
	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/target"
	"github.com/pethost-dev/cli/internal/ui"
)

// openCommand is `pethost open [service]`.
func openCommand(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "open [service]",
		Short: "Open the site in the browser",
		Long: `Opens the project's address in your browser, and prints it. A project whose services have
addresses of their own opens the one of the service you name. Where there is no browser or no
terminal, the address is only printed.`,
		Example: `  pethost open
  pethost open api
  pethost open -p recipes`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: target.CompleteServices(a),
		RunE: func(cmd *cobra.Command, args []string) error {
			named := ""
			if len(args) == 1 {
				named = args[0]
			}
			return runOpen(cmd, a, named)
		},
	}
}

func runOpen(cmd *cobra.Command, a *app.App, named string) error {
	ctx := cmd.Context()
	project, err := target.ForRead(ctx, a)
	if err != nil {
		return err
	}
	client, err := a.Client()
	if err != nil {
		return err
	}
	answer, err := client.GetProject(ctx, &pethost.GetProjectRequest{ProjectID: project.ID})
	if err != nil {
		return err
	}
	if a.JSON {
		return a.PrintJSON(answer)
	}

	site, err := siteOf(a, project, answer.Project, named)
	if errors.Is(err, errNoAddress) {
		return noAddress(ctx, client, project, answer.Project)
	}
	if err != nil {
		return err
	}
	if site.unavailable != "" {
		a.UI.Problem("%s", ui.Clean(site.unavailable))
	}
	fmt.Fprintln(a.UI.Out, ui.Clean(site.address))

	// A person reads the address on a terminal; a program that takes it wants no browser.
	if a.UI.Out.Terminal && browser.Open(site.address) == nil {
		a.UI.Info("Opened in your browser.")
	}
	return nil
}

// A site is an address a service is reached at: its first route's.
type site struct {
	service string
	address string
	// The machine's reason why the address answers nobody; empty = it answers.
	unavailable string
}

// siteOf is the address to open: the named service's; else the only one there is; else the
// person's pick among the services that have one, or off a terminal a refusal that names the
// command for each.
func siteOf(a *app.App, named target.Project, project *pethost.Project, service string) (site, error) {
	sites := sitesOf(project)
	if service != "" {
		for _, candidate := range sites {
			if candidate.service == service {
				return candidate, nil
			}
		}
		return site{}, fail.Failed("%s of %s has no address: no route sends to it.", ui.Clean(service), ui.Clean(named.ID)).
			WithLead("The addresses the project has:", "pethost domain"+named.Flag())
	}

	switch len(sites) {
	case 0:
		return site{}, errNoAddress
	case 1:
		return sites[0], nil
	}
	return pickSite(a, named, sites)
}

// errNoAddress is siteOf's answer for a project none of whose services has a route.
var errNoAddress = errors.New("the project has no address")

// noAddress is the end of `open` on a project without an address: it names the command that
// gives it one, for each port its services listen on, under the account's domain.
func noAddress(ctx context.Context, client *pethost.Client, named target.Project, project *pethost.Project) error {
	e := fail.Failed("%s has no address.", ui.Clean(named.ID))
	machine, err := client.GetMachine(ctx, nil)
	if err != nil || machine.Machine == nil {
		return e.WithLead("Give it one:", "pethost domain add <host> <service:port>"+named.Flag())
	}

	commands := named.AddressCommands(project, ui.Clean(machine.Machine.AppsDomain))
	if len(commands) == 0 {
		e.Message = ui.Clean(named.ID) + " has no address, and none of its services listens on a port."
		return e
	}
	return e.WithLead("Give it one:", strings.Join(commands, "\n"))
}

// pickSite asks which of several services' addresses is meant, each shown beside its service.
func pickSite(a *app.App, named target.Project, sites []site) (site, error) {
	if !a.UI.CanAsk() {
		commands := make([]string, len(sites))
		for i, candidate := range sites {
			commands[i] = "pethost open " + app.ShellWord(candidate.service) + named.Flag()
		}
		return site{}, fail.Choice("Which service?", commands...)
	}

	choices := make([]ui.Choice, len(sites))
	for i, candidate := range sites {
		choices[i] = ui.Choice{Label: ui.Clean(candidate.service), Detail: ui.Clean(candidate.address)}
	}
	picked, err := a.UI.Pick("Which service?", choices)
	if err != nil {
		return site{}, err
	}
	return sites[picked], nil
}

// sitesOf are the project's addresses, one for each service that has a route, in the routes'
// order: the first is the project's own address.
func sitesOf(project *pethost.Project) []site {
	var sites []site
	seen := map[string]bool{}
	for _, route := range project.Routes {
		if seen[route.Service] {
			continue
		}
		seen[route.Service] = true
		sites = append(sites, routeSite(project, route))
	}
	return sites
}

// routeSite is where a route is reached: its host's address as the machine gives it, which
// says whether the certificate is served yet, and the route's path.
func routeSite(project *pethost.Project, route pethost.Route) site {
	found := site{service: route.Service, address: "https://" + route.Host}
	for _, host := range project.Hosts {
		if host.Host == route.Host {
			found.address, found.unavailable = host.URL, host.UnavailableMessage
		}
	}
	if route.Path != "" && route.Path != "/" {
		found.address = strings.TrimSuffix(found.address, "/") + route.Path
	}
	return found
}
