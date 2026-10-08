package change

import (
	"context"
	"fmt"
	"slices"
	"time"

	pethost "github.com/pethost-dev/sdk/go"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/operation"
	"github.com/pethost-dev/cli/internal/target"
	"github.com/pethost-dev/cli/internal/ui"
)

// backupCommand is `pethost backup`: the project's snapshots, as the machine last read their
// list; `now` takes one, `restore` puts one back.
func backupCommand(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "The project's snapshots",
		Long: `The project's snapshots: backups of its files and volumes, kept off the machine,
taken every night and on demand while the machine's backups are on.

A project that was deleted keeps its snapshots: "pethost backup -p <id>" shows
them, and "pethost backup restore <snapshot> -p <id>" brings the project back.`,
		Example: `  pethost backup
  pethost backup now
  pethost backup restore 3f9a1c2e`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return listSnapshots(cmd.Context(), a)
		},
	}
	cmd.AddCommand(backupNowCommand(a), backupRestoreCommand(a))
	return cmd
}

// backupNowCommand is `pethost backup now`.
func backupNowCommand(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "now",
		Short: "Back the project up now",
		Long: `Back up the project's files and volumes now, while it runs. It prints the
snapshot's id.`,
		Example: `  pethost backup now`,
		Args:    cobra.NoArgs,
	}
	noQueue := operation.NoQueueFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		project, err := target.ForChange(cmd.Context(), a)
		if err != nil {
			return err
		}
		done, err := operate(cmd.Context(), a, project, *noQueue, pethost.RunProjectActionBackUp{},
			operation.Words{Doing: "Backing up…", Failed: "Backup failed"})
		if err != nil {
			return err
		}
		if a.JSON {
			return a.PrintJSON(done.answer)
		}

		a.UI.Done("Backed up %s in %s. The snapshot:", ui.Clean(project.ID), done.took())
		_, err = fmt.Fprintln(a.UI.Out, ui.Clean(done.Operation.SnapshotID))
		return err
	}
	return cmd
}

// A backupState is what the machine says of a project's backups: the machine, and the project
// if it is on the machine.
type backupState struct {
	machine *pethost.GetMachineResponse
	// The project's answer; nil = the machine has no such project now.
	answer  *pethost.GetProjectResponse
	project *pethost.Project
	// The machine's answer for the project when it has none: its own sentence.
	projectErr error
}

// readBackupState asks for the machine and the project at once. A project that is not there is
// not a failure yet: a deleted one's snapshots remain, and the machine lists them.
func readBackupState(ctx context.Context, a *app.App, projectID string) (*backupState, error) {
	client, err := a.Client()
	if err != nil {
		return nil, err
	}

	state := &backupState{}
	err = app.Together(
		func() (err error) {
			state.machine, err = client.GetMachine(ctx, nil)
			return err
		},
		func() error {
			answer, err := client.GetProject(ctx, &pethost.GetProjectRequest{ProjectID: projectID})
			if isNotFound(err) {
				state.projectErr = err
				return nil
			}
			if err == nil {
				state.answer, state.project = answer, answer.Project
			}
			return err
		},
	)
	return state, err
}

// deleted is the project among those the machine no longer has and whose snapshots remain.
func (s *backupState) deleted(projectID string) *pethost.DeletedProject {
	at := slices.IndexFunc(s.machine.DeletedProjects, func(project pethost.DeletedProject) bool { return project.ProjectID == projectID })
	if at < 0 {
		return nil
	}
	return &s.machine.DeletedProjects[at]
}

func (s *backupState) backupsOn() bool {
	return s.machine.Machine != nil && s.machine.Machine.BackupsEnabled
}

