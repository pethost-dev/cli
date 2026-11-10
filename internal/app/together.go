package app

import (
	"cmp"
	"context"
	"sync"

	pethost "github.com/pethost-dev/sdk/go"
)

// Together runs calls that do not depend on each other at once, and returns when all have
// ended: the first error in the order given, or nil. The panel and a machine may be far apart,
// so a screen made of two answers asks for both at the same time.
func Together(calls ...func() error) error {
	errs := make([]error, len(calls))
	var group sync.WaitGroup
	for i, call := range calls {
		group.Go(func() { errs[i] = call() })
	}
	group.Wait()

	return cmp.Or(errs...)
}

// MachineAndProject asks for the machine and for one project at once.
func (a *App) MachineAndProject(ctx context.Context, projectID string) (*pethost.GetMachineResponse, *pethost.GetProjectResponse, error) {
	if err := a.NeedSignIn(); err != nil {
		return nil, nil, err
	}
	client := a.Client()

	var machine *pethost.GetMachineResponse
	var project *pethost.GetProjectResponse
	// The project's refusal comes first: it is the one that says what became of the project
	// (moved, gone) and, for a call that needed a workspace, which ones hold it.
	err := Together(
		func() (err error) {
			project, err = client.GetProject(ctx, &pethost.GetProjectRequest{ProjectID: projectID})
			return err
		},
		func() (err error) {
			machine, err = client.GetMachine(ctx, nil)
			return err
		},
	)
	if err != nil {
		return nil, nil, err
	}
	return machine, project, nil
}
