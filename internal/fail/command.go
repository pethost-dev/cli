package fail

import (
	"fmt"

	"github.com/pethost-dev/cli/internal/ui"
)

// A command that runs a program elsewhere (pethost run, pethost sandbox run, a shell) exits as
// the program did, so its own ends need codes no program is likely to use: the ones `timeout`
// and `docker run` gave these meanings.
const (
	ExitTimedOut = 124 // The program was stopped at its timeout.
	ExitOwn      = 125 // The CLI's own failure: the program did not run, or its end is not known.
)

// commandEnded is the code of an end that is the program's own, not the CLI's failure.
const commandEnded = "COMMAND_ENDED"

// Ended is the end of a command whose program ended with exit, which it has shown: the process
// exits with it.
func Ended(exit int, format string, args ...any) *Error {
	return &Error{Exit: ExitCodeOf(exit), Code: commandEnded, Shown: true, Message: fmt.Sprintf(format, args...)}
}

// TimedOut is the end of a command whose program was stopped at its timeout, exit 124.
func TimedOut(message string) *Error {
	return &Error{Exit: ExitTimedOut, Code: commandEnded, Shown: true, Message: message}
}

// Own is an end of a command that runs a program which is not the program's own: it exits 125,
// whatever kind of failure it is, so that a script tells it from every code the program may exit
// with. Ctrl-C stays 130. How a failure is worded for people goes by its exit code, so it is
// printed as the kind it is before the code becomes 125: with asJSON, Print writes it later.
func Own(screen *ui.Screen, asJSON bool, err error) error {
	if err == nil {
		return nil
	}
	e := From(err)
	if e.Exit == ExitOwn || e.Exit == ExitInterrupted || e.Code == commandEnded {
		return e
	}
	if !asJSON {
		Print(screen, e, false)
		e.Shown = true
	}
	e.Exit = ExitOwn
	return e
}

// ExitCodeOf is a program's exit code as this process can exit with it: one byte, and never 0
// for a program that did not exit with 0.
func ExitCodeOf(code int) int {
	if code < 0 || code > 255 {
		return 255
	}
	return code
}
