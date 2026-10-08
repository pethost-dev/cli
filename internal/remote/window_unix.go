//go:build !windows

package remote

import (
	"os"
	"os/signal"
	"syscall"
)

// watchWindow calls changed whenever the terminal's window may have a new size, until stop is
// closed: at each SIGWINCH, and once at the start, for a resize made before the signal was
// asked for.
func watchWindow(stop <-chan struct{}, changed func()) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGWINCH)
	defer signal.Stop(signals)

	changed()
	for {
		select {
		case <-stop:
			return
		case <-signals:
			changed()
		}
	}
}
