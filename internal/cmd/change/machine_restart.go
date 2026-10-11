package change

import (
	"context"
	"errors"
	"time"

	pethost "github.com/pethost-dev/sdk/go"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/fail"
)

// machineRestartCommand is `pethost machine restart`: now, at a time, or in the machine's
// nightly window. Every project is down while it restarts, so it asks first; --cancel takes a
// restart that waits back.
func machineRestartCommand(a *app.App) *cobra.Command {
	var at string
	var cancel bool
	cmd := &cobra.Command{
		Use:   "restart",
		Short: "Restart the machine, now or at a time",
		Long: `Restart the machine: now, or at a time. Every project is down while it restarts,
and containers come back as their restart policies say. It waits for operations
and backups that run to end first.

  --at 03:00               the next time the clock here says so
  --at "2026-10-08 03:00"  that moment; an RFC 3339 time is read too
  --at night               the machine's next nightly window
  --cancel                 no restart after all`,
		Example: `  pethost machine restart
  pethost machine restart --at 03:00
  pethost machine restart --at night
  pethost machine restart --cancel`,
		Args: cobra.NoArgs,
	}
	cmd.Flags().StringVar(&at, "at", "", "when to restart instead of now: a time, or night")
	cmd.Flags().BoolVar(&cancel, "cancel", false, "cancel the restart that is scheduled")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if cancel && at != "" {
			return fail.Usage(cmd.CommandPath()+": --cancel takes no --at", cmd.UseLine())
		}
		if cancel {
			return cancelRestart(cmd.Context(), a)
		}
		when, err := restartMoment(at, time.Now())
		if err != nil {
			return fail.Usage(cmd.CommandPath()+": "+err.Error(), cmd.UseLine())
		}
		return restartMachine(cmd.Context(), a, when)
	}
	return cmd
}

// A restartWhen is when a restart is asked for: now, at a moment, or in the nightly window.
type restartWhen struct {
	moment time.Time
	window bool
}

// atLayouts are the ways --at writes a moment of the person's own time zone.
var atLayouts = []string{"2006-01-02 15:04", "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02T15:04:05"}

// restartMoment reads --at. A clock time alone is the next moment the clock here shows it:
// today, or tomorrow when today's has passed.
func restartMoment(at string, now time.Time) (restartWhen, error) {
	switch at {
	case "":
		return restartWhen{}, nil
	case "night":
		return restartWhen{window: true}, nil
	}

	if moment, err := time.Parse(time.RFC3339, at); err == nil {
		return restartWhen{moment: moment}, nil
	}
	for _, layout := range atLayouts {
		if moment, err := time.ParseInLocation(layout, at, now.Location()); err == nil {
			return restartWhen{moment: moment}, nil
		}
	}
	if clock, err := time.ParseInLocation("15:04", at, now.Location()); err == nil {
		moment := time.Date(now.Year(), now.Month(), now.Day(), clock.Hour(), clock.Minute(), 0, 0, now.Location())
		if !moment.After(now) {
			moment = moment.AddDate(0, 0, 1)
		}
		return restartWhen{moment: moment}, nil
	}
	return restartWhen{}, errors.New(`--at takes a time (03:00, "2026-10-08 03:00", or RFC 3339) or the word night`)
}

func restartMachine(ctx context.Context, a *app.App, when restartWhen) error {
	a.SayToken()
	client, err := a.Client()
	if err != nil {
		return err
	}

	said := "now"
	switch {
	case when.window:
		said = "in its next nightly window"
	case !when.moment.IsZero():
		said = a.UI.Err.When(when.moment)
	}
	question := "Restart your machine " + said + "? Every project is down while it restarts, and containers come back as their restart policies say."
	if err := askFirst(a, question, "The machine was not restarted."); err != nil {
		return err
	}

	answer, err := client.RunMachineAction(ctx, &pethost.RunMachineActionRequest{
		Action: pethost.RunMachineActionRestartMachine{RestartTime: when.moment, AtMaintenanceWindow: when.window},
	})
	if err != nil {
		return err
	}
	if a.JSON {
		return a.PrintJSON(answer)
	}

	// The machine says when it will be: later than asked while an operation or a backup runs.
	if answer.Machine == nil || answer.Machine.ScheduledRestartTime.IsZero() {
		a.UI.Done("Your machine restarts: it does not answer until it is back.")
		return nil
	}
	a.UI.Done("Your machine restarts %s, once what runs then has ended.", a.UI.Err.When(answer.Machine.ScheduledRestartTime))
	a.UI.Info("  No restart after all:  %s", a.UI.Err.Bold("pethost machine restart --cancel"))
	return nil
}

func cancelRestart(ctx context.Context, a *app.App) error {
	a.SayToken()
	client, err := a.Client()
	if err != nil {
		return err
	}
	answer, err := client.RunMachineAction(ctx, &pethost.RunMachineActionRequest{Action: pethost.RunMachineActionCancelRestart(true)})
	if err != nil {
		return err
	}
	if a.JSON {
		return a.PrintJSON(answer)
	}
	a.UI.Done("No restart of your machine is scheduled.")
	return nil
}
