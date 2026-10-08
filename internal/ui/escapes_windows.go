package ui

import (
	"os"

	"golang.org/x/sys/windows"
)

// enableEscapes turns on a console's own reading of escape sequences, which an old Windows
// console leaves off; false = it has none, so the stream is written as if off a terminal.
func enableEscapes(file *os.File) bool {
	handle := windows.Handle(file.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		return false
	}
	return windows.SetConsoleMode(handle, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING) == nil
}
