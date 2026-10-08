package kernel

import (
	"context"
	"errors"
	"time"

	pethost "github.com/pethost-dev/sdk/go"

	"github.com/pethost-dev/cli/internal/browser"
	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/say"
)

// The machine a deploy goes to. An account that has none yet is told where a plan is chosen,
// and on a terminal the deploy waits for the machine and then goes on.

// readMachine reads the machine and its projects. Without a machine it waits for one on a
// terminal; elsewhere the API's refusal ends the command, as a person must act.
func (d *deploy) readMachine(ctx context.Context) error {
	answer, err := d.client.GetMachine(ctx, nil)
	detail := noMachine(err)
	waits := detail != nil && d.a.UI.CanAsk() && !d.a.JSON
	if waits {
		// The sign-in is done whatever the wait brings: it is said before, not after it.
		if d.signedInNow {
			d.a.UI.Done("Signed in.")
		}
		answer, err = d.waitForMachine(ctx, detail)
	}
	if err != nil {
		return err
	}

	d.machine = answer
	switch {
	case answer.Machine == nil:
	case waits:
		d.a.UI.Done("Your machine is ready: %s", say.Machine(answer.Machine))
	case d.signedInNow:
		d.a.UI.Done("Signed in. Your machine: %s", say.Machine(answer.Machine))
	}
	return nil
}

func noMachine(err error) *pethost.NoMachine {
	if api, ok := errors.AsType[*pethost.Error](err); ok {
		return api.NoMachine
	}
	return nil
}

// machineCheck is how often a waiting deploy asks whether the machine is there.
const machineCheck = 5 * time.Second

// waitForMachine says why there is no machine, with the API's link to where a plan is chosen,
// and asks again until there is one. Enter opens the link.
func (d *deploy) waitForMachine(ctx context.Context, detail *pethost.NoMachine) (*pethost.GetMachineResponse, error) {
	screen := d.a.UI
	ready := make(chan struct{})
	defer close(ready)

	if detail.Reason == pethost.NoMachineReasonBeingPrepared {
		screen.Info("Your machine is being prepared.")
	} else {
		screen.Info("%s   %s", fail.NoMachineMessage(detail), screen.Err.Dim("(Enter opens it)"))
		go func() {
			if pressed, _ := screen.Enter(ready); pressed {
				_ = browser.Open(detail.URL)
			}
		}()
	}
	// The line under it is the wait itself, with its time: the two read as one sentence.
	screen.Info("Ctrl-C is safe: an email comes when it is ready, then run pethost deploy.")

	live := screen.Live("Waiting for your machine…")
	defer live.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(machineCheck):
		}

		answer, err := d.client.GetMachine(ctx, nil)
		if noMachine(err) == nil {
			return answer, err
		}
	}
}
