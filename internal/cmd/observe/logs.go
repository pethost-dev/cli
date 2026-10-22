package observe

import (
	"context"
	"fmt"

	pethost "github.com/pethost-dev/sdk/go"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/target"
)

// logFlags are what `pethost logs` was asked for.
type logFlags struct {
	service string
	follow  bool
	since   string
	grep    string
	lines   uint32
	deploy  bool
}

// logsCommand is `pethost logs [service]`.
func logsCommand(a *app.App) *cobra.Command {
	var flags logFlags
	cmd := &cobra.Command{
		Use:   "logs [service]",
		Short: "Container output: follow it, search it, or read the build log",
		Long: `What the project's containers wrote, every service's or one's: the newest 100 lines, or with
--since every line from then on. -f goes on with each new line until Ctrl-C.

--deploy is the newest deploy's own log instead, the build's and the start's; with -f it
follows a deploy that still runs to its end. A deploy that sent no files and succeeded, as
a change of the address or a rollback, is passed over: the line under the log says which
deploy it is.`,
		Example: `  pethost logs
  pethost logs web -f
  pethost logs --since 1h --grep error
  pethost logs -n 500
  pethost logs --deploy`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: target.CompleteServices(a),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				flags.service = args[0]
			}
			if flags.deploy {
				return runDeployLog(cmd, a, flags)
			}
			return runLogs(cmd, a, flags)
		},
	}
	cmd.Flags().BoolVarP(&flags.follow, "follow", "f", false, "go on printing new lines as they are written")
	cmd.Flags().StringVar(&flags.since, "since", "", sinceHelp)
	cmd.Flags().StringVar(&flags.grep, "grep", "", "only lines that contain this text, in any case")
	cmd.Flags().Uint32VarP(&flags.lines, "lines", "n", 0, "how many of the newest lines (100; with --since, every line)")
	cmd.Flags().BoolVar(&flags.deploy, "deploy", false, "the newest deploy's own log instead")
	return cmd
}

// logs is one run of `pethost logs` on container output.
type logs struct {
	ctx     context.Context
	a       *app.App
	client  *pethost.Client
	project target.Project
	flags   logFlags
	since   since
	printer *logPrinter
}

func runLogs(cmd *cobra.Command, a *app.App, flags logFlags) error {
	ctx := cmd.Context()
	from, err := sinceOf(cmd, flags.since)
	if err != nil {
		return err
	}
	project, err := target.ForRead(ctx, a)
	if err != nil {
		return err
	}
	client, err := a.Client()
	if err != nil {
		return err
	}

	l := &logs{ctx: ctx, a: a, client: client, project: project, flags: flags, since: from, printer: &logPrinter{out: a.UI.Out}}
	cursor, err := l.past()
	if err != nil || !flags.follow {
		return err
	}
	return l.follow(cursor)
}

// filter is which lines the machine is asked for, in the query and in the stream alike.
func (l *logs) filter() *pethost.ContainerLogFilter {
	return &pethost.ContainerLogFilter{Service: l.flags.service, TextContains: l.flags.grep}
}

// past prints the lines that are there, and returns where a stream goes on after them.
func (l *logs) past() (cursor string, err error) {
	shown := 0
	if l.since.given() && l.flags.lines == 0 {
		shown, cursor, err = l.everyLine()
	} else {
		shown, cursor, err = l.newestLines(l.count())
	}
	if err != nil {
		return "", err
	}

	if shown == 0 {
		l.sayNothing()
	}
	return cursor, nil
}

// defaultLines is how many of the newest lines are shown when -n does not say.
const defaultLines = 100

func (l *logs) count() int {
	if l.flags.lines == 0 {
		return defaultLines
	}
	return int(l.flags.lines)
}

// show writes one page of lines, or with --json the API's answer that holds them.
func (l *logs) show(page *pethost.QueryContainerLogsResponse) error {
	if l.a.JSON {
		return l.a.PrintJSON(page)
	}
	l.printer.page(page.Lines)
	return nil
}

// sayNothing says that no line was there to print, in the words of what was asked for: a
// service's name that was mistyped is then seen.
func (l *logs) sayNothing() {
	said := "No lines"
	if l.flags.service != "" {
		said += " from " + l.flags.service
	}
	if l.flags.grep != "" {
		said += fmt.Sprintf(" with %q", l.flags.grep)
	}
	if l.since.given() {
		said += " " + l.since.words(l.a.UI.Err, 0)
	}
	if said == "No lines" {
		said += " yet"
	}

	if l.flags.follow {
		said += ": new ones show here. Ctrl-C leaves"
	}
	l.a.UI.Info("%s.", said)
}
