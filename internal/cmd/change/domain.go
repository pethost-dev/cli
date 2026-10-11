package change

import (
	"context"
	"fmt"

	pethost "github.com/pethost-dev/sdk/go"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/target"
	"github.com/pethost-dev/cli/internal/ui"
)

// domainCommand is `pethost domain`: the project's addresses, where each leads and how its
// certificate stands, all as the machine states them.
func domainCommand(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "domain",
		Short: "The project's addresses and their certificates",
		Long: `The project's addresses: where each leads, and how its certificate stands.

A name under your Pethost domain works at once over HTTPS. A domain of your own
needs one DNS record, which "pethost domain add" prints; its certificate then
comes by itself.`,
		Example: `  pethost domain
  pethost domain add recipes.example.com
  pethost domain add api.example.com api:8000
  pethost domain rm recipes.example.com`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return listDomains(cmd.Context(), a)
		},
	}
	cmd.AddCommand(domainAddCommand(a), domainRmCommand(a))
	return cmd
}

func listDomains(ctx context.Context, a *app.App) error {
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

	if len(answer.Project.Hosts) == 0 {
		a.UI.Info("%s has no address. Give it one:  %s", ui.Clean(project.ID),
			a.UI.Err.Bold("pethost domain add <host>"+project.Flag()))
		return nil
	}
	printHosts(a.UI.Out, answer.Project)
	if answer.Project.PasswordProtected {
		a.UI.Info("Every address asks for the project's password.")
	}
	return nil
}

// printHosts writes a row for each route under its host's address, the host's certificate
// beside the first, and under a host that answers nobody the machine's reason.
func printHosts(out *ui.Stream, project *pethost.Project) {
	table := out.Table()
	for _, host := range project.Hosts {
		certificate := certificateWords(out, host)
		if host.UnavailableMessage != "" {
			certificate = ""
		}
		for _, route := range project.Routes {
			if route.Host != host.Host {
				continue
			}
			table.Row(ui.Clean(host.URL)+routePath(route), "→ "+routeTarget(route), certificate)
			certificate = ""
		}
		if host.UnavailableMessage != "" {
			table.Line("  " + out.Cross() + ui.Clean(host.UnavailableMessage))
		}
	}
	table.Flush()
}

// routePath is a route's path after its host's address; nothing for the route of every path.
func routePath(route pethost.Route) string {
	if everyPath(route) {
		return ""
	}
	return ui.Clean(route.Path)
}

// routeTarget is where a route leads: "app:3000".
func routeTarget(route pethost.Route) string {
	target := fmt.Sprintf("%s:%d", ui.Clean(route.Service), route.Port)
	if route.StripPath {
		target += ", the path's prefix removed"
	}
	return target
}

// certificateWords say how a host's certificate stands, from where the machine says it comes
// and until when the one it serves lasts.
func certificateWords(out *ui.Stream, host pethost.Host) string {
	served := !host.CertificateExpireTime.IsZero()
	switch {
	case served && host.CertificateSource == pethost.CertificateSourceAutomatic:
		return "certificate until " + out.When(host.CertificateExpireTime) + ", renewed by itself"
	case served:
		return "certificate until " + out.When(host.CertificateExpireTime)
	case host.CertificateSource == pethost.CertificateSourceAutomatic:
		return out.Yellow("no certificate yet: it comes once the name resolves to your machine")
	case host.CertificateSource == pethost.CertificateSourceNone:
		return out.Yellow("no certificate will come: plain HTTP only")
	}
	return out.Yellow("no certificate is served yet")
}
