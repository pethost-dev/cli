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
