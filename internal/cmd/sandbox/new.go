package sandbox

import (
	"fmt"
	"strings"
	"time"

	"github.com/pethost-dev/sdk/go/sandboxes"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/ui"
)

// newCommand is `pethost sandbox new`.
func newCommand(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "new",
		Aliases: []string{"create"},
		Short:   "Make a sandbox and start it",
		Long: `Makes a sandbox from a template, "base" unless --template names another, and starts it.
Its id is the answer, on stdout; the rest of what is said goes to stderr.

base is Ubuntu with Python, Node.js, the coding agents, Docker and Chrome. A setting given
replaces that part of the template's settings: --allow and --deny its egress, each --env
its whole environment, and a timer its lifecycle, whose other timers take their defaults.
--deny without --allow keeps every other host open.`,
		Example: `  pethost sandbox new
  pethost sandbox new --name api-dev
  pethost sandbox new --size large --label user=42
  pethost sandbox new --template my-app --public-port 3000
  pethost sandbox new --allow pypi.org --allow files.pythonhosted.org
  pethost sandbox new --env ANTHROPIC_API_KEY=secret:anthropic
  pethost sandbox new --pause-after 1m --stop-after 10m --delete-after 24h
  pethost sandbox new --mount vol_8f2k3j:/data`,
		Args: cobra.NoArgs,
	}
	settings := addSettingsFlags(cmd.Flags())
	var template, name string
	var labels, mounts []string
	var ports []uint
	cmd.Flags().StringVar(&template, "template", "", `the template, "name" or "name@version" (default "base")`)
	cmd.Flags().StringVar(&name, "name", "", "a name to call it by, unique in the workspace: a-z, 0-9 and -")
	cmd.Flags().StringArrayVar(&labels, "label", nil, "a label to find it by, key=value (again for more)")
	cmd.Flags().UintSliceVar(&ports, "public-port", nil, "a port anyone may open at its own address (again for more)")
	cmd.Flags().StringArrayVar(&mounts, "mount", nil, "a volume to mount, VOLUME_ID:/path, or VOLUME_ID:/path:ro read-only (again for more)")

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		request := &sandboxes.CreateSandboxRequest{Template: template, Name: name, Labels: labelsOf(labels), Settings: &sandboxes.SandboxSettings{}}
		if err := settings.apply(request.Settings, nil); err != nil {
			return err
		}
		if cmd.Flags().Changed("public-port") {
			request.Settings.Ingress = ingressOf(ports)
		}
		for _, typed := range mounts {
			mount, err := mountOf(typed)
			if err != nil {
				return err
			}
			request.Mounts = append(request.Mounts, mount)
		}
		return create(cmd, a, request)
	}
	return cmd
}

func create(cmd *cobra.Command, a *app.App, request *sandboxes.CreateSandboxRequest) error {
	c, err := clientForChange(a)
	if err != nil {
		return err
	}
	started := time.Now()
	live := a.UI.Live("Making a sandbox…")
	sandbox, err := c.CreateSandbox(cmd.Context(), request)
	live.Stop()
	if err != nil {
		return err
	}

	if a.JSON {
		return a.PrintJSON(sandbox)
	}
	id := ui.Clean(sandbox.SandboxID)
	a.UI.Done("Made sandbox %s: %s, %s, from %s, in %s", nameWords(sandbox), word(sizeOf(sandbox)), word(sandbox.State),
		ui.Clean(sandbox.Template), ui.Duration(time.Since(started)))
	for _, port := range publicPorts(sandbox) {
		a.UI.Info("  port %d: %s", port.Port, ui.Clean(port.URL))
	}
	if _, err := fmt.Fprintln(a.UI.Out, id); err != nil {
		return err
	}
	a.UI.Next("pethost sandbox run "+id+" -- <command>", "pethost sandbox shell "+id)
	return nil
}

// ingressOf is the ingress group of the ports given.
func ingressOf(ports []uint) *sandboxes.Ingress {
	ingress := &sandboxes.Ingress{}
	for _, port := range ports {
		ingress.PublicPorts = append(ingress.PublicPorts, sandboxes.PublicPort{Port: uint32(port)})
	}
	return ingress
}

// publicPorts are a sandbox's public ports, with their addresses.
func publicPorts(sandbox *sandboxes.Sandbox) []sandboxes.PublicPort {
	if sandbox.Settings == nil || sandbox.Settings.Ingress == nil {
		return nil
	}
	return sandbox.Settings.Ingress.PublicPorts
}

// mountOf is a --mount: "vol_8f2k3j:/data", or "vol_8f2k3j:/data:ro".
func mountOf(typed string) (sandboxes.Mount, error) {
	volume, rest, ok := strings.Cut(typed, ":")
	path, mode, _ := strings.Cut(rest, ":")
	if !ok || volume == "" || path == "" || (mode != "" && mode != "ro" && mode != "rw") {
		return sandboxes.Mount{}, wrongValue("--mount takes VOLUME_ID:/path or VOLUME_ID:/path:ro, and %q was given.", typed)
	}
	return sandboxes.Mount{VolumeID: volume, Path: path, ReadOnly: mode == "ro"}, nil
}
