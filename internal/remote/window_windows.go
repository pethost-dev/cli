package remote

import "time"

// windowLook is how often the console's size is read: Windows sends no signal when it changes.
const windowLook = 250 * time.Millisecond

// watchWindow calls changed whenever the terminal's window may have a new size, until stop is
// closed.
func watchWindow(stop <-chan struct{}, changed func()) {
	ticker := time.NewTicker(windowLook)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			changed()
		}
	}
}
