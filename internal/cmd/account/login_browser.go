package account

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/browser"
	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/oauth"
	"github.com/pethost-dev/cli/internal/signin"
	"github.com/pethost-dev/cli/internal/ui"
)

// The sign-in in the browser: the panel's page is opened, the person presses Connect there, and
// the browser brings the answer back to a port this program listens on.

// signInInBrowser has the person sign in in their browser and stores the sign-in. With
// pressEnter the person is first told that they are not signed in and opens the browser with
// Enter, as inside `pethost deploy`; without, the browser opens at once, as the person who
// typed `pethost login` asked.
func signInInBrowser(ctx context.Context, a *app.App, pressEnter bool) (*signin.Stored, error) {
	current, err := a.SignIn()
	if err != nil {
		return nil, err
	}

	// Before anything is said: a panel that cannot be reached is not worth an Enter.
	server, err := oauth.Discover(ctx, current.URL)
	if err != nil {
		return nil, signInFailure(ctx, err)
	}
	a.Debugf("sign-in  the page %s  the token %s", server.AuthorizationEndpoint, server.TokenEndpoint)
	clientID, err := server.Register(ctx, clientName())
	if err != nil {
		return nil, signInFailure(ctx, err)
	}
	a.Debugf("sign-in  registered as %q", clientName())
	attempt, err := server.Begin(clientID)
	if err != nil {
		return nil, signInFailure(ctx, err)
	}
	defer attempt.Close()

	if err := showTheWay(a, hostOf(current.URL), attempt.URL, pressEnter); err != nil {
		return nil, err
	}
	live := a.UI.Live("Waiting for you to press Connect there…")
	waiting, cancel := context.WithTimeout(ctx, signInWait)
	token, err := attempt.Wait(waiting)
	cancel()
	live.Stop()
	if err != nil {
		return nil, signInFailure(ctx, err)
	}

	return store(a, signin.Stored{URL: current.URL, Token: token, ClientID: clientID, SignInTime: time.Now().UTC()})
}

// signInWait is how long the browser's answer is waited for. Nobody needs longer to press a
// button, and a program that ran `pethost login` with nobody there must end by itself.
const signInWait = 10 * time.Minute

// clientName is what the panel's Settings call this sign-in, so that a person with two
// computers knows which one they revoke.
func clientName() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		return "Pethost CLI"
	}
	return "Pethost CLI on " + host
}

func hostOf(panelURL string) string {
	if parsed, err := url.Parse(panelURL); err == nil && parsed.Host != "" {
		return parsed.Host
	}
	return panelURL
}

// showTheWay opens the panel's page in the browser and always writes its address too: the
// browser may not open, as on a computer without a desktop. These lines are the way to sign in,
// not progress, so -q does not silence them.
func showTheWay(a *app.App, panel, address string, pressEnter bool) error {
	screen := a.UI
	if pressEnter {
		fmt.Fprintf(screen.Err, "Not signed in. Press Enter to open %s in your browser… ", panel)
		if _, err := screen.Enter(nil); err != nil {
			fmt.Fprintln(screen.Err) // The question's line ends here: no Enter ended it.
			if errors.Is(err, ui.ErrInterrupted) {
				return err
			}
			return fail.NotSignedIn()
		}
	}

	switch err := browser.Open(address); {
	case err != nil && !screen.Err.Terminal:
		// No browser shows the page and nobody reads the address: nobody would press Connect.
		return (&fail.Error{Exit: fail.ExitPerson, Code: "NOT_SIGNED_IN",
			Message: "No browser opens from here, and nobody is at a terminal to open one: not signed in. A program reads an API token from PETHOST_TOKEN."}).
			WithLead("To keep one on this computer, run:", "pethost login --token < FILE")
	case err != nil:
		screen.ErrLine("No browser opens from here. In a browser on this computer, go to:")
		screen.ErrLine("  " + address)
		screen.ErrLine(screen.Err.Dim("On a computer with no browser, sign in with an API token:   pethost login --token"))
	case pressEnter:
		screen.ErrLine("If it does not open, go to:")
		screen.ErrLine("  " + address)
	default:
		screen.ErrLine(fmt.Sprintf("Opening %s in your browser. If it does not open, go to:", panel))
		screen.ErrLine("  " + address)
	}
	return nil
}

// signInFailure is how a sign-in that did not finish ends the command. The person's own no is
// said plainly; a panel that was not reached is worth another try later; the rest is the
// panel's or the browser's word, which is other people's text.
func signInFailure(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, oauth.ErrDenied) {
		return fail.Declined("You cancelled the sign-in in the browser: nothing was changed.")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return (&fail.Error{Exit: fail.ExitPerson, Code: "NOT_SIGNED_IN",
			Message: fmt.Sprintf("Nobody pressed Connect in the browser within %s: not signed in.", ui.Rough(signInWait))}).
			WithLead("To try again, run:", "pethost login")
	}
	if unreached, ok := errors.AsType[*oauth.NotReached](err); ok {
		return &fail.Error{Exit: fail.ExitLater, Code: "UNAVAILABLE",
			Message: "Pethost could not be reached: " + unreached.Cause.Error() + ". Try again in a minute."}
	}
	return fail.Failed("The sign-in did not finish: %s.", ui.Clean(err.Error())).WithLead("To try again, run:", "pethost login")
}
