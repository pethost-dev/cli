// Package fail is how a command ends when it does not end well: one sentence, the machine's own
// where the machine said it, then the next command, and the exit code that tells a script what
// kind of end it was. A command returns an error; Print turns any error into that.
package fail

import (
	"fmt"
	"strings"
)

// The exit codes.
const (
	ExitFailed      = 1   // It failed, or the machine refused it.
	ExitChoice      = 2   // A choice is needed: an ambiguous name, a missing --yes, a wrong flag.
	ExitPerson      = 3   // A person must act: sign in, choose a plan.
	ExitLater       = 4   // Try later: the project is busy, the machine does not answer.
	ExitInterrupted = 130 // Ctrl-C.
)

// An Error is the end of a command that did not end well.
type Error struct {
	// The process's exit code.
	Exit int
	// What kind of failure it is, for --json: the API's code as its documentation writes it
	// ("NOT_FOUND"), or the CLI's own word in the same form ("NOT_SIGNED_IN").
	Code string
	// What failed, for people: the machine's sentence where the machine said it.
	Message string
	// The next thing to type: one command, or one a line.
	Next string
	// The words between Message and Next. Empty = "Run:", or "Run one of:" before several.
	Lead string
	// The API's typed detail under its name in the API, as --json prints it beside the code:
	// "project_busy", "no_machine". nil = none.
	Detail map[string]any
	// The command has already written Message for people: only --json prints it again.
	Shown bool

	cause error
}

func (e *Error) Error() string { return e.Message }

func (e *Error) Unwrap() error { return e.cause }

// Failed is a failure of the CLI's own, exit 1.
func Failed(format string, args ...any) *Error {
	return &Error{Exit: ExitFailed, Code: "FAILED", Message: fmt.Sprintf(format, args...)}
}

// Declined is the end of a command whose person answered no to its question, exit 1: it is said
// plainly, since nothing went wrong.
func Declined(message string) *Error {
	return &Error{Exit: ExitFailed, Code: declined, Message: message}
}

const declined = "DECLINED"

// Choice says a choice is needed, exit 2, and names the commands that make it, one for each
// answer.
func Choice(message string, commands ...string) *Error {
	return &Error{Exit: ExitChoice, Code: "CHOICE_NEEDED", Message: message, Next: strings.Join(commands, "\n")}
}

// Usage says the command line is wrong, exit 2: an unknown flag, a missing argument. usage is
// the command's usage line.
func Usage(message, usage string) *Error {
	return &Error{Exit: ExitChoice, Code: "USAGE", Message: message + "\nUsage: " + usage}
}

// NotSignedIn is the end of a command that has no token to call with, exit 3.
func NotSignedIn() *Error {
	return &Error{Exit: ExitPerson, Code: "NOT_SIGNED_IN", Message: "Not signed in.", Next: "pethost login"}
}

// Interrupted is the end by Ctrl-C, exit 130: nothing is printed.
func Interrupted() *Error {
	return &Error{Exit: ExitInterrupted, Code: "INTERRUPTED", Shown: true}
}

// WithNext is the error with the next thing to type.
func (e *Error) WithNext(next string) *Error {
	e.Next = next
	return e
}

// WithLead is the error with the next thing to type and the words that lead to it, in place of
// "Run:".
func (e *Error) WithLead(lead, next string) *Error {
	e.Lead, e.Next = lead, next
	return e
}

// AlreadyShown is the error as one the command has written for people itself.
func (e *Error) AlreadyShown() *Error {
	e.Shown = true
	return e
}
