package app

import (
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

	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// MachineAndProject asks for the machine and for one project at once.
func (a *App) MachineAndProject(ctx context.Context, projectID string) (*pethost.GetMachineResponse, *pethost.GetProjectResponse, error) {
	client, err := a.Client()
	if err != nil {
		return nil, nil, err
	}

	var machine *pethost.GetMachineResponse
	var project *pethost.GetProjectResponse
	err = Together(
		func() (err error) {
			machine, err = client.GetMachine(ctx, nil)
			return err
		},
		func() (err error) {
			project, err = client.GetProject(ctx, &pethost.GetProjectRequest{ProjectID: projectID})
			return err
		},
	)
	if err != nil {
		return nil, nil, err
	}
	return machine, project, nil
}
