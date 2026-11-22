package sandbox

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/pethost-dev/sdk/go/sandboxes"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/ui"
)

// portsCommand is `pethost sandbox ports <id>`.
func portsCommand(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ports <id>",
		Short: "A sandbox's public ports and their addresses; open or close one",
		Long: `Lists the sandbox's public ports, each with its address, which anyone may open in a
browser: https://<port>-<id>.<server>. --open makes a port public, --close private again;
a private port answers only a request with its token (pethost sandbox url --token).`,
		Example: `  pethost sandbox ports k7f3q2xy0a1b2c3d
  pethost sandbox ports k7f3q2xy0a1b2c3d --open 3000
  pethost sandbox ports k7f3q2xy0a1b2c3d --close 3000`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeSandboxes(a),
	}
	var opened, closed []uint
	cmd.Flags().UintSliceVar(&opened, "open", nil, "make this port public (again for more)")
	cmd.Flags().UintSliceVar(&closed, "close", nil, "make this port private again (again for more)")

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		c, err := client(a)
		if err != nil {
			return err
		}
		id := idOf(cmd.Context(), c, args[0])
		sandbox, err := c.GetSandbox(cmd.Context(), &sandboxes.GetSandboxRequest{SandboxID: id})
		if err != nil {
			return about(id, err)
		}
		if len(opened) > 0 || len(closed) > 0 {
			a.SayToken()
			if sandbox, err = changePorts(cmd.Context(), c, sandbox, opened, closed); err != nil {
				return about(id, err)
			}
		}
		return printPorts(a, sandbox)
	}
	return cmd
}

// changePorts sends the sandbox's ingress with the ports opened and closed, if it has not changed
// since it was read.
func changePorts(ctx context.Context, c *sandboxes.Client, sandbox *sandboxes.Sandbox, opened, closed []uint) (*sandboxes.Sandbox, error) {
	var ports []sandboxes.PublicPort
	for _, port := range publicPorts(sandbox) {
		if !slices.Contains(closed, uint(port.Port)) {
			ports = append(ports, sandboxes.PublicPort{Port: port.Port})
		}
	}
	for _, port := range opened {
		if !slices.ContainsFunc(ports, func(listed sandboxes.PublicPort) bool { return uint(listed.Port) == port }) {
			ports = append(ports, sandboxes.PublicPort{Port: uint32(port)})
		}
	}
	return c.UpdateSandbox(ctx, &sandboxes.UpdateSandboxRequest{
		SandboxID: sandbox.SandboxID, IfRevision: sandbox.Revision,
		Settings: &sandboxes.SandboxSettings{Ingress: &sandboxes.Ingress{PublicPorts: ports}},
	})
}

func printPorts(a *app.App, sandbox *sandboxes.Sandbox) error {
	if a.JSON {
		return a.PrintJSON(sandbox)
	}
	ports := publicPorts(sandbox)
	if len(ports) == 0 {
		a.UI.Info("No public port. Open one:  %s", a.UI.Err.Bold("pethost sandbox ports "+ui.Clean(sandbox.SandboxID)+" --open 3000"))
		return nil
	}
	table := a.UI.Out.Table()
	for _, port := range ports {
		table.Row(fmt.Sprint(port.Port), ui.Clean(port.URL))
	}
	table.Flush()
	return nil
}

// urlCommand is `pethost sandbox url <id> <port>`.
func urlCommand(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "url <id> <port>",
		Short: "The address of a port of a sandbox, and the token of a private one",
		Long: `Prints the address of a port of the sandbox. A public port's address answers anyone. A
private port answers only a request that sends its token as the header Pethost-Token: a
program's, curl's, a page's fetch; --token prints that header, valid for --ttl.`,
		Example: `  pethost sandbox url k7f3q2xy0a1b2c3d 3000
  curl -H "$(pethost sandbox url k7f3q2xy0a1b2c3d 8080 --token | tail -1)" "$(pethost sandbox url k7f3q2xy0a1b2c3d 8080)"`,
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: completeSandboxes(a),
	}
	var withToken bool
	var ttl time.Duration
	cmd.Flags().BoolVar(&withToken, "token", false, "a private port: also print the header Pethost-Token: <token>")
	cmd.Flags().DurationVar(&ttl, "ttl", time.Hour, "how long the token works, at most 24h")

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		var port uint32
		if _, err := fmt.Sscan(args[1], &port); err != nil {
			return fail.Usage(fmt.Sprintf("pethost sandbox url takes a port, a number, and %q was given.", args[1]), cmd.UseLine())
		}
		ttlSeconds, err := seconds("ttl", ttl)
		if err != nil {
			return err
		}
		return portURL(cmd.Context(), a, args[0], port, ttlSeconds, withToken)
	}
	return cmd
}

func portURL(ctx context.Context, a *app.App, id string, port, ttl uint32, withToken bool) error {
	c, err := client(a)
	if err != nil {
		return err
	}
	id = idOf(ctx, c, id)
	sandbox, err := c.GetSandbox(ctx, &sandboxes.GetSandboxRequest{SandboxID: id})
	if err != nil {
		return about(id, err)
	}
	if public := slices.IndexFunc(publicPorts(sandbox), func(p sandboxes.PublicPort) bool { return p.Port == port }); public >= 0 {
		if withToken {
			a.UI.Info("Port %d is public: it needs no token.", port)
		}
		_, err := fmt.Fprintln(a.UI.Out, ui.Clean(publicPorts(sandbox)[public].URL))
		return err
	}

	link, err := c.CreateLink(ctx, &sandboxes.CreateLinkRequest{SandboxID: id, Target: sandboxes.CreateLinkPort(port), TTLSeconds: ttl})
	if err != nil {
		return about(id, err)
	}
	if _, err := fmt.Fprintln(a.UI.Out, ui.Clean(link.URL)); err != nil {
		return err
	}
	if !withToken {
		a.UI.Info("Port %d is private: a request sends its token, which --token prints. A browser opens a public port: pethost sandbox ports %s --open %d", port, ui.Clean(id), port)
		return nil
	}
	_, err = fmt.Fprintln(a.UI.Out, "Pethost-Token: "+link.Token)
	return err
}
