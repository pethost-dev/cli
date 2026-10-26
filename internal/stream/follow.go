// Package stream follows a stream of the API until the person leaves, or until what it streams
// has ended. The SDK never calls again by itself, so a stream that breaks, as every one does when
// the panel restarts for its own deploy or the machine for an update, is opened again from where
// the last one stopped. The pauses between the calls grow, and when followTries calls in a row
// brought nothing, the last failure is the command's.
package stream

import (
	"context"
	"errors"
	"iter"
	"time"

	pethost "github.com/pethost-dev/sdk/go"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/fail"
)

// followTries is how many calls in a row may bring nothing: with their pauses, about two
// minutes, which a machine's restart fits in.
const followTries = 8

// longestPause is the most a follow waits before it calls again.
const longestPause = 30 * time.Second

// stayedOpen is how long a call lasts that was a stream and not a failure to open one: a quiet
// service's stream that broke after it is no step towards giving up.
const stayedOpen = time.Minute

// Follow ranges over a stream that is opened again each time it ends. open makes the next call:
// it reads where to go on from, which each keeps as it takes the responses. It returns nil when
// the person left with Ctrl-C, each's error when each gave one, and the stream's last failure
// when calling again did not help.
func Follow[R any](ctx context.Context, a *app.App, open func() iter.Seq2[R, error], each func(R) error) error {
	said := false
	for tries := 0; ; tries++ {
		started := time.Now()
		brought, err := drain(open(), each)
		switch {
		case ctx.Err() != nil:
			return nil
		case err != nil && !fail.CallAgain(err):
			return err
		}

		if brought || time.Since(started) >= stayedOpen {
			tries, said = 0, false
		}
		if tries == followTries {
			return orKeptEnding(err)
		}
		// The first break is opened again without a word: most are over by then.
		if !said && tries > 0 {
			said = sayBroken(a, err)
		}
		if !sleep(ctx, pauseBefore(tries)) {
			return nil
		}
	}
}

// drain takes a stream's responses to its end, and says whether it brought any.
func drain[R any](stream iter.Seq2[R, error], each func(R) error) (brought bool, err error) {
	for response, err := range stream {
		if err != nil {
			return brought, err
		}
		if err := each(response); err != nil {
			return brought, err
		}
		brought = true
	}
	return brought, nil
}

// sayBroken says, once for a row of failures, that the machine or Pethost does not answer and
// that the follow goes on. It returns whether it said anything: a stream that only ended, or
// failed otherwise, is opened again without a word.
func sayBroken(a *app.App, err error) bool {
	api, ok := errors.AsType[*pethost.Error](err)
	switch {
	case !ok:
		return false
	case api.MachineUnreachable != nil:
		a.UI.Info("Your machine does not answer now, as while it restarts. Still following: what is new shows when it is back.")
	case api.Unwrap() != nil:
		a.UI.Info("Pethost cannot be reached now. Still following: what is new shows when it is back.")
	default:
		return false
	}
	return true
}

// orKeptEnding is the failure of a follow that gave up: the stream's own, or for streams that
// ended without one, that they did.
func orKeptEnding(err error) error {
	if err != nil {
		return err
	}
	return &fail.Error{Exit: fail.ExitLater, Code: pethost.CodeUnavailable.String(), Message: "The stream ended each time it was opened. Try again in a minute."}
}

// pauseBefore is the wait before the call after tries failed ones: 1 s, 2 s, 4 s, up to
// longestPause.
func pauseBefore(tries int) time.Duration {
	return min(time.Second<<tries, longestPause)
}

// sleep waits, and says whether it waited to the end: false = the person left.
func sleep(ctx context.Context, length time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(length):
		return true
	}
}
