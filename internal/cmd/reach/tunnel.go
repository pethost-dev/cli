package reach

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/remote"
	"github.com/pethost-dev/cli/internal/say"
	"github.com/pethost-dev/cli/internal/target"
	"github.com/pethost-dev/cli/internal/ui"
)

// tunnelCommand is `pethost tunnel [service[:port]]`.
func tunnelCommand(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tunnel [service[:port]]",
		Short: "Bring a service's port to localhost, as a database to your laptop",
		Long: `Opens a port on this computer that leads to a service's port in its container, over SSH.

Only this computer reaches it: it listens on localhost alone. The local port is the service's
own, or the next free one, which the first line says; --local picks it. Without a port the
service's only one is taken. Ctrl-C closes the tunnel.`,
		Example: `  pethost tunnel db
  pethost tunnel db:5432
  pethost tunnel db --local 15432`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: target.CompleteServices(a),
	}
	local := cmd.Flags().Uint16("local", 0, "the port on this computer, not the service's own")

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		named, port, err := splitPort(cmd, firstOf(args))
		if err != nil {
			return err
		}
		project, err := target.ForRead(ctx, a)
		if err != nil {
			return err
		}
		f, err := readFacts(ctx, a, project)
		if err != nil {
			return err
		}

		t := &tunnel{a: a, project: project}
		t.service, err = target.Service(a, named, target.ServiceNames(f.project), func(service string) string {
			return t.commandFor(service, port)
		})
		if err != nil {
			return err
		}
		t.port, err = t.portOf(f, port)
		if err != nil {
			return err
		}

		conn, err := dialService(ctx, a, f, t.service)
		if err != nil {
			return err
		}
		defer conn.Close()
		return t.open(ctx, conn, f, int(*local))
	}
	return cmd
}

// splitPort reads "service[:port]". 0 = no port named.
func splitPort(cmd *cobra.Command, argument string) (service string, port uint32, err error) {
	service, number, hasPort := strings.Cut(argument, ":")
	if !hasPort {
		return service, 0, nil
	}
	parsed, err := strconv.ParseUint(number, 10, 16)
	if err != nil || parsed == 0 {
		return "", 0, fail.Usage(fmt.Sprintf("pethost tunnel: %q is not a port", number), cmd.UseLine())
	}
	return service, uint32(parsed), nil
}

// tunnel is one run of the command.
type tunnel struct {
	a       *app.App
	project target.Project
	service string
	// The port in the container.
	port uint32
}

// commandFor is the command that tunnels to a service, and to a port of it when one is given.
func (t *tunnel) commandFor(service string, port uint32) string {
	if port != 0 {
		service += ":" + strconv.FormatUint(uint64(port), 10)
	}
	return "pethost tunnel " + app.ShellWord(service) + t.project.Flag()
}

// portOf is the container's port the tunnel leads to: the one named; else the only one the
// service listens on now, or, while it listens on none, the only one the machine knows it by;
// else the person's pick, or a refusal with the command for each. A service the project does
// not have ends it here: the machine's gateway would take the login and refuse each connection
// later, behind a line that said the tunnel was open.
func (t *tunnel) portOf(f facts, named uint32) (uint32, error) {
	service := serviceOf(f.project, t.service)
	if service == nil {
		return 0, fail.Failed("%s has no service %s. It has: %s.", ui.Clean(f.project.ProjectID), ui.Clean(t.service),
			ui.Clean(strings.Join(target.ServiceNames(f.project), ", ")))
	}
	if named != 0 {
		return named, nil
	}
	ports := service.ListeningPorts
	if len(ports) == 0 {
		ports = service.Ports
	}

	switch {
	case len(ports) == 1:
		return ports[0], nil
	case len(ports) == 0:
		return 0, fail.Failed("The machine knows of no port that %s listens on.", ui.Clean(t.service)).
			WithLead("Name the port:", "pethost tunnel "+app.ShellWord(t.service)+":PORT"+t.project.Flag())
	case !t.a.UI.CanAsk():
		commands := make([]string, len(ports))
		for i, port := range ports {
			commands[i] = t.commandFor(t.service, port)
		}
		return 0, fail.Choice("Which port of "+ui.Clean(t.service)+"?", commands...)
	}

	choices := make([]ui.Choice, len(ports))
	for i, port := range ports {
		choices[i] = ui.Choice{Label: strconv.FormatUint(uint64(port), 10)}
	}
	picked, err := t.a.UI.Pick("Which port of "+ui.Clean(t.service)+"?", choices)
	if err != nil {
		return 0, err
	}
	return ports[picked], nil
}

