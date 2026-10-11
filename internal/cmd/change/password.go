package change

import (
	"context"
	"errors"

	pethost "github.com/pethost-dev/sdk/go"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/operation"
	"github.com/pethost-dev/cli/internal/target"
	"github.com/pethost-dev/cli/internal/ui"
)

// passwordCommand is `pethost password [off]`: a password page in front of every address of
// the project. The password is typed hidden or piped in, never an argument, and is never
// printed; `off` makes the site public again, so it asks first.
func passwordCommand(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "password [off]",
		Short: "Put a password page in front of the site, or take it away",
		Long: `Put a password page in front of every address of the project: visitors enter the
password once. It is asked for without being shown, or read from stdin where nobody
can be asked, and nothing ever prints it.

  pethost password off     takes the page away: the site is public again`,
		Example: `  pethost password
  pethost password < password.txt
  pethost password off`,
		Args:      cobra.MaximumNArgs(1),
		ValidArgs: []string{"off"},
	}
	noQueue := operation.NoQueueFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) == 1 && args[0] != "off" {
			return fail.Usage(cmd.CommandPath()+": the password is never an argument: it is asked for, or read from stdin", cmd.UseLine())
		}

		project, err := target.ForChange(cmd.Context(), a)
		if err != nil {
			return err
		}
		if len(args) == 1 {
			return removePassword(cmd.Context(), a, project, *noQueue)
		}
		return setPassword(cmd.Context(), a, project, *noQueue)
	}
	return cmd
}

var passwordWords = operation.Words{Doing: "Changing the password…", Failed: "The password was not changed"}

// setPassword reads the password and sets it. One the machine cannot take is asked for again,
// with the machine's reason, where a person types it: the rule for passwords is the machine's.
func setPassword(ctx context.Context, a *app.App, project target.Project, noQueue bool) error {
	for {
		password, err := a.UI.Secret("Password")
		if err != nil {
			return err
		}
		if password == "" && !a.UI.CanAsk() {
			// As `env set KEY` without its value: a program is told what to run instead.
			return fail.Choice("No password came: it is read from stdin here.", "pethost password"+project.Flag()+" < FILE")
		}
		if password == "" {
			return fail.Declined("No password was given: nothing changed.")
		}

		done, err := deploy(ctx, a, project, noQueue, &pethost.DeployProjectRequest{
			XPethost: &pethost.ProjectExtension{Password: password},
		}, passwordWords)
		if api, ok := errors.AsType[*pethost.Error](err); ok && api.Code == pethost.CodeInvalidArgument && a.UI.CanAsk() {
			a.UI.Problem("%s", ui.Clean(api.Message))
			continue
		}
		if err != nil {
			return err
		}

		if done.Project != nil && len(done.Project.Hosts) == 0 {
			a.UI.Done("%s has a password now: every address it gets will ask for it.", ui.Clean(project.ID))
		} else {
			a.UI.Done("Every address of %s asks for the password now.", ui.Clean(project.ID))
		}
		sayWaitingHosts(a, done.Project)
		a.UI.Info("  To take it away:  %s", a.UI.Err.Bold("pethost password off"+project.Flag()))
		return done.said(a)
	}
}

// sayWaitingHosts names the addresses that answer nobody yet: a protected project is never
// served over plain HTTP, so a host without its certificate waits for it.
func sayWaitingHosts(a *app.App, project *pethost.Project) {
	if project == nil {
		return
	}
	for _, host := range project.Hosts {
		if host.CertificateExpireTime.IsZero() && host.UnavailableMessage == "" {
			a.UI.Info("  %s answers only once it has its certificate: a site behind a password is never served over plain HTTP.", ui.Clean(host.Host))
		}
	}
}

// removePassword takes the password away. The machine is asked first whether there is one: a
// site that has none is not asked about.
func removePassword(ctx context.Context, a *app.App, project target.Project, noQueue bool) error {
	client, err := a.Client()
	if err != nil {
		return err
	}
	answer, err := client.GetProject(ctx, &pethost.GetProjectRequest{ProjectID: project.ID})
	if err != nil {
		return err
	}
	id := ui.Clean(project.ID)
	if !answer.Project.PasswordProtected {
		a.UI.Info("%s has no password: nothing changed.", id)
		return nil
	}

	if err := askFirst(a, "Without its password "+id+" is open to everyone. Take it away?", "Nothing changed: the password stays."); err != nil {
		return err
	}

	done, err := deploy(ctx, a, project, noQueue, &pethost.DeployProjectRequest{
		XPethost: &pethost.ProjectExtension{RemovePassword: true},
	}, passwordWords)
	if err != nil {
		return err
	}
	a.UI.Done("%s has no password now: its addresses are open to everyone.", id)
	return done.said(a)
}
