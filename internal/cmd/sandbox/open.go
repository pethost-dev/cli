package sandbox

import (
	"context"
	"fmt"

	"github.com/pethost-dev/sdk/go/sandboxes"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/browser"
	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/ui"
)

type openOptions struct {
	terminal, desktop, viewOnly bool
	command                     string
}

// openCommand is `pethost sandbox open <id>`.
func openCommand(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "open <id>",
		Short: "Open a sandbox's page, terminal or desktop in the browser",
		Long: `Opens the sandbox's page in the panel in your browser, and prints its address: there you
see its terminal, desktop and events once signed in.

--terminal and --desktop open a link straight into its terminal or its screen, which works
once and is never printed: where no browser opens, the page's address is said instead.`,
		Example: `  pethost sandbox open k7f3q2xy0a1b2c3d
  pethost sandbox open k7f3q2xy0a1b2c3d --terminal --command claude
  pethost sandbox open k7f3q2xy0a1b2c3d --desktop --view-only`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeSandboxes(a),
	}
	var options openOptions
	cmd.Flags().BoolVar(&options.terminal, "terminal", false, "open its terminal")
	cmd.Flags().StringVar(&options.command, "command", "", "with --terminal: run this in place of a shell, as claude")
	cmd.Flags().BoolVar(&options.desktop, "desktop", false, "open its screen, the desktop started first if it does not run")
	cmd.Flags().BoolVar(&options.viewOnly, "view-only", false, "with --desktop: watch it without clicking or typing")
	cmd.MarkFlagsMutuallyExclusive("terminal", "desktop")

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		c, err := client(a)
		if err != nil {
			return err
		}
		id, err := idOf(cmd.Context(), c, args[0])
		if err != nil {
			return err
		}
		if options.terminal || options.desktop {
			return openLink(cmd.Context(), a, c, id, options)
		}
		return openPage(cmd.Context(), a, c, id)
	}
	return cmd
}

// openPage prints the sandbox's page in the panel, and opens it where a person reads it.
func openPage(ctx context.Context, a *app.App, c *sandboxes.Client, id string) error {
	sandbox, err := c.GetSandbox(ctx, &sandboxes.GetSandboxRequest{SandboxID: id})
	if err != nil {
		return about(id, err)
	}
	if _, err := fmt.Fprintln(a.UI.Out, ui.Clean(sandbox.PanelURL)); err != nil {
		return err
	}
	// A program that takes the address wants no browser.
	if a.UI.Out.Terminal && browser.Open(sandbox.PanelURL) == nil {
		a.UI.Info("Opened in your browser.")
	}
	return nil
}

// openLink opens a one-time link into the sandbox's terminal or screen in the browser. The link
// is a key to the sandbox for whoever holds it: where no browser opens it, it is not printed,
// and the sandbox's page, behind the panel's sign-in, is named instead.
func openLink(ctx context.Context, a *app.App, c *sandboxes.Client, id string, options openOptions) error {
	var target sandboxes.CreateLinkTarget = sandboxes.CreateLinkTerminal{Command: options.command}
	what := "terminal"
	if options.desktop {
		target, what = sandboxes.CreateLinkDesktop{ViewOnly: options.viewOnly}, "screen"
	}

	if a.UI.Out.Terminal {
		link, err := linkFor(ctx, c, id, target)
		if err != nil {
			return err
		}
		if browser.Open(link.URL) == nil {
			a.UI.Done("Opened the %s of %s in your browser.", what, ui.Clean(id))
			return nil
		}
	}

	sandbox, err := c.GetSandbox(ctx, &sandboxes.GetSandboxRequest{SandboxID: id})
	if err != nil {
		return about(id, err)
	}
	e := &fail.Error{Exit: fail.ExitPerson, Code: "NO_BROWSER",
		Message: "No browser opens here, and the link into its " + what + " is for a browser alone."}
	return e.WithLead("Open its page, where you sign in:", ui.Clean(sandbox.PanelURL))
}