// open listens on localhost and carries every connection to the container's port until Ctrl-C,
// which is how a tunnel is closed and so no failure, or until the connection to the machine is
// lost.
func (t *tunnel) open(ctx context.Context, conn *remote.Conn, f facts, local int) error {
	listeners, opened, err := t.listen(local)
	if err != nil {
		return err
	}
	defer closeAll(listeners)

	if err := t.say(f, opened, local); err != nil {
		return err
	}
	for _, listener := range listeners {
		go conn.Forward(listener, t.port, t.refused)
	}

	select {
	case <-ctx.Done():
		return nil
	case <-conn.Lost():
		return lostConnection(t.a, fail.ExitLater, "The connection to your machine was lost: the tunnel is closed.")
	}
}

// portsTried is how many ports after the service's own are tried when that one is not free.
const portsTried = 20

// listen opens the local port: the one --local names, and nothing else when it cannot be had;
// without it the container's own number, the next free one after it, or any the system gives.
func (t *tunnel) listen(local int) (listeners []net.Listener, opened int, err error) {
	if local != 0 {
		listeners, opened, err = remote.Listen(local)
		if err != nil {
			return nil, 0, fail.Failed("localhost:%d cannot be opened here: %s.", local, fail.Innermost(err)).
				WithLead("For the next free port, run:", t.commandFor(t.service, t.port))
		}
		return listeners, opened, nil
	}

	for port := int(t.port); port < int(t.port)+portsTried && port <= 65535; port++ {
		listeners, opened, err = remote.Listen(port)
		if err == nil {
			return listeners, opened, nil
		}
	}
	listeners, opened, err = remote.Listen(0)
	if err != nil {
		return nil, 0, fail.Failed("No port can be opened on localhost: %s.", fail.Innermost(err))
	}
	return listeners, opened, nil
}

// say writes the tunnel's line, and to stdout, where a program reads it, the address alone.
func (t *tunnel) say(f facts, opened, asked int) error {
	a := t.a
	aside := ""
	if asked == 0 && opened != int(t.port) {
		aside = fmt.Sprintf(" (%d is not free here)", t.port)
	}
	a.UI.Done("localhost:%d → %s:%d on your machine%s. Ctrl-C closes it.", opened, ui.Clean(t.service), t.port, aside)
	if service := serviceOf(f.project, t.service); service != nil && !service.ContainerRunning {
		a.UI.Info("  %s is %s now: the machine refuses connections to it until it runs.", ui.Clean(t.service), say.State(service.State))
	}

	switch {
	case a.JSON:
		return a.PrintJSON(map[string]any{"local_port": opened, "service": t.service, "port": t.port})
	case !a.UI.Out.Terminal || a.UI.Quiet:
		fmt.Fprintf(a.UI.Out, "localhost:%d\n", opened)
	}
	return nil
}

// refused says that a connection through the tunnel was not taken, in the machine's words.
func (t *tunnel) refused(reason string) {
	t.a.UI.ErrLine(fmt.Sprintf("%s%s:%d took no connection: %s", t.a.UI.Err.Cross(), ui.Clean(t.service), t.port, ui.Clean(reason)))
}

func closeAll(listeners []net.Listener) {
	for _, listener := range listeners {
		listener.Close()
	}
}
