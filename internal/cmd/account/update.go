package account

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/release"
)

// updateCommand is `pethost update`: the newest release takes this program's place.
func updateCommand(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "Replace this program with the newest release",
		Long: `Replace this program with the newest release: its binary is downloaded, checked
against the release's checksums and put in this one's place. A program that came by
go install is told the go install line to run instead.

pethost says by itself when a newer release is out: once a day at most, on a terminal
only, after a command's own output. PETHOST_NO_UPDATE_CHECK=1 turns that off.`,
		Example: `  pethost update`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return update(cmd.Context(), a)
		},
	}
}

func update(ctx context.Context, a *app.App) error {
	newest, err := release.Newest(ctx)
	if err != nil {
		return updateFailure(ctx, err)
	}
	// The person has just looked: the daily look does not ask them again today.
	_ = release.Look{Time: time.Now(), Settled: release.LastLook().Settled}.Remember()

	switch {
	case newest == a.Build.Version:
		a.UI.Info("pethost %s is the newest release.", newest)
		return nil
	case release.Newer(a.Build.Version, newest):
		// Never back: the releases may name an older one for a moment, while a new one is published.
		a.UI.Info("This is pethost %s, newer than the newest release, %s: nothing to update.", a.Build.Version, newest)
		return nil
	case a.Build.GoInstall:
		// The command to run is this command's answer: it goes to stdout, alone.
		a.UI.Info("pethost %s is out (this is %s). This one came by go install, and updates the same way:", newest, a.Build.Version)
		_, err := fmt.Fprintln(a.UI.Out, release.GoInstall(newest))
		return err
	}
	return install(ctx, a, newest)
}

// install replaces this program with a release and says so.
func install(ctx context.Context, a *app.App, version string) error {
	live := a.UI.Live("Downloading pethost " + version + "…")
	path, err := release.Install(ctx, version)
	live.Stop()

	if _, blocked := errors.AsType[*release.NotReplaced](err); blocked {
		return fail.Failed("pethost is in %s, which you cannot write to (%s): run pethost update as the user who put it there.",
			filepath.Dir(path), fail.Innermost(err))
	}
	if err != nil {
		return updateFailure(ctx, err)
	}
	a.UI.Done("pethost is %s now (it was %s; checksum verified).", version, a.Build.Version)
	return nil
}

// updateFailure is an update that did not happen: this program is as it was.
func updateFailure(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, release.ErrChecksum) {
		return fail.Failed("The download is not what the release published: its checksum differs. Nothing was replaced.").
			WithLead("To try again, run:", "pethost update")
	}
	return &fail.Error{Exit: fail.ExitLater, Code: "UNAVAILABLE",
		Message: fmt.Sprintf("The release could not be fetched: %s. Nothing was replaced.", fail.Innermost(err))}
}

const (
	// lookEvery is how often a newer release is looked for.
	lookEvery = 24 * time.Hour
	// lookWait bounds the look: it comes after a command's output, and must not keep the
	// person from their prompt.
	lookWait = 2 * time.Second
)

// CheckForUpdate looks for a newer release after a command's own output, and offers it: at most
// once a day, on a terminal only, never in CI, with --json or -q, or under
// PETHOST_NO_UPDATE_CHECK. The root calls it after every command that ended well. A look that
// fails says nothing.
func CheckForUpdate(ctx context.Context, a *app.App) {
	if !looksForUpdates(a) {
		return
	}
	look := release.LastLook()
	if time.Since(look.Time) < lookEvery {
		return
	}
	// Remembered before the look itself: one that fails is not tried again at every command,
	// and where nothing can be remembered nothing is looked for.
	look.Time = time.Now()
	if look.Remember() != nil {
		return
	}
	release.Tidy()

	bounded, cancel := context.WithTimeout(ctx, lookWait)
	newest, err := release.Newest(bounded)
	cancel()
	if err != nil || !release.Newer(newest, a.Build.Version) || newest == look.Settled {
		return
	}
	if offerUpdate(ctx, a, newest) {
		look.Settled = newest
		_ = look.Remember()
	}
}

// looksForUpdates says whether this run may look: a release, in front of a person, who did not
// ask for the answer alone.
func looksForUpdates(a *app.App) bool {
	return a.Build.Version != "dev" && !a.UI.Quiet && a.UI.CanAsk() && a.UI.Out.Terminal &&
		os.Getenv("CI") == "" && os.Getenv("PETHOST_NO_UPDATE_CHECK") == ""
}

// offerUpdate says that a newer release is out, and returns whether that is settled: the
// person said no to it, or was told the go install line, which is all this program can do for
// one that came that way. A yes updates in place; a question left with Ctrl-C comes again.
func offerUpdate(ctx context.Context, a *app.App, newest string) (settled bool) {
	said := fmt.Sprintf("pethost %s is out (this is %s).", newest, a.Build.Version)
	a.UI.Info("")
	if a.Build.GoInstall {
		a.UI.Info("%s Update:   %s", said, a.UI.Err.Bold(release.GoInstall(newest)))
		return true
	}

	yes, err := a.UI.Offer(said + " Update now?")
	if err != nil {
		return false
	}
	if !yes {
		return true
	}
	fail.Print(a.UI, install(ctx, a, newest), false)
	return false
}
