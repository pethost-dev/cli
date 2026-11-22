package sandbox

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/pethost-dev/sdk/go/sandboxes"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/ui"
)

// kinds are the kinds of event a person may name.
var kinds = []sandboxes.SandboxEventKind{
	sandboxes.SandboxEventKindLifecycle, sandboxes.SandboxEventKindCommand, sandboxes.SandboxEventKindEgress,
	sandboxes.SandboxEventKindPort, sandboxes.SandboxEventKindAgentTurn,
}

// eventsCommand is `pethost sandbox events <id>`.
func eventsCommand(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "events <id>",
		Short: "What happened in a sandbox: its states, commands, connections, requests and agents",
		Long: `Shows a sandbox's last events, oldest first, a line each: its changes of state, the
commands run in it, the hosts it connected to or was refused, the requests to its ports
and its agents' turns. -f then follows them as they come, until Ctrl-C.`,
		Example: `  pethost sandbox events k7f3q2xy0a1b2c3d
  pethost sandbox events k7f3q2xy0a1b2c3d -f --kind egress
  pethost sandbox events k7f3q2xy0a1b2c3d -n 100 --json`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeSandboxes(a),
	}
	var follow bool
	var lines int32
	var kindWords []string
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "then show events as they come, until Ctrl-C")
	cmd.Flags().Int32VarP(&lines, "lines", "n", 20, "how many of the last events")
	cmd.Flags().StringSliceVar(&kindWords, "kind", nil, "only these: lifecycle, command, egress, port, agent-turn")

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		var wanted []sandboxes.SandboxEventKind
		for _, typed := range kindWords {
			kind, err := fromWord("kind", typed, kinds...)
			if err != nil {
				return err
			}
			wanted = append(wanted, kind)
		}
		return events(cmd.Context(), a, args[0], wanted, lines, follow)
	}
	return cmd
}

func events(ctx context.Context, a *app.App, id string, wanted []sandboxes.SandboxEventKind, lines int32, follow bool) error {
	c, err := client(a)
	if err != nil {
		return err
	}
	id = idOf(ctx, c, id)
	last, err := c.ListSandboxEvents(ctx, &sandboxes.ListSandboxEventsRequest{SandboxID: id, Kinds: wanted, NewestFirst: true, Limit: lines})
	if err != nil {
		return about(id, err)
	}
	slices.Reverse(last.Events)
	after := ""
	for i := range last.Events {
		if err := printEvent(a, &last.Events[i]); err != nil {
			return err
		}
		after = last.Events[i].EventID
	}
	if !follow {
		return nil
	}

	for event, err := range c.WatchSandboxEvents(ctx, &sandboxes.WatchSandboxEventsRequest{SandboxID: id, Kinds: wanted, After: after}) {
		if ctx.Err() != nil {
			return nil // Ctrl-C is how events are left: it ended well.
		}
		if err != nil {
			return about(id, err)
		}
		if err := printEvent(a, event); err != nil {
			return err
		}
	}
	return nil
}

// printEvent writes an event as a line: its time, its kind and what it says.
func printEvent(a *app.App, event *sandboxes.SandboxEvent) error {
	if a.JSON {
		return a.PrintJSON(event)
	}
	out := a.UI.Out
	kind, what := eventWords(out, event.Event)
	_, err := fmt.Fprintln(out, out.Clock(event.Time)+"  "+out.Dim(fmt.Sprintf("%-10s", kind))+"  "+what)
	return err
}

// eventWords are an event's kind and what it says.
func eventWords(out *ui.Stream, event sandboxes.SandboxEventEvent) (string, string) {
	switch e := event.(type) {
	case sandboxes.SandboxEventLifecycle:
		what := word(e.Change) + " (" + word(e.Reason) + ")"
		if e.Size != sandboxes.SizeUnspecified {
			what += fmt.Sprintf(", %s, disk %d GiB", word(e.Size), e.DiskGib)
		}
		if e.StateMessage != "" {
			what += ": " + ui.Clean(e.StateMessage)
		}
		return "lifecycle", what
	case sandboxes.SandboxEventCommand:
		what := word(e.State)
		if e.ExitCode != nil {
			what += fmt.Sprintf(" %d", *e.ExitCode)
		}
		return "command", fmt.Sprintf("%s  %s  %s, by %s", what, oneLine(e.Command), ui.Millis(uint32(min(e.DurationMs, 1<<31))), word(e.StartedBy))
	case sandboxes.SandboxEventEgress:
		verdict := out.Green("allowed")
		if !e.Allowed {
			verdict = out.Red("refused")
		}
		what := fmt.Sprintf("%s:%d %s, %s, %s out, %s in", ui.Clean(e.Host), e.Port, verdict,
			ui.Plural(uint64(e.Connections), "connection"), ui.Size(uint64(e.BytesOut)), ui.Size(uint64(e.BytesIn)))
		if len(e.Secrets) > 0 {
			what += ", secrets " + oneLine(strings.Join(e.Secrets, ", "))
		}
		return "egress", what
	case sandboxes.SandboxEventPort:
		return "port", fmt.Sprintf("%d  %s, %d server errors, %s in, %s out", e.Port, ui.Plural(uint64(e.Requests), "request"),
			e.ServerErrors, ui.Size(uint64(e.BytesIn)), ui.Size(uint64(e.BytesOut)))
	case sandboxes.SandboxEventAgentTurn:
		return "agent", fmt.Sprintf("%s  %s  %s", word(e.State), word(e.Agent), oneLine(e.Prompt))
	}
	return "event", "of a kind this pethost does not know: update it (pethost update)"
}

// oneLine is text from a sandbox on one line, cut where it is long.
func oneLine(text string) string {
	return cut(strings.Join(strings.Fields(ui.Clean(text)), " "), 80)
}
