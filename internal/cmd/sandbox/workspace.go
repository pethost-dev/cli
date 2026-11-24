package sandbox

import (
	"fmt"
	"strings"

	"github.com/pethost-dev/sdk/go/sandboxes"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/ui"
)

// workspaceCommand is `pethost sandbox workspace`.
func workspaceCommand(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "workspace",
		Short: "The workspace of sandboxes: its limits, what it uses, its billing, keys and secrets",
		Long: `Shows the workspace of sandboxes: what its plan allows and what its sandboxes and
templates take of it, what is left of the usage the plan includes and the balance, each
size with its price, and the names of its SSH keys and secrets.`,
		Example: `  pethost sandbox workspace
  pethost sandbox workspace -w research --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client(a)
			if err != nil {
				return err
			}
			workspace, err := c.GetWorkspace(cmd.Context(), &sandboxes.GetWorkspaceRequest{})
			if err != nil {
				return err
			}
			if a.JSON {
				return a.PrintJSON(workspace)
			}
			printWorkspace(a, workspace)
			return nil
		},
	}
}

func printWorkspace(a *app.App, workspace *sandboxes.Workspace) {
	out := a.UI.Out
	table := out.Table()
	table.Row("workspace", out.Bold(ui.Clean(workspace.Name))+" ("+ui.Clean(workspace.WorkspaceID)+")")
	if limits := workspace.Limits; limits != nil {
		table.Row("vCPUs", fmt.Sprintf("%d of %d, sizes up to %s", workspace.VcpusUsed, limits.Vcpus, word(limits.LargestSize)))
		table.Row("templates", fmt.Sprintf("%s of %d GiB, %d versions a name", ui.Size(uint64(workspace.TemplateBytes)), limits.TemplateGib, limits.TemplateVersions))
	}
	if billing := workspace.Billing; billing != nil {
		billingRows(out, table, billing)
	}
	currency := ""
	if workspace.Billing != nil {
		currency = workspace.Billing.Currency
	}
	for _, size := range workspace.Sizes {
		table.Row(word(size.Size), sizeWords(size, currency))
	}
	for _, key := range workspace.SSHKeys {
		table.Row("ssh key", ui.Clean(key.Fingerprint)+" "+ui.Clean(key.Comment))
	}
	for _, secret := range workspace.Secrets {
		table.Row("secret", ui.Clean(secret.Name)+" for "+strings.Join(cleaned(secret.Hosts), ", "))
	}
	table.Flush()
}

// billingRows are what pays for the workspace's usage, a row each.
func billingRows(out *ui.Stream, table *ui.Table, billing *sandboxes.Billing) {
	included := fmt.Sprintf("%s of %s left, until %s", costWords(billing.IncludedMicros, billing.Currency),
		costWords(billing.IncludedTotalMicros, billing.Currency), out.When(billing.PeriodEndTime))
	if billing.Trial {
		included += ", the trial's"
	}
	table.Row("included", included)
	table.Row("balance", costWords(billing.BalanceMicros, billing.Currency))
	if billing.Suspended {
		table.Row("", out.Red("Nothing pays for its usage now: no sandbox starts or wakes until the balance is added to in the panel, or the next period begins."))
	}
}

// sizeWords is a size and its price: "4 vCPUs, 8 GiB memory, 20 GiB disk · $0.036 an hour,
// $0.0108 paused".
func sizeWords(size sandboxes.SizeInfo, currency string) string {
	words := fmt.Sprintf("%d vCPUs, %s memory, %d GiB disk", size.Vcpus, gib(size.MemoryMib), size.DiskGib)
	if size.RunningHour != nil {
		words += " · " + costWords(int64(*size.RunningHour*1e6), currency) + " an hour"
	}
	if size.PausedHour != nil {
		words += ", " + costWords(int64(*size.PausedHour*1e6), currency) + " paused"
	}
	return words
}

// gib is memory in MiB as GiB: "8 GiB".
func gib(mib int32) string {
	if mib%1024 == 0 {
		return fmt.Sprintf("%d GiB", mib/1024)
	}
	return fmt.Sprintf("%.1f GiB", float64(mib)/1024)
}
