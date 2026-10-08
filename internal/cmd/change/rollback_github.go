package change

import (
	"context"

	pethost "github.com/pethost-dev/sdk/go"

	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/ui"
)

// A rollback of a project that deploys from GitHub: an earlier commit of its branch is deployed.
// The machine then stops following the branch (the project is pinned), and the command says so
// and how to follow it again.

// toCommit goes back to a commit: the one named, else the newest one before the deployed commit
// that was itself deployed well.
func (r *rollback) toCommit(ctx context.Context, client *pethost.Client, project *pethost.Project, github pethost.GithubSource) error {
	sha := r.version
	if sha == "" {
		commits, err := client.ListCommits(ctx, &pethost.ListCommitsRequest{ProjectID: r.project.ID})
		if err != nil {
			return err
		}
		before := commitBefore(commits.Commits)
		if before == nil {
			return fail.Choice("None of the branch's newest commits before the deployed one was deployed: name the commit to go back to.").
				WithLead("See them:", "pethost rollback --list"+r.project.Flag())
		}
		sha = before.Sha
	}

	done, err := deploy(ctx, r.a, r.project, r.noQueue, &pethost.DeployProjectRequest{
		BaseDeployID: project.DeployID,
		Version:      pethost.DeployProjectCommit(sha),
	}, rollbackWords)
	if err != nil {
		return err
	}

	screen := r.a.UI
	screen.Done("Back on commit %s, in %s.", commitWords(done.Operation.GithubCommit, sha), done.took())
	screen.Info("  %s", whatStays)
	if now, ok := githubSource(done.Project); ok && now.AutoDeploy != nil && !*now.AutoDeploy {
		follow := "pethost github " + ui.Clean(text(github.Repository)) + r.project.Flag()
		screen.Info("  Auto-deploy is off now: a push no longer deploys. To follow the branch again:  %s", screen.Err.Bold(follow))
	}
	return r.said(done)
}

// commitBefore is the newest commit older than the deployed one whose own deploy succeeded:
// the version that ran before. nil = the page of commits holds none.
func commitBefore(commits []pethost.BranchCommit) *pethost.GithubCommit {
	pastDeployed := false
	for _, commit := range commits {
		switch {
		case commit.Deployed:
			pastDeployed = true
		case pastDeployed && commit.Commit != nil && commit.LastDeploy != nil && commit.LastDeploy.Status == pethost.OperationStatusSucceeded:
			return commit.Commit
		}
	}
	return nil
}

// shortSha is how many characters of a commit's SHA a line shows.
const shortSha = 7

// commitWords are a commit in a line: its SHA's start and its title, `1a2b3c4 "Fix search"`.
// The title is other people's text.
func commitWords(commit *pethost.GithubCommit, sha string) string {
	title := ""
	if commit != nil {
		sha, title = commit.Sha, ui.Clean(commit.Title)
	}
	if len(sha) > shortSha {
		sha = sha[:shortSha]
	}
	if title == "" {
		return ui.Clean(sha)
	}
	return ui.Clean(sha) + ` "` + title + `"`
}

// text is a field that may be absent as text: empty when it is.
func text(field *string) string {
	if field == nil {
		return ""
	}
	return *field
}
