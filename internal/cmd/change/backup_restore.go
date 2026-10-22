package change

import (
	"context"
	"fmt"
	"strings"

	pethost "github.com/pethost-dev/sdk/go"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/operation"
	"github.com/pethost-dev/cli/internal/target"
	"github.com/pethost-dev/cli/internal/ui"
)

// backupRestoreCommand is `pethost backup restore <id>`. Of a project on the machine it
// replaces volumes with the snapshot's, after a backup of them as they are; a deleted project
// it brings back whole. It asks first.
func backupRestoreCommand(a *app.App) *cobra.Command {
	r := &restore{a: a}
	cmd := &cobra.Command{
		Use:   "restore <id>",
		Short: "Put a snapshot's data back",
		Long: `Put a snapshot's data back. Of a project that is on the machine it replaces the
volumes the snapshot holds, or the ones named with --volume: the project is backed
up first, stopped, restored and started again, and its files stay as they are.

A deleted project comes back whole, files, volumes and source, and is deployed:
name it with -p.`,
		Example: `  pethost backup restore 3f9a1c2e
  pethost backup restore 3f9a1c2e --volume data
  pethost backup restore 3f9a1c2e -p recipes --yes`,
		Args: cobra.ExactArgs(1),
	}
	cmd.Flags().StringArrayVar(&r.volumes, "volume", nil, "a volume to replace, by its `name`; all the snapshot holds without it")
	noQueue := operation.NoQueueFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		project, err := target.ForChange(cmd.Context(), a)
		if err != nil {
			return err
		}
		r.project, r.noQueue, r.snapshotID = project, *noQueue, args[0]
		return r.run(cmd.Context())
	}
	return cmd
}

// A restore is one run of the command.
type restore struct {
	a          *app.App
	project    target.Project
	noQueue    bool
	snapshotID string
	// --volume: the volumes to replace. Empty = every volume the snapshot holds.
	volumes []string
}

func (r *restore) run(ctx context.Context) error {
	state, err := readBackupState(ctx, r.a, r.project.ID)
	if err != nil {
		return err
	}

	var question string
	switch deleted := state.deleted(r.project.ID); {
	case state.project != nil:
		question, err = r.replaceVolumes(ctx, state.project)
	case deleted != nil || state.deletedUnknown():
		question = r.bringBack(deleted)
	default:
		return state.projectErr
	}
	if err != nil {
		return err
	}

	if err := r.confirm(question); err != nil {
		return err
	}

	done, err := operate(ctx, r.a, r.project, r.noQueue,
		pethost.RunProjectActionRestoreSnapshot{SnapshotID: r.snapshotID, Volumes: r.volumes},
		operation.Words{Doing: "Restoring…", Failed: "Restore failed"})
	if err != nil {
		return err
	}
	if r.a.JSON {
		return r.a.PrintJSON(done.answer)
	}
	r.sayRestored(done, state.project == nil)
	return nil
}

// confirm asks before anything is replaced. No question = the restore names nothing to replace:
// whether that can be is the machine's to say, so it goes there unasked.
func (r *restore) confirm(question string) error {
	if question == "" {
		return nil
	}
	return askFirst(r.a, question, "Nothing was restored.")
}

// replaceVolumes settles which volumes of a project on the machine the restore replaces, and
// is the question about it. While the machine has not read its list of snapshots, the id and
// the volumes go to it as they were given: the machine judges them.
func (r *restore) replaceVolumes(ctx context.Context, project *pethost.Project) (string, error) {
	held := "in the snapshot " + ui.Clean(r.snapshotID)
	if !project.SnapshotListTime.IsZero() {
		snapshot, err := r.findSnapshot(ctx, project)
		if err != nil {
			return "", err
		}
		r.snapshotID = snapshot.SnapshotID
		if len(r.volumes) == 0 {
			r.volumes = snapshot.Volumes
		}
		held = r.a.UI.Err.When(snapshot.CreateTime)
	}

	if len(r.volumes) == 0 {
		return "", nil
	}
	return fmt.Sprintf("This replaces %s of %s with what %s held %s. The project is backed up first, then stopped, restored and started again. Go on?",
		volumesNamed(r.volumes), ui.Clean(project.ProjectID), itOrThey(r.volumes), held), nil
}

