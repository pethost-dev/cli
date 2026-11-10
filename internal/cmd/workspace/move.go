package workspace

import (
	"context"
	"crypto/rand"
	"errors"
	"strings"

	pethost "github.com/pethost-dev/sdk/go"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/operation"
	"github.com/pethost-dev/cli/internal/target"
	"github.com/pethost-dev/cli/internal/ui"
)

// moveCommand is `pethost move <workspace>`: the folder's project goes to another workspace of
// the person's, as one operation of the panel's (MoveProject), watched to its end. Each database
// it is linked to stays; what it uses there instead is the panel's choice unless a flag names
// one (switches.go).
func moveCommand(a *app.App) *cobra.Command {
	m := &move{a: a}
	cmd := &cobra.Command{
		Use:   "move <workspace>",
		Short: "Move this folder's project to another workspace",
		Long: `Move a project to another of your workspaces, named by its id or name (pethost
workspaces lists them): this folder's project, or the one -p names. It stops, is backed
up into the other workspace's backups, and starts there under the same id; then it is
deleted here. Until it starts there, a failure puts it back as it ran here.

Each database it uses stays here, for its other apps. There it uses a database of the
same engine instead: the one of the same id, or a new one, unless --into names another,
with a copy of its data, unless --empty. Where you wrote its connection lines in .env
yourself, --prefix names them (DATABASE for DATABASE_URL), and they are written anew.
A database that apps use does not move: move each app.

Its addresses move to the other workspace's domain, and a domain of your own must point
at the other machine's name, which the end says. Its operation history, logs and earlier
snapshots stay behind. The folder's .pethost names the new workspace afterwards.`,
		Example: `  pethost move Work
  pethost move k7f3q2xy -p recipes --yes
  pethost move Work --into shop-db=work-db --empty cache
  pethost move Work --prefix shop-db=DATABASE`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return m.run(cmd.Context(), args[0])
		},
	}
	m.choices.flags(cmd)
	m.noQueue = operation.NoQueueFlag(cmd)
	return cmd
}

// A move is one run of the command.
type move struct {
	a       *app.App
	noQueue *bool
	choices choices

	project target.Project
	// The destination, as typed: an id or a name, which the API resolves.
	to string
	// The move's operation id, the same in every call: a call again with it answers the move
	// that started, and starts nothing.
	operationID string
	// What it uses there instead of each database the flags name; the panel chooses for the
	// others.
	switches []pethost.DatabaseSwitch
}

var words = operation.Words{Doing: "Moving…", Done: "Moved", Failed: "Move failed"}

// run is the move in its steps: the project, the question, the call, its operation watched to
// its end, and the project where it landed.
func (m *move) run(ctx context.Context, to string) error {
	switches, err := m.choices.switches()
	if err != nil {
		return err
	}
	project, err := target.ForChange(ctx, m.a)
	if err != nil {
		return err
	}
	m.project, m.to, m.operationID, m.switches = project, to, newOperationID(), switches

	if err := m.ask(ctx); err != nil {
		return err
	}
	answer, err := m.start(ctx)
	if err != nil {
		return m.refused(err)
	}

	done, err := m.watch(ctx, answer)
	if err != nil {
		return err
	}
	return m.land(ctx, done)
}

// ask asks before the project stops and its addresses change, saying what it uses there instead
// of each database it is linked to.
func (m *move) ask(ctx context.Context) error {
	answer, err := m.a.Client().GetProject(ctx, &pethost.GetProjectRequest{ProjectID: m.project.ID})
	if err != nil {
		return err
	}

	if answer.Project != nil {
		for _, dependency := range answer.Project.Dependencies {
			m.a.UI.Info("%s", sayDependency(dependency, m.switches, m.to))
		}
	}
	question := "Move " + ui.Clean(m.project.ID) + " to " + ui.Clean(m.to) +
		"? It stops while it moves, and its addresses move to that workspace's domain."
	yes, err := m.a.UI.Confirm(question, m.a.CommandLine("--yes"))
	if err != nil {
		return err
	}
	if !yes {
		return fail.Declined("Nothing was moved.")
	}
	return nil
}

// start calls MoveProject, answered as soon as the move started; a project that is busy waits
// its turn, unless --no-queue.
func (m *move) start(ctx context.Context) (*pethost.MoveProjectResponse, error) {
	return operation.Queue(ctx, m.a, m.project, *m.noQueue, func() (*pethost.MoveProjectResponse, error) {
		return m.call(ctx, new(uint32(0)))
	})
}

func (m *move) call(ctx context.Context, wait *uint32) (*pethost.MoveProjectResponse, error) {
	return m.a.Client().MoveProject(ctx, &pethost.MoveProjectRequest{
		ProjectID: m.project.ID, ToWorkspace: m.to, SwitchDatabases: m.switches, OperationID: m.operationID, WaitSeconds: wait,
	})
}

// refused is the end of a move the API did not start. A destination that is none of the
// person's workspaces is answered with the commands that move it to each of those it listed.
func (m *move) refused(err error) error {
	api, ok := errors.AsType[*pethost.Error](err)
	if !ok || api.WorkspaceChoice == nil || api.Code != pethost.CodeInvalidArgument {
		return err
	}

	e := fail.From(api)
	var commands []string
	for _, workspace := range api.WorkspaceChoice.Workspaces {
		commands = append(commands, "pethost move "+app.ShellWord(workspace.WorkspaceID)+m.project.Flag())
	}
	e.Lead, e.Next = "Move it to one of these:", strings.Join(commands, "\n")
	return e
}

// watch takes the move to its end, as the operation's log shows it. One that did not succeed
// is reported in the machine's words.
func (m *move) watch(ctx context.Context, answer *pethost.MoveProjectResponse) (*operation.Result, error) {
	done := &operation.Result{Operation: answer.Operation, Answer: answer}
	if answer.Operation == nil || answer.Operation.FinishTime.IsZero() {
		watched, err := operation.Watch(ctx, m.a, m.project, answer.Operation, words.Doing)
		if err != nil {
			return nil, err
		}
		done = watched
	}

	if done.Operation.Status != pethost.OperationStatusSucceeded {
		return nil, m.a.WithAnswer(done.Answer, done.Report(ctx, m.a, m.project, words))
	}
	return done, nil
}

// newOperationID is a move's own operation id, as the API takes one: letters and digits.
func newOperationID() string {
	return "move-" + strings.ToLower(rand.Text())
}
