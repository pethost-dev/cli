package operation

import (
	"context"
	"errors"
	"time"

	pethost "github.com/pethost-dev/sdk/go"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/target"
	"github.com/pethost-dev/cli/internal/ui"
)

// NoQueueFlag adds --no-queue to a command that starts an operation, and returns where its
// value is read from.
func NoQueueFlag(cmd *cobra.Command) *bool {
	return cmd.Flags().Bool("no-queue", false, "fail when the project is busy instead of waiting for its turn")
}

// callAgainPause is how long to wait before calling again when a short action holds the
// project, or when the operation that holds it is still starting.
const callAgainPause = 2 * time.Second

// Queue makes a call that changes a project, and when the project is busy with something else
// says so, waits for that to end and calls again; with noQueue it fails instead, with the exit
// code of "try later".
func Queue[T any](ctx context.Context, a *app.App, project target.Project, noQueue bool, call func() (T, error)) (T, error) {
	for {
		value, err := call()
		busy := busyOf(err)
		if busy == nil {
			return value, err
		}

		said := fail.BusyMessage(ui.Clean(project.ID), busy)
		if noQueue {
			e := fail.From(err)
			e.Message = said
			return value, e
		}
		if err := waitFor(ctx, a, project, busy, said); err != nil {
			return value, err
		}
	}
}

func busyOf(err error) *pethost.ProjectBusy {
	if api, ok := errors.AsType[*pethost.Error](err); ok {
		return api.ProjectBusy
	}
	return nil
}

// waitFor says what holds the project and waits until it no longer does.
func waitFor(ctx context.Context, a *app.App, project target.Project, busy *pethost.ProjectBusy, said string) error {
	a.UI.Info("%s", said)
	live := a.UI.Live("Waiting for its turn…")
	defer live.Stop()

	client, err := a.Client()
	if err != nil {
		return err
	}
	if busy.OperationID == "" {
		return pause(ctx)
	}

	for {
		// The call itself waits, up to the API's 45 seconds, for the operation to end.
		response, err := client.GetOperation(ctx, &pethost.GetOperationRequest{ProjectID: project.ID, OperationID: busy.OperationID})
		switch {
		case busyOf(err) != nil: // Still starting.
			if err := pause(ctx); err != nil {
				return err
			}
		case err != nil:
			return err
		case response.Operation == nil || response.Operation.Status != pethost.OperationStatusInProgress:
			return nil
		}
	}
}

func pause(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(callAgainPause):
		return nil
	}
}
