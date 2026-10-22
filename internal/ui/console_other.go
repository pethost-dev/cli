//go:build !windows

package ui

import "os"

// What Windows' console needs that no other terminal does.

// enableEscapes is for Windows, whose old console needs it; every other terminal reads escape
// sequences as it is.
func enableEscapes(*os.File) bool { return true }

// rawOutput is for Windows, whose console adds a carriage return to each line feed; every other
// terminal's raw mode covers what is written too.
func rawOutput(*Stream) (restore func()) { return func() {} }

// typedCtrlZ is for Windows, where a read of the console ends at Ctrl-Z; elsewhere the key is a
// byte like any other.
func typedCtrlZ(*os.File, int, error) bool { return false }
