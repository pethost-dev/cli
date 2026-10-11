package change

import (
	"context"
	"fmt"

	pethost "github.com/pethost-dev/sdk/go"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/operation"
	"github.com/pethost-dev/cli/internal/target"
	"github.com/pethost-dev/cli/internal/ui"
)

// githubCommand is `pethost github [owner/repo]`: bare, the repositories the account's GitHub
// App installation gives Pethost, and where this folder's project takes its files from; with a
// repository, the project deploys from it from now on.
func githubCommand(a *app.App) *cobra.Command {
	var branch, directory string
	cmd := &cobra.Command{
		Use:   "github [owner/repo]",
		Short: "Deploy from a GitHub repository: list them, connect one, or stop",
		Long: `Bare, it lists the repositories Pethost can deploy from: those your installation
of its GitHub App covers. With a repository, the project takes its files from it
from now on: every push to the branch deploys itself, and the same command turns
that back on after a rollback. "pethost github off" goes back to the files as
they are, which "pethost deploy" then replaces.`,
		Example: `  pethost github
  pethost github sam/recipes
  pethost github sam/recipes --branch main --directory apps/web
  pethost github off`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeRepositories(a),
	}
	cmd.Flags().StringVar(&branch, "branch", "", "the branch to follow; the default one without it")
	cmd.Flags().StringVar(&directory, "directory", "", "the project's directory in it; the root without it")
	noQueue := operation.NoQueueFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return showGithub(cmd.Context(), a)
		}

		// A field that is not given keeps its value; auto-deploy is turned on whenever asked.
		source := pethost.GithubSource{Repository: &args[0], AutoDeploy: new(true)}
		if cmd.Flags().Changed("branch") {
			source.Branch = &branch
		}
		if cmd.Flags().Changed("directory") {
			source.Directory = &directory
		}
		return connectGithub(cmd.Context(), a, *noQueue, source)
	}
	cmd.AddCommand(githubOffCommand(a))
	return cmd
}

// connectGithub makes a repository the project's source, and follows the deploy of its newest
// commit when the machine starts one.
func connectGithub(ctx context.Context, a *app.App, noQueue bool, source pethost.GithubSource) error {
	project, err := target.ForChange(ctx, a)
	if err != nil {
		return err
	}
	action := pethost.RunProjectActionSetSource{Kind: pethost.ProjectSourceGithub(source)}
	answer, err := act(ctx, a, project, noQueue, "Connecting the repository…", action)
	if err != nil {
		return err
	}

	a.UI.Done("%s deploys from %s now.", ui.Clean(project.ID), ui.Clean(*source.Repository))
	if answer.Operation == nil {
		if a.JSON {
			return a.PrintJSON(answer)
		}
		saySource(a, answer.Project)
		return nil
	}

	call := started{operation: answer.Operation, project: answer.Project, log: answer.Log, answer: answer}
	done, err := finish(ctx, a, project, call, operation.Words{Doing: "Deploying its newest commit…", Failed: "Deploy failed"})
	if err != nil {
		return err
	}
	a.UI.Done("Deployed commit %s in %s.", commitWords(done.Operation.GithubCommit, ""), done.took())
	saySource(a, done.Project)
	return done.said(a)
}

// githubOffCommand is `pethost github off`.
func githubOffCommand(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "off",
		Short:   "Stop deploying from GitHub: the files stay as they are",
		Example: `  pethost github off`,
		Args:    cobra.NoArgs,
	}
	noQueue := operation.NoQueueFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		project, err := target.ForChange(cmd.Context(), a)
		if err != nil {
			return err
		}
		action := pethost.RunProjectActionSetSource{Kind: pethost.ProjectSourceFiles(true)}
		answer, err := act(cmd.Context(), a, project, *noQueue, "Disconnecting the repository…", action)
		if err != nil {
			return err
		}
		if a.JSON {
			return a.PrintJSON(answer)
		}
		// True of a project that never deployed from GitHub as well: the answer does not say
		// what its source was.
		a.UI.Done("%s does not deploy from GitHub. Its files stay as they are: %s sends new ones.",
			ui.Clean(project.ID), a.UI.Err.Bold("pethost deploy"))
		return nil
	}
	return cmd
}