func volumesNamed(volumes []string) string {
	if len(volumes) == 1 {
		return "the volume " + listed(volumes)
	}
	return "the volumes " + listed(volumes)
}

// findSnapshot is the project's snapshot of the id given, or of the only id that starts with
// it, looked for page by page from the newest until every snapshot the machine counts was seen.
func (r *restore) findSnapshot(ctx context.Context, project *pethost.Project) (pethost.Snapshot, error) {
	client, err := r.a.Client()
	if err != nil {
		return pethost.Snapshot{}, err
	}

	var fits []pethost.Snapshot
	seen := 0
	for page := project.Snapshots; len(page) > 0; {
		for _, snapshot := range page {
			if snapshot.SnapshotID == r.snapshotID {
				return snapshot, nil
			}
			if strings.HasPrefix(snapshot.SnapshotID, r.snapshotID) {
				fits = append(fits, snapshot)
			}
		}
		seen += len(page)
		if seen >= int(project.SnapshotCount) {
			break
		}

		older, err := client.GetProject(ctx, &pethost.GetProjectRequest{ProjectID: r.project.ID, SnapshotsBefore: page[len(page)-1].CreateTime})
		if err != nil {
			return pethost.Snapshot{}, err
		}
		page = older.Project.Snapshots
	}

	switch len(fits) {
	case 1:
		return fits[0], nil
	case 0:
		return pethost.Snapshot{}, fail.Failed("%s has no snapshot %s.", ui.Clean(r.project.ID), ui.Clean(r.snapshotID)).
			WithLead("Its snapshots:", "pethost backup"+r.project.Flag())
	}
	return pethost.Snapshot{}, fail.Choice(fmt.Sprintf("%d snapshots of %s have an id that starts with %s: write more of it.",
		len(fits), ui.Clean(r.project.ID), ui.Clean(r.snapshotID))).WithLead("Its snapshots:", "pethost backup"+r.project.Flag())
}

// bringBack is the question before a project the machine does not have is restored whole: a
// deleted one, or nil while the machine has not read its list of snapshots, when the id and the
// project go to it as they were given and it judges them. When the id given is the start of a
// deleted project's newest snapshot's, that snapshot is the one restored.
func (r *restore) bringBack(deleted *pethost.DeletedProject) string {
	id, from := ui.Clean(r.project.ID), "the snapshot "+ui.Clean(r.snapshotID)
	if deleted == nil {
		return fmt.Sprintf("%s is not on the machine. This brings back its files, volumes and source from %s, and deploys it. Go on?", id, from)
	}
	if newest := deleted.NewestSnapshot; newest != nil && strings.HasPrefix(newest.SnapshotID, r.snapshotID) {
		r.snapshotID = newest.SnapshotID
		from = "its snapshot of " + r.a.UI.Err.When(newest.CreateTime)
	}
	return fmt.Sprintf("%s was deleted. This brings back its files, volumes and source from %s, and deploys it. Go on?", id, from)
}

// sayRestored writes what the restore did, by the operation's own facts.
func (r *restore) sayRestored(done *outcome, broughtBack bool) {
	screen, id := r.a.UI, ui.Clean(r.project.ID)
	switch {
	case broughtBack:
		screen.Done("%s is back, in %s.", id, done.took())
	case len(done.Operation.RestoredVolumes) > 0:
		screen.Done("Restored %s of %s in %s.", volumesNamed(done.Operation.RestoredVolumes), id, done.took())
	default:
		screen.Done("Restored %s in %s.", id, done.took())
	}
	if undo := done.Operation.UndoSnapshotID; undo != "" {
		screen.Info("  It was backed up first. To undo:  %s", screen.Err.Bold("pethost backup restore "+shortID(undo)+r.project.Flag()))
	}
	done.Notes(r.a)
	done.Problems(r.a)

	if broughtBack && done.Project != nil && done.Project.URL != "" {
		fmt.Fprintln(screen.Out, ui.Clean(done.Project.URL))
	}
}
