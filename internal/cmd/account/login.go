package account

import (
	"context"
	"errors"
	"strings"
	"time"

	pethost "github.com/pethost-dev/sdk/go"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/say"
	"github.com/pethost-dev/cli/internal/signin"
)

// loginCommand is `pethost login`: the person signs in in their browser, or, with --token,
// gives an API token on stdin. Either way the sign-in is stored for the next runs.
func loginCommand(a *app.App) *cobra.Command {
	var token bool
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Sign in, in the browser",
		Long: `Sign in: your browser opens the panel, where you press Connect. The sign-in is kept
on this computer, in a file only you can read, until pethost logout.

Where no browser can open, as in a remote shell, sign in with an API token: make one
in the panel's Settings, under API tokens, and give it to pethost login --token on
stdin. A program needs neither: it reads PETHOST_TOKEN.`,
		Example: `  pethost login
  pethost login --token < token.txt`,
		Args: noTokenArgument,
		RunE: func(cmd *cobra.Command, args []string) error {
			if token {
				return loginWithToken(cmd.Context(), a)
			}
			return loginInBrowser(cmd.Context(), a)
		},
	}
	cmd.Flags().BoolVar(&token, "token", false, "read an API token from stdin, for a shell with no browser")
	return cmd
}

// noTokenArgument refuses a word after `pethost login`, which is most likely a token: one typed
// there stays in the shell's history and shows in the list of processes.
func noTokenArgument(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	return fail.Usage("pethost login takes no argument. A token is read from stdin, so that it is in no command line.",
		"pethost login --token < token.txt")
}

func loginInBrowser(ctx context.Context, a *app.App) error {
	stored, err := signInInBrowser(ctx, a, false)
	if err != nil {
		return err
	}
	answer, err := machineOf(ctx, a, stored.URL, stored.Token)
	return sayWhoSignedIn(ctx, a, answer, err)
}

// SignIn signs the person in from inside another command, as `pethost deploy` does on a
// terminal: it says they are not signed in, has them sign in in the browser, stores the sign-in
// and calls a.SignedIn, so that the command goes on. Its caller has seen that a.UI.CanAsk, and
// says who signed in itself.
func SignIn(ctx context.Context, a *app.App) error {
	_, err := signInInBrowser(ctx, a, true)
	return err
}

// loginWithToken stores an API token read from stdin, hidden when a person types it, once the
// panel took it: a token the panel refuses must not replace a sign-in that works.
func loginWithToken(ctx context.Context, a *app.App) error {
	current, err := a.SignIn()
	if err != nil {
		return err
	}
	typed, err := a.UI.Secret("API token")
	if err != nil {
		return err
	}
	token := strings.TrimSpace(typed)
	if token == "" {
		return fail.Failed("No token came on stdin.").
			WithLead("Make one in the panel's Settings, under API tokens:", current.URL+"/settings")
	}

	answer, err := machineOf(ctx, a, current.URL, token)
	if api, ok := errors.AsType[*pethost.Error](err); ok && !reachedTheAccount(api) {
		if api.Code == pethost.CodeUnauthenticated {
			return (&fail.Error{Exit: fail.ExitPerson, Code: api.Code.String(), Message: "The panel does not take this token."}).
				WithLead("Make one in its Settings, under API tokens:", current.URL+"/settings")
		}
		return err
	}
	if _, err := store(a, signin.Stored{URL: current.URL, Token: token, SignInTime: time.Now().UTC()}); err != nil {
		return err
	}
	return sayWhoSignedIn(ctx, a, answer, err)
}

// reachedTheAccount says whether a refusal came from past the panel's check of the token: a
// typed detail is about the account's machine, which only a token the panel took is told.
func reachedTheAccount(api *pethost.Error) bool {
	return api.NoMachine != nil || api.MachineUnreachable != nil
}

// machineOf asks for the machine with a token of its own, not the run's: the run may go on
// under PETHOST_TOKEN while the person signs this computer in.
func machineOf(ctx context.Context, a *app.App, panelURL, token string) (*pethost.GetMachineResponse, error) {
	started := time.Now()
	answer, err := pethost.NewClient(pethost.Config{Token: token, BaseURL: panelURL}).GetMachine(ctx, nil)

	outcome := "answered"
	if api, ok := errors.AsType[*pethost.Error](err); ok {
		outcome = api.Code.String()
	}
	a.Debugf("GetMachine  %s  %s", time.Since(started).Round(time.Millisecond), outcome)
	return answer, err
}

// store keeps a sign-in for the next runs, and for the rest of this one.
func store(a *app.App, stored signin.Stored) (*signin.Stored, error) {
	if err := signin.Store(stored); err != nil {
		return nil, fail.Failed("The sign-in could not be kept on this computer: %s", err)
	}
	a.SignedIn()
	return &stored, nil
}

// sayWhoSignedIn ends `pethost login` with what the panel answered about the account's machine:
// the machine in a few words, or that there is none yet, with the API's link to where a plan is
// chosen. The sign-in is stored by now, so a machine that does not answer is not this command's
// failure.
func sayWhoSignedIn(ctx context.Context, a *app.App, answer *pethost.GetMachineResponse, err error) error {
	api, _ := errors.AsType[*pethost.Error](err)
	switch {
	case err == nil && answer.Machine != nil:
		a.UI.Done("Signed in. Your machine: %s", say.Machine(answer.Machine))
	case api != nil && api.NoMachine != nil:
		a.UI.Done("Signed in.")
		a.UI.Info("%s", fail.NoMachineMessage(api.NoMachine))
	case ctx.Err() != nil:
		return ctx.Err()
	default:
		a.UI.Done("Signed in.")
	}

	if current, err := a.SignIn(); err == nil && current.FromEnvironment {
		a.UI.Info("PETHOST_TOKEN is set and wins over this sign-in: unset it to use the sign-in.")
	}
	if a.JSON && answer != nil {
		return a.PrintJSON(answer)
	}
	if answer != nil {
		a.UI.Next("pethost deploy", "pethost")
	}
	return nil
}
