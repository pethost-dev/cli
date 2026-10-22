package change

import (
	"context"
	"fmt"
	"slices"
	"strings"

	pethost "github.com/pethost-dev/sdk/go"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/operation"
	"github.com/pethost-dev/cli/internal/target"
	"github.com/pethost-dev/cli/internal/ui"
)

// volumeCommand is `pethost volume`: the project's volumes, where each is mounted and when it
// was last backed up; `rm` deletes one that compose.yaml no longer declares.
func volumeCommand(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "volume",
		Short: "The project's volumes",
		Long: `The project's volumes: its lasting data. A volume survives deploys, and also its
removal from compose.yaml: it then keeps its data, mounted by nothing, until
"pethost volume rm" deletes it.`,
		Example: `  pethost volume
  pethost volume rm old-data`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return listVolumes(cmd.Context(), a)
		},
	}
	cmd.AddCommand(volumeRmCommand(a))
	return cmd
}

func listVolumes(ctx context.Context, a *app.App) error {
	project, err := target.ForRead(ctx, a)
	if err != nil {
		return err
	}
	client, err := a.Client()
	if err != nil {
		return err
	}
	answer, err := client.GetProject(ctx, &pethost.GetProjectRequest{ProjectID: project.ID})
	if err != nil {
		return err
	}
	if a.JSON {
		return a.PrintJSON(answer)
	}

	if len(answer.Project.Volumes) == 0 {
		a.UI.Info("%s has no volume: what its containers write is lost when they are recreated.", ui.Clean(project.ID))
		return nil
	}

	out := a.UI.Out
	table := out.Table()
	for _, volume := range answer.Project.Volumes {
		table.Row(out.Bold(ui.Clean(volume.Volume)), ui.Size(volume.SizeBytes), mountedBy(out, volume), lastBackup(out, answer.Project, volume))
	}
	table.Flush()

	if at := slices.IndexFunc(answer.Project.Volumes, func(volume pethost.Volume) bool { return !volume.Declared }); at >= 0 {
		rm := "pethost volume rm " + word(answer.Project.Volumes[at].Volume) + project.Flag()
		a.UI.Info("A volume that compose.yaml no longer declares keeps its data. Delete it:  %s", a.UI.Err.Bold(rm))
	}
	return nil
}

// mountedBy says where a volume is mounted: "db:/var/lib/postgresql/data".
func mountedBy(out *ui.Stream, volume pethost.Volume) string {
	if !volume.Declared {
		return out.Yellow("not declared any more: no service mounts it")
	}
	if len(volume.MountedBy) == 0 {
		return out.Dim("mounted by no service")
	}
	mounts := make([]string, len(volume.MountedBy))
	for i, mount := range volume.MountedBy {
		mounts[i] = ui.Clean(mount.Service) + ":" + ui.Clean(mount.ContainerPath)
	}
	return strings.Join(mounts, ", ")
}

// lastBackup says when a volume was last backed up; nothing while the machine does not know
// its snapshots, which is not the same as none.
func lastBackup(out *ui.Stream, project *pethost.Project, volume pethost.Volume) string {
	switch {
	case project.SnapshotListTime.IsZero():
		return ""
	case volume.LastBackupTime.IsZero():
		return out.Yellow("in no snapshot")
	}
	return "backed up " + out.When(volume.LastBackupTime)
}

// volumeRmCommand is `pethost volume rm <name>`. Its data is then only in snapshots, if any
// holds it: it asks first.
func volumeRmCommand(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rm <name>",
		Short: "Delete a volume the project no longer declares",
		Long: `Delete a volume that compose.yaml no longer declares, with its data. The data
then lives only in the snapshots that hold it. A volume still declared is refused:
remove it from compose.yaml and deploy first.`,
		Example: `  pethost volume rm old-data
  pethost volume rm old-data --yes -p recipes`,
		Args: cobra.ExactArgs(1),
	}
	noQueue := operation.NoQueueFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		project, err := target.ForChange(cmd.Context(), a)
		if err != nil {
			return err
		}
		return removeVolume(cmd.Context(), a, project, *noQueue, args[0])
	}
	return cmd
}

func removeVolume(ctx context.Context, a *app.App, project target.Project, noQueue bool, name string) error {
	state, err := readBackupState(ctx, a, project.ID)
	if err != nil {
		return err
	}
	if state.project == nil {
		return state.projectErr
	}

	at := slices.IndexFunc(state.project.Volumes, func(volume pethost.Volume) bool { return volume.Volume == name })
	if at < 0 {
		return noSuchVolume(project, state.project, name)
	}

	question := fmt.Sprintf("Delete the volume %s of %s, with its data? %s", ui.Clean(name), ui.Clean(project.ID), afterVolume(a.UI.Err, state, state.project.Volumes[at]))
	if err := askFirst(a, question, "Nothing was deleted."); err != nil {
		return err
	}

	answer, err := act(ctx, a, project, noQueue, "Deleting the volume…", pethost.RunProjectActionDeleteVolume(name))
	if err != nil {
		return err
	}
	if a.JSON {
		return a.PrintJSON(answer)
	}
	a.UI.Done("Deleted the volume %s of %s.", ui.Clean(name), ui.Clean(project.ID))
	return nil
}

// noSuchVolume ends the command before its question, which would ask about data that the
// machine has just said is not there: the name is none of the volumes the project was read with.
func noSuchVolume(project target.Project, read *pethost.Project, name string) error {
	if len(read.Volumes) == 0 {
		return fail.Failed("%s has no volume.", ui.Clean(project.ID))
	}
	names := make([]string, len(read.Volumes))
	for i, volume := range read.Volumes {
		names[i] = ui.Clean(volume.Volume)
	}
	return fail.Failed("%s has no volume %s. Its volumes: %s.", ui.Clean(project.ID), ui.Clean(name), strings.Join(names, ", ")).
		WithLead("See them:", "pethost volume"+project.Flag())
}

// afterVolume says where a volume's data is once the volume is deleted, by what the machine
// knows of its snapshots.
func afterVolume(stderr *ui.Stream, state *backupState, volume pethost.Volume) string {
	switch {
	case !state.backupsOn():
		return "Backups are off: it is gone for good."
	case state.project.SnapshotListTime.IsZero():
		return "It then lives only in the snapshots that hold it, if any does."
	case volume.SnapshotCount == 0:
		return "No snapshot holds it: it is gone for good."
	}
	return fmt.Sprintf("It then lives only in %s, the newest of %s.", ui.Plural(uint64(volume.SnapshotCount), "snapshot"), stderr.When(volume.LastBackupTime))
}
