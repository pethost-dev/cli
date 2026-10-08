package change

import (
	"context"
	"fmt"
	"strings"
	"time"

	pethost "github.com/pethost-dev/sdk/go"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/ui"
)

// machineCommand is `pethost machine`: the machine's resources, where its disk goes, its
// backups and software, then the SSH keys that open the containers and what is open in them.
func machineCommand(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "machine",
		Short: "The machine: its resources, SSH keys and open sessions",
		Long: `The machine your projects run on: its plan and resources, where its disk goes,
its backups, the SSH keys that open its containers and the sessions open in them
now. The plan itself is changed in the panel.`,
		Example: `  pethost machine
  pethost machine restart
  pethost machine restart --at 03:00
  pethost machine keys rm SHA256:Yx3…
  pethost machine sessions end k3f9`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return showMachine(cmd.Context(), a, func(machine *pethost.Machine) {
				printResources(a, machine)
				a.UI.Info("")
				printKeys(a, machine)
				a.UI.Info("")
				printSessions(a, machine)
			})
		},
	}
	cmd.AddCommand(machineRestartCommand(a), machineKeysCommand(a), machineSessionsCommand(a))
	return cmd
}

// showMachine reads the machine and prints of it what print does; with --json, the API's answer.
func showMachine(ctx context.Context, a *app.App, print func(machine *pethost.Machine)) error {
	client, err := a.Client()
	if err != nil {
		return err
	}
	answer, err := client.GetMachine(ctx, nil)
	if err != nil {
		return err
	}
	if a.JSON {
		return a.PrintJSON(answer)
	}
	if answer.Machine != nil {
		print(answer.Machine)
	}
	return nil
}

// printResources writes the machine as rows of a name and what the machine reports of it.
func printResources(a *app.App, machine *pethost.Machine) {
	out := a.UI.Out
	table := out.Table()

	table.Row("Plan", planWords(out, machine))
	table.Row("CPU", fmt.Sprintf("%s of %s cores in use", cores(machine.CPUUsedCores), cores(machine.CPUTotalCores)))
	table.Row("Memory", ui.SizeOf(machine.MemoryUsedBytes, machine.MemoryTotalBytes)+" in use")
	table.Row("Disk", ui.SizeOf(machine.DiskUsedBytes, machine.DiskTotalBytes)+" in use")
	if shares := diskShares(machine.DiskUsage); shares != "" {
		table.Row("", shares)
	}
	if machine.AppsDomain != "" {
		table.Row("Domain", ui.Clean(machine.AppsDomain)+aside(out, "any name under it is an address at once"))
	}
	table.Row("Name", ui.Clean(machine.Hostname)+aside(out, "what a domain of your own points at"))
	table.Row("Host key", ui.Clean(machine.SSHHostKeyFingerprint))
	for i, words := range backupWords(out, machine) {
		table.Row([]string{"Backups", ""}[min(i, 1)], words)
	}
	table.Row("Software", softwareWords(out, machine))
	table.Flush()

	switch {
	case !machine.ScheduledRestartTime.IsZero():
		fmt.Fprintf(out, "%sIt restarts %s. No restart after all:  pethost machine restart --cancel\n", out.Tilde(), out.When(machine.ScheduledRestartTime))
	case !machine.RestartRequiredTime.IsZero():
		fmt.Fprintf(out, "%sAn update waits for a restart since %s:  pethost machine restart\n", out.Tilde(), out.When(machine.RestartRequiredTime))
	}
}

// aside is a few words beside a value for a person; a program reads the value alone.
func aside(out *ui.Stream, words string) string {
	if !out.Terminal {
		return ""
	}
	return "   " + out.Dim(words)
}

func planWords(out *ui.Stream, machine *pethost.Machine) string {
	plan := out.Bold(ui.Clean(machine.Name))
	if machine.Location != "" {
		plan += " · " + ui.Clean(machine.Location)
	}
	return plan
}

// cores is a number of CPU cores: "2", "0.3".
func cores(count float64) string {
	return strings.TrimSuffix(fmt.Sprintf("%.1f", count), ".0")
}

// diskShares say where the projects' disk goes, as the machine measures it every few minutes.
func diskShares(usage *pethost.DiskUsage) string {
	if usage == nil {
		return ""
	}

	shares := []string{
		"images " + ui.Size(usage.ImagesBytes),
		"volumes " + ui.Size(usage.VolumesBytes),
	}
	if usage.BuildCacheBytes != nil {
		shares = append(shares, "build cache "+ui.Size(*usage.BuildCacheBytes))
	}
	shares = append(shares,
		"containers "+ui.Size(usage.ContainerLayersBytes),
		"project files "+ui.Size(usage.ProjectFilesBytes),
		"request log "+ui.Size(usage.HTTPTrafficBytes),
	)
	return strings.Join(shares, " · ")
}

// backupWords say whether the machine backs its projects up, how long a snapshot is kept, and
// why the list of snapshots is not known when it is not: a line each.
func backupWords(out *ui.Stream, machine *pethost.Machine) []string {
	if !machine.BackupsEnabled {
		return []string{out.Yellow("off: nothing is backed up, and a deleted project or volume is gone for good")}
	}

	words := []string{"on, every night"}
	if machine.BackupRetentionHours > 0 {
		kept := ui.Duration(time.Duration(machine.BackupRetentionHours) * time.Hour)
		words[0] += " · a snapshot is kept until it is " + kept + " older than its project's newest"
	}
	switch {
	case machine.SnapshotListFailureMessage != "":
		words = append(words, out.Red("the list of snapshots could not be read: "+ui.Clean(machine.SnapshotListFailureMessage)))
	case machine.SnapshotListTime.IsZero():
		words = append(words, "the list of snapshots is not read yet")
	}
	return words
}

func softwareWords(out *ui.Stream, machine *pethost.Machine) string {
	words := "petnode " + ui.Clean(machine.DaemonVersion) + " · Docker " + ui.Clean(machine.DockerVersion)
	if !machine.BootTime.IsZero() {
		words += " · up since " + out.At(machine.BootTime)
	}
	return words
}
