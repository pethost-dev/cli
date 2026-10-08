package change

import (
	"context"
	"fmt"

	pethost "github.com/pethost-dev/sdk/go"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/operation"
	"github.com/pethost-dev/cli/internal/target"
	"github.com/pethost-dev/cli/internal/ui"
)

// deleteCommand is `pethost delete`: the project with its containers, volumes, files,
// addresses and logs. The person types the project's id to go on; --yes stands for that only
// beside -p, so that a script never deletes whatever project its folder happens to name. While
// backups are on a final snapshot stays and brings the project back; while they are off the
// command says that it is gone for good.
func deleteCommand(a *app.App) *cobra.Command {
	var skipFinalBackup bool
	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete the project",
		Long: `Delete the project: its containers, volumes, files, addresses and logs.

While the machine's backups are on, a final backup is taken first and the project's
snapshots stay: "pethost backup restore" brings it back. While they are off, it is
gone for good.

It asks you to type the project's id. Where nobody can be asked, name the project
with -p and say --yes.

  --skip-final-backup   deletes without the final backup: what changed since the
                        newest snapshot is lost`,
		Example: `  pethost delete
  pethost delete -p recipes --yes`,
		Args: cobra.NoArgs,
	}
	cmd.Flags().BoolVar(&skipFinalBackup, "skip-final-backup", false, "take no final backup first")
	noQueue := operation.NoQueueFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		project, err := target.ForChange(cmd.Context(), a)
		if err != nil {
			return err
		}
		d := &deletion{a: a, project: project, noQueue: *noQueue, skipFinalBackup: skipFinalBackup}
		return d.run(cmd.Context())
	}
	return cmd
}

// A deletion is one run of the command.
type deletion struct {
	a               *app.App
	project         target.Project
	noQueue         bool
	skipFinalBackup bool
}

func (d *deletion) run(ctx context.Context) error {
	if err := d.namedForYes(); err != nil {
		return err
	}
	state, err := readBackupState(ctx, d.a, d.project.ID)
	if err != nil {
		return err
	}
	if state.project == nil {
		return state.projectErr
	}

	yes, err := d.a.UI.ConfirmName(d.question(state), d.project.ID, d.unasked("--yes"))
	if err != nil {
		return err
	}
	if !yes {
		return fail.Declined("That is not its id: nothing was deleted.")
	}

	done, err := operate(ctx, d.a, d.project, d.noQueue,
		pethost.RunProjectActionDeleteProject{SkipFinalBackup: d.skipFinalBackup},
		operation.Words{Doing: "Deleting…", Failed: "It was not deleted"})
	if err != nil {
		return err
	}
	if d.a.JSON {
		return d.a.PrintJSON(done.answer)
	}
	d.sayDeleted(done, state)
	return nil
}

// namedForYes refuses --yes without -p: a yes given ahead must name what it is a yes to.
func (d *deletion) namedForYes() error {
	if !d.a.UI.Yes || d.project.Source == target.FromFlag {
		return nil
	}
	return fail.Choice("--yes deletes without a question, so it wants the project named beside it.", d.unasked())
}

// unasked is this command line as one that deletes without a question: with the project named,
// and with what else it lacks for that.
func (d *deletion) unasked(more ...string) string {
	if d.project.Source != target.FromFlag {
		more = append([]string{"-p", d.project.ID}, more...)
	}
	return d.a.CommandLine(more...)
}

// question says what goes and what stays, by the machine's facts, before the id is typed.
func (d *deletion) question(state *backupState) string {
	project := state.project
	id := ui.Clean(project.ProjectID)

	goes := []string{ui.Plural(uint64(len(project.Services)), "service")}
	if volumes := volumeSizes(project); volumes != "" {
		goes = append(goes, volumes)
	}
	goes = append(goes, "its files", "its logs")
	if project.URL != "" {
		goes = append(goes, "its address "+project.URL)
	}
	says := "This deletes " + id + ": " + listed(goes) + "."

	switch {
	case !state.backupsOn():
		says += "\nBackups are off on your machine: it is gone for good."
	case d.skipFinalBackup && hasNoSnapshot(project):
		says += "\nNo final backup is taken, and it has no snapshot: it is gone for good."
	case d.skipFinalBackup:
		says += "\nNo final backup is taken: its snapshots stay, without what changed since the newest."
	default:
		says += "\nA final backup is taken first, and its snapshots stay: one brings it back."
	}
	return says
}

// hasNoSnapshot says whether the machine knows the project's snapshots and there is none.
func hasNoSnapshot(project *pethost.Project) bool {
	return !project.SnapshotListTime.IsZero() && project.SnapshotCount == 0
}

// volumeSizes names the project's volumes with their sizes: "its volumes data (312 MB) and
// uploads (1.2 GB)".
func volumeSizes(project *pethost.Project) string {
	names := make([]string, len(project.Volumes))
	for i, volume := range project.Volumes {
		names[i] = fmt.Sprintf("%s (%s)", volume.Volume, ui.Size(volume.SizeBytes))
	}
	switch len(names) {
	case 0:
		return ""
	case 1:
		return "its volume " + listed(names)
	}
	return "its volumes " + listed(names)
}

// sayDeleted writes that the project is gone and, when a snapshot of it stays, the command
// that brings it back.
func (d *deletion) sayDeleted(done *outcome, state *backupState) {
	screen, id := d.a.UI, ui.Clean(d.project.ID)
	screen.Done("Deleted %s in %s.", id, done.took())

	switch snapshot := done.Operation.SnapshotID; {
	case snapshot != "":
		back := "pethost backup restore " + shortID(snapshot) + " -p " + word(d.project.ID)
		screen.Info("  Its snapshots stay. To bring it back as it was:  %s", screen.Err.Bold(back))
	case !state.backupsOn():
		screen.Info("  Backups were off: nothing of it is kept.")
	case hasNoSnapshot(state.project):
		screen.Info("  No final backup was taken, and it had no snapshot: nothing of it is kept.")
	default:
		screen.Info("  No final backup was taken. What snapshots it had stay:  %s", screen.Err.Bold("pethost backup -p "+word(d.project.ID)))
	}
	if d.project.Source == target.FromFile {
		screen.Info("  This folder's %s still names it: the next %s here asks before it makes a project again.", target.FileName, screen.Err.Bold("pethost deploy"))
	}
}