func listSnapshots(ctx context.Context, a *app.App) error {
	project, err := target.ForRead(ctx, a)
	if err != nil {
		return err
	}
	state, err := readBackupState(ctx, a, project.ID)
	if err != nil {
		return err
	}

	switch {
	case state.project == nil && state.deleted(project.ID) == nil:
		return state.projectErr
	case a.JSON && state.project == nil:
		return a.PrintJSON(state.machine)
	case a.JSON:
		return a.PrintJSON(state.answer)
	case state.project == nil:
		sayDeleted(a, state.deleted(project.ID))
		return nil
	}

	id := ui.Clean(project.ID)
	switch machine := state.machine.Machine; {
	case !state.backupsOn():
		a.UI.Info("Backups are off on your machine: nothing is backed up, and a deleted project or volume is gone for good.")
	case state.project.SnapshotListTime.IsZero():
		a.UI.Info("The machine has not read the list of snapshots yet.")
		if machine.SnapshotListFailureMessage != "" {
			a.UI.Problem("%s", ui.Clean(machine.SnapshotListFailureMessage))
		}
	case len(state.project.Snapshots) == 0:
		a.UI.Info("%s has no snapshot yet. Take one:  %s", id, a.UI.Err.Bold("pethost backup now"+project.Flag()))
	default:
		printSnapshots(a.UI.Out, state.project.Snapshots)
		a.UI.Info("%s", snapshotsLine(state.project, machine))
		a.UI.Info("Put one back:  %s", a.UI.Err.Bold("pethost backup restore "+shortID(state.project.Snapshots[0].SnapshotID)+project.Flag()))
	}
	return nil
}

// printSnapshots writes a row for each snapshot: for a person its id's start, which is what
// they type; for a program the whole id.
func printSnapshots(out *ui.Stream, snapshots []pethost.Snapshot) {
	table := out.Table()
	for _, snapshot := range snapshots {
		id := ui.Clean(snapshot.SnapshotID)
		if out.Terminal {
			id = shortID(snapshot.SnapshotID)
		}
		table.Row(out.Bold(id), out.When(snapshot.CreateTime), snapshotSize(snapshot), snapshotHolds(snapshot))
	}
	table.Flush()
}

// shownID is how many of a snapshot id's 64 characters a person is shown and types.
const shownID = 8

// shortID is a snapshot's id as a command names it for a person: its start, which
// `pethost backup restore` takes, as a word a shell takes.
func shortID(id string) string {
	if len(id) > shownID {
		id = id[:shownID]
	}
	return word(id)
}

func snapshotSize(snapshot pethost.Snapshot) string {
	if snapshot.SizeBytes == 0 {
		return ""
	}
	return ui.Size(snapshot.SizeBytes)
}

// snapshotHolds says what a snapshot holds besides the project's files.
func snapshotHolds(snapshot pethost.Snapshot) string {
	if len(snapshot.Volumes) == 0 {
		return "the files"
	}
	return "the files, " + listed(snapshot.Volumes)
}

// snapshotsLine says how many snapshots the project has in all and how long the machine keeps
// one.
func snapshotsLine(project *pethost.Project, machine *pethost.Machine) string {
	line := ui.Plural(uint64(project.SnapshotCount), "snapshot")
	if shown := len(project.Snapshots); int(project.SnapshotCount) > shown {
		line = fmt.Sprintf("The newest %d of %s", shown, line)
	}

	if machine.BackupRetentionHours == 0 {
		return line + " · kept forever"
	}
	kept := ui.Duration(time.Duration(machine.BackupRetentionHours) * time.Hour)
	return line + " · each kept until it is " + kept + " older than the newest"
}

// sayDeleted writes what remains of a project that was deleted, and how to bring it back.
func sayDeleted(a *app.App, deleted *pethost.DeletedProject) {
	id := ui.Clean(deleted.ProjectID)
	if deleted.NewestSnapshot == nil {
		a.UI.Info("%s was deleted. Its snapshots remain: %d in all.", id, deleted.SnapshotCount)
		return
	}
	a.UI.Info("%s was deleted. Its snapshots remain: %d in all, the newest:", id, deleted.SnapshotCount)
	printSnapshots(a.UI.Out, []pethost.Snapshot{*deleted.NewestSnapshot})
	a.UI.Info("Bring it back as it was then:  %s",
		a.UI.Err.Bold("pethost backup restore "+shortID(deleted.NewestSnapshot.SnapshotID)+" -p "+word(deleted.ProjectID)))
}
