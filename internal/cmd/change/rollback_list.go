package change

import (
	"context"
	"fmt"
	"strings"

	pethost "github.com/pethost-dev/sdk/go"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/say"
	"github.com/pethost-dev/cli/internal/target"
	"github.com/pethost-dev/cli/internal/ui"
)

// rollback --list: the versions a project can go back to. Of a project of files they are the
// deploys whose files the machine keeps, under the one that runs now; of a GitHub project, its
// branch's commits.

func listVersions(ctx context.Context, a *app.App) error {
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

	if _, ok := githubSource(answer.Project); ok {
		return listCommits(ctx, a, client, project)
	}
	if a.JSON {
		return a.PrintJSON(answer)
	}
	listDeploys(a, project, answer.Project)
	return nil
}

// listDeploys writes the deploy whose files the project has, then those it can go back to.
func listDeploys(a *app.App, named target.Project, project *pethost.Project) {
	out := a.UI.Out
	table := out.Table()
	for _, operation := range project.Operations {
		if operation.OperationID == project.DeployID {
			table.Row(deployCells(out, operation, "runs now")...)
		}
	}
	kept := keptDeploys(project)
	for _, deploy := range kept {
		table.Row(deployCells(out, deploy, "")...)
	}
	table.Flush()

	if len(kept) == 0 {
		a.UI.Info("%s has no earlier version to go back to.", ui.Clean(project.ProjectID))
		return
	}
	a.UI.Info("Go back to one:  %s", a.UI.Err.Bold("pethost rollback "+word(kept[0].OperationID)+named.Flag()))
}

// deployCells are a version's row: its id, when it was deployed, what it brought, and a note.
func deployCells(out *ui.Stream, deploy pethost.Operation, note string) []string {
	return []string{
		out.Bold(ui.Clean(deploy.OperationID)),
		"deployed " + out.When(deployedAt(deploy)),
		brought(deploy),
		out.Green(note),
	}
}

// brought says what a deploy brought, by the facts the machine keeps of it: the archive or the
// commit and the paths its request changed, else the first thing the machine says it did.
func brought(deploy pethost.Operation) string {
	var parts []string
	if deploy.GithubCommit != nil {
		parts = append(parts, "commit "+commitWords(deploy.GithubCommit, ""))
	}
	if deploy.ArchiveName != "" {
		parts = append(parts, ui.Clean(deploy.ArchiveName))
	}
	if len(deploy.ChangedPaths) > 0 {
		changed := "changed " + listed(deploy.ChangedPaths)
		if more := int(deploy.ChangedPathCount) - len(deploy.ChangedPaths); more > 0 {
			changed += fmt.Sprintf(" (%d paths in all)", deploy.ChangedPathCount)
		}
		parts = append(parts, changed)
	}
	if len(parts) == 0 && len(deploy.Adjustments) > 0 {
		return ui.Clean(deploy.Adjustments[0])
	}
	return strings.Join(parts, " · ")
}

// listCommits writes the newest commits of a GitHub project's branch.
func listCommits(ctx context.Context, a *app.App, client *pethost.Client, project target.Project) error {
	answer, err := client.ListCommits(ctx, &pethost.ListCommitsRequest{ProjectID: project.ID})
	if err != nil {
		return err
	}
	if a.JSON {
		return a.PrintJSON(answer)
	}

	out := a.UI.Out
	table := out.Table()
	for _, commit := range answer.Commits {
		table.Row(commitCells(out, commit)...)
	}
	table.Flush()

	if answer.More {
		a.UI.Info("Older commits exist: name one by its SHA.")
	}
	a.UI.Info("Go back to one:  %s", a.UI.Err.Bold("pethost rollback <sha>"+project.Flag()))
	return nil
}

// commitCells are a commit's row: its SHA's start, when it was committed, its title and author,
// and how it stands: the one that runs, the branch's newest, how its last deploy went.
func commitCells(out *ui.Stream, commit pethost.BranchCommit) []string {
	sha, title := "", ""
	if commit.Commit != nil {
		sha, title = commitWords(&pethost.GithubCommit{Sha: commit.Commit.Sha}, ""), ui.Clean(commit.Commit.Title)
	}

	var notes []string
	if commit.Deployed {
		notes = append(notes, out.Green("runs now"))
	}
	if commit.Newest {
		notes = append(notes, "the branch's newest")
	}
	if last := commit.LastDeploy; last != nil && !commit.Deployed {
		notes = append(notes, "its deploy "+say.Ended(last.Status))
	}
	return []string{out.Bold(sha), out.When(commit.CommitTime), title, ui.Clean(commit.Author), strings.Join(notes, ", ")}
}
