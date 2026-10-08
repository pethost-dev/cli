//go:build !windows

package ui

import "os"

// rawOutput is for Windows, whose console adds a carriage return to each line feed; every other
// terminal's raw mode covers what is written too.
func rawOutput(*Stream) (restore func()) { return func() {} }

// typedCtrlZ is for Windows, where a read of the console ends at Ctrl-Z; elsewhere the key is a
// byte like any other.
func typedCtrlZ(*os.File, int, error) bool { return false }
