package change

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	pethost "github.com/pethost-dev/sdk/go"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/operation"
	"github.com/pethost-dev/cli/internal/target"
	"github.com/pethost-dev/cli/internal/ui"
)

// domainAddCommand is `pethost domain add <host> [service:port]`: one more address, sent as
// the project's routes as they are plus the new one. For a domain of the person's own it ends
// with the DNS record that is theirs to make.
func domainAddCommand(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add <host> [service:port]",
		Short: "Give the project an address",
		Long: `Give the project an address. Without service:port it leads where the project's
addresses already lead, or to the one port a service listens on.

A name under your Pethost domain works at once. For a domain of your own the
command prints the one DNS record to make; the certificate then comes by itself.`,
		Example: `  pethost domain add recipes.example.com
  pethost domain add api.example.com api:8000`,
		Args: cobra.RangeArgs(1, 2),
	}
	noQueue := operation.NoQueueFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		project, err := target.ForChange(cmd.Context(), a)
		if err != nil {
			return err
		}
		d := &domainAdd{a: a, project: project, noQueue: *noQueue, host: args[0], usage: cmd.UseLine()}
		if len(args) == 2 {
			d.to = args[1]
		}
		return d.run(cmd.Context())
	}
	return cmd
}

// A domainAdd is one run of the command.
type domainAdd struct {
	a       *app.App
	project target.Project
	noQueue bool
	usage   string
	host    string
	// Where the address leads, "service:port" or "service": as typed, or as picked.
	to string

	machine *pethost.GetMachineResponse
}

var routeWords = operation.Words{Doing: "Changing the addresses…", Failed: "The addresses were not changed"}

func (d *domainAdd) run(ctx context.Context) error {
	var route pethost.Route
	var before *pethost.Project
	var done *outcome
	err := againIfChanged(func() error {
		var err error
		before, err = d.read(ctx)
		if err != nil {
			return err
		}

		route, err = d.route(before)
		if err != nil {
			return err
		}

		routes, changed := withRoute(before.Routes, route)
		if !changed {
			return nil
		}
		done, err = deploy(ctx, d.a, d.project, d.noQueue, &pethost.DeployProjectRequest{
			BaseDeployID: before.DeployID,
			XPethost:     &pethost.ProjectExtension{Routes: routes},
		}, routeWords)
		return err
	})
	if err != nil {
		return err
	}

	if done == nil {
		d.a.UI.Info("%s already leads to %s.", ui.Clean(route.Host), routeTarget(route))
		return d.sayHost(before, route)
	}
	if d.a.JSON {
		return d.a.PrintJSON(done.answer)
	}
	d.a.UI.Done("%s → %s", ui.Clean(route.Host), routeTarget(route))
	done.Notes(d.a)
	done.Problems(d.a)
	return d.sayHost(done.Project, route)
}

// read asks at once for the project, whose routes the change is made of, and for the machine,
// whose name a DNS record points at.
func (d *domainAdd) read(ctx context.Context) (*pethost.Project, error) {
	client, err := d.a.Client()
	if err != nil {
		return nil, err
	}

	var answer *pethost.GetProjectResponse
	err = app.Together(
		func() (err error) {
			answer, err = client.GetProject(ctx, &pethost.GetProjectRequest{ProjectID: d.project.ID})
			return err
		},
		func() (err error) {
			d.machine, err = client.GetMachine(ctx, nil)
			return err
		},
	)
	if err != nil {
		return nil, err
	}
	return answer.Project, nil
}

// route is the route to add: the host, and where it leads.
func (d *domainAdd) route(project *pethost.Project) (pethost.Route, error) {
	to, err := d.leadsTo(project)
	if err != nil {
		return pethost.Route{}, err
	}
	return pethost.Route{Host: d.host, Service: to.service, Port: to.port}, nil
}

// A listener is a service's port.
type listener struct {
	service string
	port    uint32
}

func (l listener) String() string {
	return fmt.Sprintf("%s:%d", l.service, l.port)
}

// leadsTo settles where the address leads: as typed; else where the host already leads; else
// where every address of the project leads, when that is one place; else to the one port that
// listens; else the person picks, or is given one runnable line for each port.
func (d *domainAdd) leadsTo(project *pethost.Project) (listener, error) {
	candidates, err := d.candidates(project)
	if err != nil {
		return listener{}, err
	}

	switch {
	case len(candidates) == 1:
		return candidates[0], nil
	case len(candidates) == 0:
		return listener{}, fail.Failed("No service of %s listens on a port now: name where the address leads.", ui.Clean(project.ProjectID)).
			WithNext(d.command("<service>:<port>"))
	case !d.a.UI.CanAsk():
		return listener{}, fail.Choice("Where does "+ui.Clean(d.host)+" lead?", d.commands(candidates)...)
	}

	choices := make([]ui.Choice, len(candidates))
	for i, candidate := range candidates {
		choices[i] = ui.Choice{Label: ui.Clean(candidate.String())}
	}
	picked, err := d.a.UI.Pick("Where does "+ui.Clean(d.host)+" lead?", choices)
	if err != nil {
		return listener{}, err
	}
	d.to = candidates[picked].String()
	return candidates[picked], nil
}

// candidates are the places the address may lead to, in leadsTo's order: one when it is settled,
// several when the person is to pick, none when nothing listens.
func (d *domainAdd) candidates(project *pethost.Project) ([]listener, error) {
	if d.to != "" {
		return d.typed(project)
	}

	if own := d.ownRoute(project); own != nil {
		return []listener{{service: own.Service, port: own.Port}}, nil
	}

	now := routed(project.Routes)
	if len(now) == 1 {
		return now, nil
	}

	if ports := listening(project, ""); len(ports) > 0 {
		return ports, nil
	}
	return now, nil
}

