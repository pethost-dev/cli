package fail

import (
	"maps"
	"strings"

	"github.com/pethost-dev/cli/internal/jsonout"
	"github.com/pethost-dev/cli/internal/ui"
)

// Print writes how a command ended to stderr and returns the process's exit code. nil is 0 and
// writes nothing. For people it is the sentence, then the next command; with asJSON it is one
// object: {"code", "message", <the API's detail>, "next"}. An interruption says nothing, unless
// it left something to follow: a program reads that in the object, as a person did on the screen.
func Print(screen *ui.Screen, err error, asJSON bool) int {
	if err == nil {
		return 0
	}

	e := From(err)
	switch {
	case e.Exit == ExitInterrupted && (!asJSON || e.Next == ""):
	case asJSON:
		_ = jsonout.Write(screen.Err, e.object())
	case !e.Shown:
		for _, line := range e.lines(screen.Err) {
			screen.ErrLine(line)
		}
	}
	return e.Exit
}

// object is the error as --json prints it.
func (e *Error) object() map[string]any {
	object := map[string]any{"code": e.Code, "message": e.Message}
	maps.Copy(object, e.Detail)
	if e.Next != "" {
		object["next"] = e.Next
	}
	return object
}

// sameLine is how wide a sentence with its one next command may be to stay on one line.
const sameLine = 100

// lines is the error for people: a failure is marked, a choice or a person's turn is not, and
// the next commands follow the sentence, on its line when there is one and it fits.
func (e *Error) lines(stream *ui.Stream) []string {
	message := e.Message
	if e.Exit == ExitFailed && e.Code != declined {
		message = stream.Cross() + message
	}
	if e.Next == "" {
		return []string{message}
	}

	commands := strings.Split(e.Next, "\n")
	lead := e.lead(len(commands))
	if len(commands) == 1 && ui.Width(message)+len(lead)+len(commands[0]) < sameLine {
		return []string{message + " " + lead + " " + stream.Bold(commands[0])}
	}

	lines := []string{message + " " + lead}
	for _, command := range commands {
		lines = append(lines, "  "+stream.Bold(command))
	}
	return lines
}

func (e *Error) lead(commands int) string {
	switch {
	case e.Lead != "":
		return e.Lead
	case commands > 1:
		return "Run one of:"
	}
	return "Run:"
}
