package change

import (
	"context"
	"strings"
	"time"

	pethost "github.com/pethost-dev/sdk/go"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/operation"
	"github.com/pethost-dev/cli/internal/target"
	"github.com/pethost-dev/cli/internal/ui"
)

// rollbackCommand is `pethost rollback [version]`: the project's files as an earlier deploy had
// them, deployed as a new version. A project of files goes back to a deploy whose files the
// machine keeps; a GitHub project to a commit. Nothing is asked: rollback again undoes it.
func rollbackCommand(a *app.App) *cobra.Command {
	var list bool
	cmd := &cobra.Command{
		Use:   "rollback [version]",
		Short: "Go back to the version before, or to one of --list",
		Long: `Deploy the project's files as an earlier version had them: the version before,
or the one named, by its id in "pethost rollback --list". The machine keeps the
files of the three versions before the current one. A project that deploys from
GitHub goes back to a commit, and stops following its branch until told to.

/.env, the addresses and the volumes' data are not part of a version: they stay.`,
		Example: `  pethost rollback
  pethost rollback --list
  pethost rollback k3v9x2m1qa`,
		Args: cobra.MaximumNArgs(1),
	}
	cmd.Flags().BoolVar(&list, "list", false, "list the versions to go back to")
	noQueue := operation.NoQueueFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if list {
			return listVersions(cmd.Context(), a)
		}

		project, err := target.ForChange(cmd.Context(), a)
		if err != nil {
			return err
		}
		r := &rollback{a: a, project: project, noQueue: *noQueue}
		if len(args) == 1 {
			r.version = args[0]
		}
		return againIfChanged(func() error { return r.run(cmd.Context()) })
	}
	return cmd
}

// A rollback is one run of the command.
type rollback struct {
	a       *app.App
	project target.Project
	noQueue bool
	// The version named: a deploy's id, or a commit's SHA. Empty = the version before.
	version string
}

var rollbackWords = operation.Words{Doing: "Going back…", Failed: "Rollback failed"}

// run reads the project, which says where its files come from and which versions it keeps,
// and goes back on that read: a deploy in between makes the machine refuse it, and it runs
// again.
func (r *rollback) run(ctx context.Context) error {
	client, err := r.a.Client()
	if err != nil {
		return err
	}
	answer, err := client.GetProject(ctx, &pethost.GetProjectRequest{ProjectID: r.project.ID})
	if err != nil {
		return err
	}

	if github, ok := githubSource(answer.Project); ok {
		return r.toCommit(ctx, client, answer.Project, github)
	}
	return r.toDeploy(ctx, answer.Project)
}

// githubSource is the repository a project's files come from; false for a project of files.
func githubSource(project *pethost.Project) (pethost.GithubSource, bool) {
	if project == nil || project.Source == nil {
		return pethost.GithubSource{}, false
	}
	github, ok := project.Source.Kind.(pethost.ProjectSourceGithub)
	return pethost.GithubSource(github), ok
}

// toDeploy goes back to an earlier deploy's files.
func (r *rollback) toDeploy(ctx context.Context, project *pethost.Project) error {
	version, err := r.keptVersion(project)
	if err != nil {
		return err
	}

	done, err := deploy(ctx, r.a, r.project, r.noQueue, &pethost.DeployProjectRequest{
		BaseDeployID: project.DeployID,
		Version:      pethost.DeployProjectRollbackDeployID(version.OperationID),
	}, rollbackWords)
	if err != nil {
		return err
	}

	screen := r.a.UI
	which := "the version " + ui.Clean(version.OperationID)
	if at := deployedAt(version); !at.IsZero() {
		which = "the version deployed " + screen.Err.When(at)
	}
	screen.Done("Back on %s, in %s.", which, done.took())
	screen.Info("  %s", whatStays)
	// Whether the folder's code is newer than this version the machine does not say: a rollback
	// of a rollback goes forward, and another computer may have deployed last.
	if r.project.Source == target.FromFile {
		screen.Info("  This folder is as it was: the next %s puts its code online again.", screen.Err.Bold("pethost deploy"))
	} else {
		screen.Info("  The folder it was deployed from is as it was: the next %s there puts its code online again.", screen.Err.Bold("pethost deploy"))
	}
	return r.said(done)
}

// said ends a rollback that went well. The machine's own notes of it say what the lines above
// have said (whose files it took, that /.env and x-pethost were kept), so they are left out.
func (r *rollback) said(done *outcome) error {
	if r.a.JSON {
		return r.a.PrintJSON(done.answer)
	}
	done.Problems(r.a)
	return nil
}

// whatStays says what a rollback does not touch, as the API states it of every version.
const whatStays = "/.env, the routes and the volumes' data stay as they are now."

// keptVersion is the deploy to go back to among those whose files the machine keeps: the
// newest, or the one named by its id or by the id's start. A name that fits none goes to the
// machine as it is, which then says what it has.
func (r *rollback) keptVersion(project *pethost.Project) (pethost.Operation, error) {
	kept := keptDeploys(project)
	if r.version == "" {
		if len(kept) == 0 {
			return pethost.Operation{}, fail.Failed("%s has no earlier version to go back to.", ui.Clean(project.ProjectID))
		}
		return kept[0], nil
	}

	var fits []pethost.Operation
	for _, deploy := range kept {
		if deploy.OperationID == r.version {
			return deploy, nil
		}
		if strings.HasPrefix(deploy.OperationID, r.version) {
			fits = append(fits, deploy)
		}
	}
	if len(fits) == 1 {
		return fits[0], nil
	}
	return pethost.Operation{OperationID: r.version}, nil
}

// keptDeploys are the deploys whose files the machine keeps for a rollback, newest first.
func keptDeploys(project *pethost.Project) []pethost.Operation {
	var kept []pethost.Operation
	for _, operation := range project.Operations {
		if operation.Kind == pethost.OperationKindDeploy && operation.FilesKept {
			kept = append(kept, operation)
		}
	}
	return kept
}

// deployedAt is when a deploy ended, or started while its end is not known.
func deployedAt(deploy pethost.Operation) time.Time {
	if deploy.FinishTime.IsZero() {
		return deploy.StartTime
	}
	return deploy.FinishTime
}
