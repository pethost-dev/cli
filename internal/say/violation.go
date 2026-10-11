package say

import (
	pethost "github.com/pethost-dev/sdk/go"

	"github.com/pethost-dev/cli/internal/ui"
)

// Violation is one reason the machine cannot deploy a project's files, as one line: where it
// is, then what is wrong and what to write instead, in the machine's words.
func Violation(violation pethost.SpecViolation) string {
	message := ui.Clean(violation.ViolationMessage)
	if violation.Location == "" {
		return message
	}
	return ui.Clean(violation.Location) + ": " + message
}

// Refusal prints why the machine cannot deploy a project's files: each violation where it is and
// as the machine says it, after what the machine had changed on its way there.
func Refusal(screen *ui.Screen, adjustments []string, violations []pethost.SpecViolation) {
	for _, adjustment := range adjustments {
		screen.Note("%s", ui.Clean(adjustment))
	}
	for _, violation := range violations {
		screen.ErrLine(screen.Err.Cross() + Violation(violation))
	}
}