// showGithub lists the repositories Pethost can deploy from, under the source of the project
// that -p, PETHOST_PROJECT or this folder names, when one does. Nothing is asked: the list is
// the machine's, with or without a project.
func showGithub(ctx context.Context, a *app.App) error {
	project, named, err := target.Named(a)
	if err != nil {
		return err
	}
	client, err := a.Client()
	if err != nil {
		return err
	}

	var machine *pethost.GetMachineResponse
	var answer *pethost.GetProjectResponse
	err = app.Together(
		func() (err error) {
			machine, err = client.GetMachine(ctx, nil)
			return err
		},
		func() (err error) {
			if named {
				answer, err = client.GetProject(ctx, &pethost.GetProjectRequest{ProjectID: project.ID})
			}
			return err
		},
	)
	if err != nil {
		return err
	}
	if a.JSON {
		return a.PrintJSON(machine)
	}

	if named {
		target.Announce(a, project)
		saySource(a, answer.Project)
	}
	printRepositories(a, machine.Github)
	return nil
}

// saySource writes where a project's files come from, and for a repository how it follows it.
func saySource(a *app.App, project *pethost.Project) {
	if project == nil {
		return
	}
	id := ui.Clean(project.ProjectID)
	github, ok := githubSource(project)
	if !ok {
		a.UI.Info("%s takes its files from %s, not from GitHub.", id, a.UI.Err.Bold("pethost deploy"))
		return
	}

	from := ui.Clean(text(github.Repository))
	if branch := text(github.Branch); branch != "" {
		from += ", branch " + ui.Clean(branch)
	}
	if directory := text(github.Directory); directory != "" {
		from += ", directory " + ui.Clean(directory)
	}
	a.UI.Info("%s deploys from %s: %s", id, from, followWords(github))

	if github.DeployedCommit != nil {
		a.UI.Info("  It runs commit %s.", commitWords(github.DeployedCommit, ""))
	}
	if newest := github.NewestCommit; newest != nil && (github.DeployedCommit == nil || newest.Sha != github.DeployedCommit.Sha) {
		a.UI.Info("  The branch's newest is %s.", commitWords(newest, ""))
	}
}

// followWords say how a project follows its branch, by the API's own fields.
func followWords(github pethost.GithubSource) string {
	switch {
	case github.AutoDeploy != nil && !*github.AutoDeploy:
		return "it stays on its commit, and a push does not deploy."
	case github.AutoDeployPending:
		return "every push deploys itself, and the newest commit deploys once the project is free."
	}
	return "every push deploys itself."
}

// printRepositories writes the repositories of the account's installations of the GitHub App,
// or how to install it.
func printRepositories(a *app.App, github *pethost.GithubConnection) {
	if github == nil {
		a.UI.Info("GitHub is not set up on this panel.")
		return
	}
	if len(github.Repositories) == 0 {
		a.UI.Info("Pethost's GitHub App is on no repository of yours yet. Install it:")
		fmt.Fprintln(a.UI.Out, ui.Clean(github.InstallURL))
		return
	}

	out := a.UI.Out
	table := out.Table()
	for _, repository := range github.Repositories {
		private := ""
		if repository.Private {
			private = "private"
		}
		table.Row(out.Bold(ui.Clean(repository.Repository)), ui.Clean(repository.DefaultBranch), private, "pushed "+out.Ago(repository.PushTime))
	}
	table.Flush()

	if int(github.RepositoryCount) > len(github.Repositories) {
		a.UI.Info("The %d most recently pushed, of %d.", len(github.Repositories), github.RepositoryCount)
	}
	if !github.WebhooksEnabled {
		a.UI.Info("A push reaches Pethost by its check every 5 minutes, not at once.")
	}
	a.UI.Info("Deploy a project from one:  %s", a.UI.Err.Bold("pethost github <owner/repo>"))
	a.UI.Info("Choose the repositories:  %s", ui.Clean(github.InstallURL))
}