// typed is where the address leads as typed: service:port, or a service alone.
func (d *domainAdd) typed(project *pethost.Project) ([]listener, error) {
	service, port, hasPort := strings.Cut(d.to, ":")
	if !hasPort {
		// A service alone: the port is the one it listens on.
		candidates := listening(project, service)
		if len(candidates) == 0 {
			return nil, fail.Failed("%s listens on no port now: name the port.", ui.Clean(service)).
				WithNext(d.command(word(service) + ":<port>"))
		}
		return candidates, nil
	}

	number, err := strconv.ParseUint(port, 10, 32)
	if err != nil {
		return nil, fail.Usage("pethost domain add: "+ui.Clean(d.to)+" is not service:port", d.usage)
	}
	return []listener{{service: service, port: uint32(number)}}, nil
}

// ownRoute is the route the host already has for every path; nil = none.
func (d *domainAdd) ownRoute(project *pethost.Project) *pethost.Route {
	at := slices.IndexFunc(project.Routes, func(route pethost.Route) bool { return sameHost(route, d.host) && everyPath(route) })
	if at < 0 {
		return nil
	}
	return &project.Routes[at]
}

// command is this command with where the address leads, a word as a shell takes it.
func (d *domainAdd) command(to string) string {
	return "pethost domain add " + word(d.host) + " " + to + d.project.Flag()
}

func (d *domainAdd) commands(candidates []listener) []string {
	commands := make([]string, len(candidates))
	for i, candidate := range candidates {
		commands[i] = d.command(word(candidate.String()))
	}
	return commands
}

// listening are the ports the project's services listen on now, as the machine reports them:
// of one service, or of all when none is named.
func listening(project *pethost.Project, service string) []listener {
	var found []listener
	for _, candidate := range project.Services {
		if service != "" && candidate.Service != service {
			continue
		}
		for _, port := range candidate.ListeningPorts {
			found = append(found, listener{service: candidate.Service, port: port})
		}
	}
	return found
}

// routed are the places the project's routes lead to, each once.
func routed(routes []pethost.Route) []listener {
	var found []listener
	for _, route := range routes {
		if to := (listener{service: route.Service, port: route.Port}); !slices.Contains(found, to) {
			found = append(found, to)
		}
	}
	return found
}

func sameHost(route pethost.Route, host string) bool {
	return strings.EqualFold(route.Host, host)
}

// everyPath says whether a route takes every path of its host.
func everyPath(route pethost.Route) bool {
	return route.Path == "" || route.Path == "/"
}

// withRoute is the project's routes as they are plus the new one. A host that already has a
// route for every path gets it changed in place; false = it already leads there.
func withRoute(routes []pethost.Route, added pethost.Route) ([]pethost.Route, bool) {
	routes = slices.Clone(routes)
	at := slices.IndexFunc(routes, func(route pethost.Route) bool { return sameHost(route, added.Host) && everyPath(route) })
	if at < 0 {
		return append(routes, added), true
	}
	if routes[at].Service == added.Service && routes[at].Port == added.Port {
		return routes, false
	}
	routes[at].Service, routes[at].Port = added.Service, added.Port
	return routes, true
}

// sayHost ends the command with how the address stands, by the machine's facts of it: why it
// answers nobody, or the DNS record that is the person's to make while no certificate can come,
// and on stdout the address itself.
func (d *domainAdd) sayHost(project *pethost.Project, route pethost.Route) error {
	if project == nil || d.a.JSON {
		return nil
	}
	at := slices.IndexFunc(project.Hosts, func(host pethost.Host) bool { return strings.EqualFold(host.Host, route.Host) })
	if at < 0 {
		return nil
	}
	host := project.Hosts[at]

	screen := d.a.UI
	served := !host.CertificateExpireTime.IsZero()
	switch {
	case host.UnavailableMessage != "":
		screen.Problem("%s", ui.Clean(host.UnavailableMessage))
	case served:
	case host.CertificateSource == pethost.CertificateSourceAutomatic:
		d.sayRecord(host)
	case host.CertificateSource == pethost.CertificateSourceNone:
		screen.Info("  No certificate will come for it: it answers over plain HTTP only.")
	}
	_, err := fmt.Fprintln(screen.Out, ui.Clean(host.URL))
	return err
}

// sayRecord prints the DNS record a domain of the person's own needs: a CNAME to the machine's
// name, or at a domain's apex, where no CNAME can be, an ALIAS to it.
func (d *domainAdd) sayRecord(host pethost.Host) {
	if d.machine == nil || d.machine.Machine == nil {
		return
	}
	screen := d.a.UI
	name, machine := ui.Clean(host.Host), ui.Clean(d.machine.Machine.Hostname)

	whose, record, note := "the domain", "CNAME", ""
	if strings.Count(name, ".") == 1 {
		whose, record, note = name, "ALIAS", "   "+screen.Err.Dim("(a domain's apex takes no CNAME; never an IP address)")
	}
	screen.Info("One step is yours, where you manage %s's DNS:", whose)
	screen.Info("  %s   %s   %s%s", screen.Err.Bold(name), record, screen.Err.Bold(machine), note)
	if record == "CNAME" {
		// Whether a longer name is a domain's apex only its registry's list says, as with
		// example.co.uk: the case is named, not guessed.
		screen.Info("  %s", screen.Err.Dim("(if the name is the domain itself, which takes no CNAME: ALIAS there, never an IP address)"))
	}
	screen.Info("The certificate comes by itself once the name resolves:  %s", screen.Err.Bold("pethost domain"+d.project.Flag()))
}
