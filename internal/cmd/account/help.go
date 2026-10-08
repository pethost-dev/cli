package account

import (
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/fail"
)

// Help gives the command tree its help: what `pethost help`, `pethost help <command>` and
// --help print. The root calls it once, with every command added.
//
// The help of the program itself is a list by task: what a person comes to do, each with the
// one command that does it, then every command under what it is for. A command's help is its
// summary, how it is typed, its commands and flags, and last its examples; an alias is a wrong
// guess that lands, and is not shown.
func Help(root *cobra.Command) {
	wordCompletion(root)
	root.SetUsageTemplate(usageTemplate)
	ofCommand := root.HelpFunc()
	root.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		if cmd != root {
			ofCommand(cmd, args)
			return
		}
		fmt.Fprint(cmd.OutOrStdout(), overview(root))
	})
	help := helpCommand(root)
	root.SetHelpCommand(help)
	hideHelpFlag(root)
	hideHelpFlag(help) // The library adds it to the tree only when the program runs.
}

// hideHelpFlag takes -h, --help out of every command's list of flags: each command has it, and
// the library lists it under `deploy -h` but not under `help deploy`, where it has not been
// added yet. The flag itself works as before.
func hideHelpFlag(cmd *cobra.Command) {
	cmd.InitDefaultHelpFlag()
	if flag := cmd.Flags().Lookup("help"); flag != nil {
		flag.Hidden = true
	}
	for _, sub := range cmd.Commands() {
		hideHelpFlag(sub)
	}
}

// wordCompletion puts cobra's own completion command into the CLI's words. cobra adds the
// command when the program runs; it is added here, earlier, to be listed with the others and
// to end with examples as they do.
func wordCompletion(root *cobra.Command) {
	root.InitDefaultCompletionCmd()
	completion, _, err := root.Find([]string{"completion"})
	if err != nil || completion == root {
		return
	}

	completion.Short = "Print a shell's completions: bash, zsh, fish or powershell"
	completion.Long = `Print the completions of your shell. They complete commands and flags, and the
names of your projects and services, which they ask your machine for.`
	// Alone it shows this help; with a word that is no shell it is a wrong command line, which
	// the library would answer with the help and no error.
	completion.Args = cobra.NoArgs
	completion.RunE = func(cmd *cobra.Command, _ []string) error { return cmd.Help() }

	var examples []string
	for _, shell := range shells {
		examples = append(examples, "  "+shell.install)
		command, _, err := completion.Find([]string{shell.name})
		if err != nil || command == completion {
			continue
		}
		command.Short = "The completions of " + shell.name
		command.Long = shell.long
		command.Example = "  " + shell.install + "\n  " + shell.now
	}
	completion.Example = strings.Join(examples, "\n")
}

// shells are the shells that have completions, each with how a person gets them: the line that
// keeps them for every new shell, and the one for the shell they are in.
var shells = []struct{ name, long, install, now string }{
	{"zsh", `Print zsh's completions. Written once into a folder of zsh's fpath, they are there in
every new shell. zsh needs its completion system on: autoload -U compinit; compinit
in ~/.zshrc.`,
		`pethost completion zsh > "${fpath[1]}/_pethost"`,
		`source <(pethost completion zsh)          this shell only`},
	{"fish", `Print fish's completions. Written once into fish's completions folder, they are
there in every new shell.`,
		`pethost completion fish > ~/.config/fish/completions/pethost.fish`,
		`pethost completion fish | source          this shell only`},
	{"bash", `Print bash's completions. A line in ~/.bashrc loads them into every new shell. bash
needs the bash-completion package.`,
		`echo 'source <(pethost completion bash)' >> ~/.bashrc`,
		`source <(pethost completion bash)         this shell only`},
	{"powershell", `Print PowerShell's completions. Added once to your profile ($PROFILE), they are
there in every new session.`,
		`pethost completion powershell >> $PROFILE`,
		`pethost completion powershell | Out-String | Invoke-Expression          this session only`},
}

// helpCommand is `pethost help [command]`.
func helpCommand(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:   "help [command]",
		Short: "What pethost does, or how one command is used",
		Example: `  pethost help
  pethost help deploy
  pethost help env set`,
		ValidArgsFunction: func(_ *cobra.Command, args []string, _ string) ([]cobra.Completion, cobra.ShellCompDirective) {
			var names []cobra.Completion
			if under, rest, err := root.Find(args); err == nil && len(rest) == 0 {
				for _, command := range under.Commands() {
					if command.IsAvailableCommand() {
						names = append(names, cobra.CompletionWithDesc(command.Name(), command.Short))
					}
				}
			}
			return names, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(_ *cobra.Command, args []string) error {
			command, rest, err := root.Find(args)
			if err != nil || len(rest) > 0 {
				return noSuchTopic(root, args)
			}
			return command.Help()
		},
	}
}

