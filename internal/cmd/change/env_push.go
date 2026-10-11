package change

import (
	"cmp"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	pethost "github.com/pethost-dev/sdk/go"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/operation"
	"github.com/pethost-dev/cli/internal/target"
	"github.com/pethost-dev/cli/internal/ui"
)

// envPushCommand is `pethost env push [file]`: the project's /.env becomes a copy of this
// folder's .env, or of the file named. What the project's file has that the new one lacks is
// gone for good, so it is named and asked about first.
func envPushCommand(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "push [file]",
		Short: "Replace the project's /.env with this folder's .env, or with a file",
		Long: `Replace the project's /.env with this folder's .env, or with the file named, and
deploy it. It names the variables that would disappear and asks first: a deploy
never sends .env by itself once the project exists, and a rollback does not bring
an earlier /.env back.`,
		Example: `  pethost env push
  pethost env push .env.production
  pethost env push --yes -p recipes     in a script: nothing is asked`,
		Args: cobra.MaximumNArgs(1),
	}
	noQueue := operation.NoQueueFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		project, err := target.ForChange(cmd.Context(), a)
		if err != nil {
			return err
		}
		p := &envPush{a: a, project: project, noQueue: *noQueue, path: pushedFile(project, args)}
		return p.run(cmd.Context())
	}
	return cmd
}

// pushedFile is the file a push sends: the one named, else the .env beside the .pethost that
// names the project, else this folder's.
func pushedFile(project target.Project, args []string) string {
	switch {
	case len(args) == 1:
		return args[0]
	case project.Source == target.FromFile:
		return filepath.Join(project.File.Dir, ".env")
	}
	return ".env"
}

// An envPush is one run of the command.
type envPush struct {
	a       *app.App
	project target.Project
	noQueue bool
	// The file sent, as the person named it, and what it holds.
	path  string
	local []byte
	// The project's /.env the person said yes to replacing: a later read that differs is asked
	// about again.
	agreed *string
}

func (p *envPush) run(ctx context.Context) error {
	client, err := p.a.Client()
	if err != nil {
		return err
	}
	p.local, err = os.ReadFile(p.path)
	if err != nil {
		return p.unreadable(err)
	}

	var done *outcome
	err = againIfChanged(func() error {
		env, err := readEnv(ctx, client, p.project.ID)
		if err != nil {
			return err
		}
		if env.exists && env.text == string(p.local) {
			return nil
		}
		if err := p.confirm(env); err != nil {
			return err
		}

		done, err = deploy(ctx, p.a, p.project, p.noQueue, &pethost.DeployProjectRequest{
			BaseDeployID: env.deployID,
			Files:        []pethost.FileChange{{Path: envPath, Change: pethost.FileChangeData(p.local)}},
		}, envWords)
		return err
	})
	if err != nil {
		sayEnvStays(p.a, done)
		return err
	}
	if done == nil {
		p.a.UI.Info("The %s of %s is %s already: nothing changed.", envPath, ui.Clean(p.project.ID), p.name())
		return nil
	}

	p.a.UI.Done("%s is a copy of %s now. Deployed in %s.", envPath, p.name(), done.took())
	if line := nobodyReadsLine(done.Project); line != "" && len(variableNames(string(p.local))) > 0 {
		p.a.UI.Info("  %s", line)
	}
	return done.said(p.a)
}

// name is the file as a sentence names it: as typed, and the .env found beside a .pethost as
// seen from here, ".env" or "../.env".
func (p *envPush) name() string {
	name := p.path
	if here, err := os.Getwd(); err == nil && filepath.IsAbs(name) {
		if relative, err := filepath.Rel(here, name); err == nil {
			name = relative
		}
	}
	return ui.Clean(filepath.ToSlash(name))
}

func (p *envPush) unreadable(err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return fail.Failed("There is no %s to send.", p.name()).WithLead("Name the file:", "pethost env push <file>"+p.project.Flag())
	}
	return fail.Failed("%s cannot be read: %s", p.name(), errors.Unwrap(err))
}

// confirm asks before the project's /.env is replaced, naming what would disappear; once for
// each text of it the person has not said yes to.
func (p *envPush) confirm(env *projectEnv) error {
	if p.agreed != nil && *p.agreed == env.text {
		return nil
	}

	if err := askFirst(p.a, p.question(env), "Nothing changed."); err != nil {
		return err
	}
	p.agreed = &env.text
	return nil
}

// question says what replacing the project's /.env does to its variables, then asks.
func (p *envPush) question(env *projectEnv) string {
	id, local := ui.Clean(p.project.ID), string(p.local)
	names := variableNames(local)
	if !env.exists {
		return id + " has no " + envPath + " yet. Send " + p.name() + ", which sets " + cmp.Or(listed(names), "nothing") + "?"
	}

	var gone, changed, added []string
	for _, name := range variableNames(env.text) {
		if !slices.Contains(names, name) {
			gone = append(gone, name)
		}
	}
	for _, name := range names {
		was, had := statementOf(env.text, name)
		is, _ := statementOf(local, name)
		switch {
		case !had:
			added = append(added, name)
		case was != is:
			changed = append(changed, name)
		}
	}

	says := "Nothing in the " + envPath + " of " + id + " would be gone."
	if len(gone) > 0 {
		says = "The " + envPath + " of " + id + " has " + listed(gone) + ", which " + p.name() + " lacks: " + itOrThey(gone) + " would be gone for good."
	}
	switch {
	case len(changed) > 0 && len(added) > 0:
		says += "\n" + p.name() + " writes " + listed(changed) + " differently and adds " + listed(added) + "."
	case len(changed) > 0:
		says += "\n" + p.name() + " writes " + listed(changed) + " differently."
	case len(added) > 0:
		says += "\n" + p.name() + " adds " + listed(added) + "."
	}
	return says + "\nReplace it with " + p.name() + "?"
}

func itOrThey(names []string) string {
	if len(names) == 1 {
		return "it"
	}
	return "they"
}
