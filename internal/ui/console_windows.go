package ui

import (
	"io"
	"os"

	"golang.org/x/sys/windows"
)

// rawOutput turns off, on a console, the carriage return Windows adds to each line feed: a
// program elsewhere moves the cursor down with a bare one, and its screen would be drawn askew.
// The function it returns puts the console's mode back.
func rawOutput(stream *Stream) (restore func()) {
	handle := windows.Handle(stream.fd)
	var mode uint32
	if !stream.Terminal || windows.GetConsoleMode(handle, &mode) != nil {
		return func() {}
	}
	_ = windows.SetConsoleMode(handle, mode|windows.DISABLE_NEWLINE_AUTO_RETURN)
	return func() { _ = windows.SetConsoleMode(handle, mode) }
}

// typedCtrlZ says whether a read of stdin that gave nothing and the end of input was Ctrl-Z
// pressed in raw mode: Go's read of a console takes that key for the end of a file, as it is
// when a line is typed, and in raw mode the key belongs to the program the person types to.
func typedCtrlZ(file *os.File, n int, err error) bool {
	if n != 0 || err != io.EOF {
		return false
	}
	var mode uint32
	if windows.GetConsoleMode(windows.Handle(file.Fd()), &mode) != nil {
		return false
	}
	return mode&windows.ENABLE_LINE_INPUT == 0
}
