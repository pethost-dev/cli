// Package root is the command tree: the bare command, every area's commands, the global flags,
// the wrong guesses that land, the answer to a command line that is wrong, and the run of the
// process from its arguments to its exit code.
package root

import (
	"context"
	"io"
	"os"
	"os/signal"
	"slices"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/cmd/account"
	"github.com/pethost-dev/cli/internal/cmd/change"
	"github.com/pethost-dev/cli/internal/cmd/kernel"
	"github.com/pethost-dev/cli/internal/cmd/observe"
	"github.com/pethost-dev/cli/internal/cmd/reach"
	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/target"
	"github.com/pethost-dev/cli/internal/ui"
)

// Run runs the CLI on the process's arguments and returns its exit code. stamped is the
// release's version, empty in a build that is no release.
func Run(stamped string) int {
	ctx, stop := signal.NotifyContext(context.Background(), endingSignals()...)
	defer stop()
	// The first Ctrl-C ends the command in its own way; from then on a second one ends the process.
	go func() {
		<-ctx.Done()
		stop()
	}()

	screen := ui.New(os.Stdin, os.Stdout, os.Stderr)
	screen.Interrupt = ctx.Done()
	a := app.New(screen, buildOf(stamped), os.Args[1:])

	ran, err := tree(a).ExecuteContextC(ctx)
	code := fail.Print(screen, target.Explain(ctx, a, err), a.JSON || asksForJSON(a.Args))
	// A command that Ctrl-C ended well, as a tunnel, is followed by no question.
	if err == nil && ctx.Err() == nil && !answersAShell(ran) {
		account.CheckForUpdate(ctx, a)
	}
	return code
}

// asksForJSON says whether the command line has --json, read by pflag from the words alone, as
// the command's own flags read it: a command line that is wrong, as one with a flag that does
// not exist, fails before its flags are read, and a program that asked for JSON is owed its
// error as JSON all the same. What follows "--" is a command for a container, not this
// program's: pflag reads no flag there.
func asksForJSON(args []string) bool {
	flags := pflag.NewFlagSet("", pflag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.ParseErrorsAllowlist.UnknownFlags = true
	asked := flags.Bool("json", false, "")
	// Help is a flag of every command, and pflag would end the reading at it.
	flags.BoolP("help", "h", false, "")
	_ = flags.Parse(args)
	return *asked
}

// endingSignals are the signals that end a command in its own way, so that it leaves the
// terminal as it found it: Ctrl-C, a kill, and the terminal's own end, unless nohup said to
// outlive that one.
func endingSignals() []os.Signal {
	signals := []os.Signal{os.Interrupt, syscall.SIGTERM}
	if !signal.Ignored(syscall.SIGHUP) {
		signals = append(signals, syscall.SIGHUP)
	}
	return signals
}

// answersAShell says whether the command that ran was one a shell calls for completions, whose
// output nothing may be added to.
func answersAShell(ran *cobra.Command) bool {
	for command := ran; command != nil; command = command.Parent() {
		if command.Name() == "completion" || command.Name() == cobra.ShellCompRequestCmd || command.Name() == cobra.ShellCompNoDescRequestCmd {
			return true
		}
	}
	return false
}

// Tree is the command tree outside a run: what the reference (reference_gen.go) reads every
// command's help from.
func Tree() *cobra.Command {
	root := tree(app.New(ui.New(os.Stdin, os.Stdout, os.Stderr), buildOf(""), nil))
	// The library adds the help command to the tree only when the program runs.
	root.InitDefaultHelpCmd()
	return root
}

// tree is the root command with every command under it.
func tree(a *app.App) *cobra.Command {
	// A command's own hook must not replace the root's, which reads the global flags.
	cobra.EnableTraverseRunHooks = true
	// The help lists the commands in the order they are added: the commonest first.
	cobra.EnableCommandSorting = false

	root := &cobra.Command{
		Use:   "pethost",
		Short: "Put a folder online, and see what runs on your machine",
		Long: `Pethost runs as many of your projects as fit in your plan, at one flat price.

  pethost deploy    puts this folder online; again, a new version
  pethost           shows this folder's project, or everything on the machine`,
		Example: `  pethost deploy
  pethost
  pethost logs -f
  pethost why`,
		Version:       a.Build.Version,
		Args:          noSuchCommand,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRun: func(*cobra.Command, []string) {
			a.Start()
		},
	}
	root.SetVersionTemplate("pethost {{.Version}}\n")
	root.SetFlagErrorFunc(wrongFlag)

	a.Flags(root.PersistentFlags())
	_ = root.RegisterFlagCompletionFunc("project", target.CompleteProjects(a))

	for _, commands := range [][]*cobra.Command{
		kernel.Commands(a), observe.Commands(a), reach.Commands(a), change.Commands(a), account.Commands(a), panelCommands(a),
	} {
		root.AddCommand(commands...)
	}

	// The bare command is the status.
	if status, _, err := root.Find([]string{"status"}); err == nil {
		root.RunE = status.RunE
	}

	landGuesses(root)
	// The help adds commands of its own, which are held to their arguments as the others are.
	account.Help(root)
	strictArguments(root)
	completeNoFiles(root)
	return root
}

// takeLocalPaths are the commands whose arguments are files of this computer, which a shell
// completes by itself.
var takeLocalPaths = []string{"pethost deploy", "pethost cp", "pethost env push"}

// completeNoFiles keeps a shell from offering this folder's files to a command that takes none:
// a version, a host and a path on the machine are not files here. A command that completes
// its own arguments is left as it is.
func completeNoFiles(cmd *cobra.Command) {
	if cmd.ValidArgsFunction == nil && len(cmd.ValidArgs) == 0 && !slices.Contains(takeLocalPaths, cmd.CommandPath()) {
		cmd.ValidArgsFunction = cobra.NoFileCompletions
	}
	for _, sub := range cmd.Commands() {
		completeNoFiles(sub)
	}
}
