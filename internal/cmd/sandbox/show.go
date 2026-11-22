package sandbox

import (
	"fmt"
	"strings"

	"github.com/pethost-dev/sdk/go/sandboxes"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/ui"
)

// showCommand is `pethost sandbox show <id>`.
func showCommand(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "One sandbox: its state, size, ports, how to reach it and what it has cost",
		Long: `Shows one sandbox as it is now: its state and why, its size and template, what its
lifecycle makes of it next and when, its SSH command and its page in the panel, its public
ports and what it has used so far.`,
		Example: `  pethost sandbox show k7f3q2xy0a1b2c3d
  pethost sandbox show api-dev --json`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeSandboxes(a),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client(a)
			if err != nil {
				return err
			}
			id := idOf(cmd.Context(), c, args[0])
			sandbox, err := c.GetSandbox(cmd.Context(), &sandboxes.GetSandboxRequest{SandboxID: id})
			if err != nil {
				return about(id, err)
			}
			if a.JSON {
				return a.PrintJSON(sandbox)
			}
			printSandbox(a, sandbox)
			return nil
		},
	}
}

// printSandbox writes a sandbox as lines of facts, each "name  value", on stdout.
func printSandbox(a *app.App, sandbox *sandboxes.Sandbox) {
	out := a.UI.Out
	table := out.Table()
	table.Row("id", out.Bold(ui.Clean(sandbox.SandboxID)))
	if sandbox.Name != "" {
		table.Row("name", ui.Clean(sandbox.Name))
	}
	table.Row("state", stateLine(out, sandbox))
	if sandbox.StateMessage != "" {
		table.Row("", ui.Clean(sandbox.StateMessage))
	}
	if sandbox.NextState != sandboxes.SandboxStateUnspecified {
		table.Row("next", word(sandbox.NextState)+" "+out.When(sandbox.NextStateTime)+", unless something happens in it")
	}
	table.Row("size", fmt.Sprintf("%s, disk %d GiB", word(sizeOf(sandbox)), sandbox.DiskGib))
	table.Row("template", ui.Clean(sandbox.Template))
	if len(sandbox.Labels) > 0 {
		table.Row("labels", labelsWords(sandbox.Labels))
	}
	for _, port := range publicPorts(sandbox) {
		table.Row(fmt.Sprintf("port %d", port.Port), ui.Clean(port.URL))
	}
	for _, mount := range sandbox.Mounts {
		table.Row("volume", mountWords(mount))
	}
	if sandbox.SSHCommand != "" {
		table.Row("ssh", ui.Clean(sandbox.SSHCommand))
	}
	table.Row("panel", ui.Clean(sandbox.PanelURL))
	if used := usageWords(sandbox.Usage); used != "" {
		table.Row("used", used)
	}
	table.Row("created", out.When(sandbox.CreateTime))
	table.Flush()
}

// stateLine is a sandbox's state, why and since when: "paused (idle) since 14:02". A state a
// call asked for needs no why.
func stateLine(out *ui.Stream, sandbox *sandboxes.Sandbox) string {
	line := stateWord(out, sandbox.State)
	if sandbox.StateReason != sandboxes.StateReasonUnspecified && sandbox.StateReason != sandboxes.StateReasonRequest {
		line += " (" + word(sandbox.StateReason) + ")"
	}
	if !sandbox.StateTime.IsZero() {
		line += " since " + out.At(sandbox.StateTime)
	}
	if restore := sandbox.Restore; restore != nil {
		line += fmt.Sprintf(", its disk fetched: %s of %s", ui.Size(uint64(restore.FetchedBytes)), ui.Size(uint64(restore.TotalBytes)))
	}
	return line
}

// mountWords is a mounted volume: "vol_8f2k3j at /data, read-only".
func mountWords(mount sandboxes.Mount) string {
	words := ui.Clean(mount.VolumeID) + " at " + ui.Clean(mount.Path)
	if mount.ReadOnly {
		words += ", read-only"
	}
	return words
}

// usageWords is what a sandbox has used, and what it was charged in its workspace's currency,
// which pethost sandbox workspace names: "running 12 min, paused 3 h · charged 0.0412".
func usageWords(usage *sandboxes.Usage) string {
	if usage == nil {
		return ""
	}
	var parts []string
	if usage.RunningSeconds > 0 {
		parts = append(parts, "running "+secondsWords(uint32(min(usage.RunningSeconds, 1<<31))))
	}
	if usage.PausedSeconds > 0 {
		parts = append(parts, "paused "+secondsWords(uint32(min(usage.PausedSeconds, 1<<31))))
	}
	if usage.StoredGibHours > 0 {
		parts = append(parts, fmt.Sprintf("stored %.2f GiB-hours", usage.StoredGibHours))
	}
	words := strings.Join(parts, ", ")
	if usage.CostMicros > 0 {
		words += " · charged " + amountWords(usage.CostMicros)
	}
	return words
}