// noSuchTopic answers a command that does not exist as the program itself answers one: with
// the commands it may have meant.
func noSuchTopic(root *cobra.Command, args []string) error {
	message := fmt.Sprintf("pethost has no command %q.", strings.Join(args, " "))
	var meant []string
	if len(args) == 1 {
		for _, suggestion := range Near(root, args[0]) {
			meant = append(meant, "pethost help "+suggestion)
		}
	}
	if len(meant) == 0 {
		return fail.Choice(message).WithLead("Every command:", "pethost help")
	}
	refusal := fail.Choice(message, meant...)
	refusal.Lead = "Did you mean:"
	return refusal
}

// Near are the commands under cmd that a mistyped word may have meant: those it starts, those a
// guess is written for, and those a letter or two off it. A short word is two letters off
// every other short word, so it may be off by one only.
func Near(cmd *cobra.Command, typed string) []string {
	cmd.SuggestionsMinimumDistance = 2
	if len(typed) <= 3 {
		cmd.SuggestionsMinimumDistance = 1
	}
	return cmd.SuggestionsFor(typed)
}

// tasks are what a person most often comes to do, the commonest first, each with the command
// that does it. The command's first example is shown: its own area wrote it.
var tasks = []struct{ title, command string }{
	{"Put a folder online", "deploy"},
	{"See what runs", "status"},
	{"My site is broken", "why"},
	{"Read the logs", "logs"},
	{"Secrets", "env set"},
	{"A domain of my own", "domain add"},
	{"The database on my laptop", "tunnel"},
	{"Go back a version", "rollback"},
}

// sections are every command by what it is for. A command that no section names is listed
// under the last one, so that a new command is never hidden.
var sections = []struct {
	title    string
	commands []string
}{
	{"Deploy and look", []string{"deploy", "status", "ls", "why", "logs", "requests", "open"}},
	{"Reach into a container", []string{"run", "shell", "tunnel", "files", "cp"}},
	{"Change a project", []string{"env", "domain", "password", "rename", "rollback", "restart", "stop", "start", "backup", "volume", "github", "delete"}},
	{"The machine, and this program", []string{"machine", "login", "logout", "whoami", "update", "completion", "api", "help"}},
}

// overview is the help of the program itself.
func overview(root *cobra.Command) string {
	var out strings.Builder
	out.WriteString("Pethost runs your projects on a machine of your own.\n\n")
	writeTasks(&out, root)
	writeSections(&out, root)
	out.WriteString("\npethost help <command> shows how a command is used, with examples.\n")
	out.WriteString("Every command takes   -p <project>   --json   -y, --yes   -q   --debug\n")
	return out.String()
}

func writeTasks(out *strings.Builder, root *cobra.Command) {
	width := 0
	for _, task := range tasks {
		width = max(width, len(task.title))
	}
	for _, task := range tasks {
		command, _, err := root.Find(strings.Fields(task.command))
		if err != nil || command == root {
			continue
		}
		fmt.Fprintf(out, "  %-*s   %s\n", width, task.title, firstExample(command))
	}
}

// firstExample is the first of a command's examples, the commonest; the command itself when it
// has none.
func firstExample(command *cobra.Command) string {
	first, _, _ := strings.Cut(strings.TrimSpace(command.Example), "\n")
	if first == "" {
		return command.CommandPath()
	}
	return strings.TrimSpace(first)
}

func writeSections(out *strings.Builder, root *cobra.Command) {
	listed := map[string]bool{}
	for i, section := range sections {
		names := section.commands
		for _, name := range names {
			listed[name] = true
		}
		if i == len(sections)-1 {
			names = append(slices.Clone(names), unlisted(root, listed)...)
		}

		fmt.Fprintf(out, "\n%s\n", section.title)
		for _, name := range names {
			if command := subcommand(root, name); command != nil {
				fmt.Fprintf(out, "  %-*s   %s\n", root.NamePadding(), command.Name(), command.Short)
			}
		}
	}
}

// subcommand is the root's command of a name, if it is one to show.
func subcommand(root *cobra.Command, name string) *cobra.Command {
	for _, command := range root.Commands() {
		if command.Name() == name && (command.IsAvailableCommand() || name == "help") {
			return command
		}
	}
	return nil
}

func unlisted(root *cobra.Command, listed map[string]bool) []string {
	var names []string
	for _, command := range root.Commands() {
		if command.IsAvailableCommand() && !listed[command.Name()] {
			names = append(names, command.Name())
		}
	}
	return names
}

const usageTemplate = `Usage:{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if .HasAvailableSubCommands}}

Commands:{{range .Commands}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

Global flags:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasExample}}

Examples:
{{.Example}}{{end}}
`
