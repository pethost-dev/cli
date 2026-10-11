// Package app is what every command gets: the global flags, the screen, the sign-in and the
// SDK's client, made when a command first calls the API. One App is made for a run, and each
// command's function closes over it. It also has what commands share of a run: its command line
// as a refusal repeats it (CommandLine, ShellWord), --debug's lines (Debugf), and Together, which
// makes independent calls at once.
//
// The context a command works under is cobra's: cmd.Context(), which Ctrl-C cancels.
package app

import (
	"slices"
	"strings"

	pethost "github.com/pethost-dev/sdk/go"
	"github.com/spf13/pflag"

	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/jsonout"
	"github.com/pethost-dev/cli/internal/signin"
	"github.com/pethost-dev/cli/internal/ui"
)

// An App is one run of the CLI.
type App struct {
	// The three streams, and what is printed and asked on them.
	UI *ui.Screen
	// Which build this is.
	Build Build
	// The command line after the program's name, as typed: what a refusal repeats in the
	// commands it names.
	Args []string

	// -p, --project: the project a command acts on. Empty = not given: internal/target finds it.
	Project string
	// --json: the answer is the API's own JSON, and an error is an object on stderr.
	JSON bool
	// --debug: each call's name, time and outcome on stderr.
	Debug bool

	client *pethost.Client
	signIn *signin.SignIn
}

// Build says which release a binary is and how it came.
type Build struct {
	// "0.3.1"; "dev" for a build that is no release.
	Version string
	// It came by `go install`, which is then how it is updated.
	GoInstall bool
}

// New is the App of a process.
func New(screen *ui.Screen, build Build, args []string) *App {
	return &App{UI: screen, Build: build, Args: args}
}

// Flags adds the global flags, which every command takes.
func (a *App) Flags(flags *pflag.FlagSet) {
	flags.StringVarP(&a.Project, "project", "p", "", "the project to act on, by its id")
	flags.BoolVar(&a.JSON, "json", false, "print the API's own answer as JSON")
	flags.BoolVarP(&a.UI.Yes, "yes", "y", false, "answer yes to a question")
	flags.BoolVarP(&a.UI.Quiet, "quiet", "q", false, "print the answer alone")
	flags.BoolVar(&a.Debug, "debug", false, "print each call to the API on stderr")
}

// Start is called once the flags are read, before a command runs: --json prints one answer, so
// it silences progress as -q does.
func (a *App) Start() {
	a.UI.Quiet = a.UI.Quiet || a.JSON
}

// SignIn is the sign-in this run uses, read once: its Token is empty when there is none.
func (a *App) SignIn() (signin.SignIn, error) {
	if a.signIn == nil {
		current, err := signin.Current()
		if err != nil {
			return signin.SignIn{}, err
		}
		a.signIn = &current
	}
	return *a.signIn, nil
}

// SayToken says, before a command changes something, that it acts by PETHOST_TOKEN where this
// computer also has a sign-in of its own: the two may be different accounts.
func (a *App) SayToken() {
	if current, err := a.SignIn(); err == nil && current.OverStored {
		a.UI.Info("Signed in by PETHOST_TOKEN, not by this computer's own sign-in.")
	}
}

// SignedIn is called after a sign-in was stored during the run: the next call uses it.
func (a *App) SignedIn() {
	a.signIn, a.client = nil, nil
}

// Client is the SDK's client, made on first use. Without a sign-in it is fail.NotSignedIn.
func (a *App) Client() (*pethost.Client, error) {
	if a.client != nil {
		return a.client, nil
	}

	current, err := a.SignIn()
	if err != nil {
		return nil, err
	}
	if current.Token == "" {
		return nil, fail.NotSignedIn()
	}
	a.client = pethost.NewClient(pethost.Config{Token: current.Token, BaseURL: current.URL, HTTPClient: a.debugClient()})
	return a.client, nil
}

// PrintJSON writes a command's answer for --json: the SDK's value as the API's JSON, on one
// line of stdout. A stream's answers are one call each.
func (a *App) PrintJSON(value any) error {
	return jsonout.Write(a.UI.Out, value)
}

// WithAnswer writes a failed call's answer for --json and returns err as it is: the answer says
// more than the error's one line.
func (a *App) WithAnswer(answer any, err error) error {
	if a.JSON {
		if printErr := a.PrintJSON(answer); printErr != nil {
			return printErr
		}
	}
	return err
}

// CommandLine is this run's command line with more arguments added, as a refusal names it:
// CommandLine("-p", "recipes") is "pethost logs -f -p recipes". They go before a "--": what
// follows it is another program's command line, as in "pethost run web -- ls -l".
func (a *App) CommandLine(more ...string) string {
	own, rest := a.Args, []string(nil)
	if dash := slices.Index(a.Args, "--"); dash >= 0 {
		own, rest = a.Args[:dash], a.Args[dash:]
	}

	words := []string{"pethost"}
	for _, word := range slices.Concat(own, more, rest) {
		words = append(words, ShellWord(word))
	}
	return strings.Join(words, " ")
}

// ShellWord is a word as a shell takes it: in single quotes unless it is plainly safe.
func ShellWord(word string) string {
	if word != "" && strings.Trim(word, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._:/=%+,@-") == "" {
		return word
	}
	return "'" + strings.ReplaceAll(word, "'", `'\''`) + "'"
}
