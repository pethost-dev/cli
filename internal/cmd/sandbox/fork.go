package sandbox

import (
	"fmt"
	"time"

	"github.com/pethost-dev/sdk/go/sandboxes"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/ui"
)

// forkCommand is `pethost sandbox fork <id>`.
func forkCommand(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fork <id>",
		Short: "Copy a sandbox as it is now, memory and processes too, into new ones",
		Long: `Copies a sandbox as it is now, its memory, processes and disk, into new running
sandboxes on its server, each with its own id. A running sandbox pauses for a moment and
goes on. The new ids are the answer, one a line. The copies have the sandbox's labels,
unless --label gives theirs, and no name: set gives one.`,
		Example: `  pethost sandbox fork k7f3q2xy0a1b2c3d
  pethost sandbox fork k7f3q2xy0a1b2c3d --count 10 --label run=eval-7`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeSandboxes(a),
	}
	var count int32
	var labels []string
	cmd.Flags().Int32Var(&count, "count", 1, "how many copies, at most 100")
	cmd.Flags().StringArrayVar(&labels, "label", nil, "the copies' label, key=value, in place of the sandbox's (again for more)")

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		c, err := clientForChange(a)
		if err != nil {
			return err
		}
		id := idOf(cmd.Context(), c, args[0])
		started := time.Now()
		live := a.UI.Live("Forking…")
		answer, err := c.ForkSandbox(cmd.Context(), &sandboxes.ForkSandboxRequest{SandboxID: id, Count: count, Labels: labelsOf(labels)})
		live.Stop()
		if err != nil {
			return about(id, err)
		}

		if a.JSON {
			return a.PrintJSON(answer)
		}
		a.UI.Done("Forked %s into %s in %s", ui.Clean(args[0]), countOf(len(answer.Sandboxes)), ui.Duration(time.Since(started)))
		for _, sandbox := range answer.Sandboxes {
			if _, err := fmt.Fprintln(a.UI.Out, ui.Clean(sandbox.SandboxID)); err != nil {
				return err
			}
		}
		return nil
	}
	return cmd
}
