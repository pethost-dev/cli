package sandbox

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/pethost-dev/sdk/go/sandboxes"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/ui"
)

// The API's values in words, and the words a flag takes for them. A flag's word is the API's
// value in lower case, with a hyphen for its underscore: "running", "xlarge", "claude-code".

// word is an enum's value as a flag takes it and a person reads it: "RUNNING" is "running".
func word(value fmt.Stringer) string {
	return strings.ReplaceAll(strings.ToLower(value.String()), "_", "-")
}

// fromWord is the value of an enum that a flag's word names, of the values given, which are
// every value a person may name. A word that is none of them is a wrong usage, which lists them.
func fromWord[T fmt.Stringer](flag, typed string, values ...T) (T, error) {
	words := make([]string, len(values))
	for i, value := range values {
		words[i] = word(value)
		if strings.EqualFold(words[i], strings.TrimSpace(typed)) {
			return value, nil
		}
	}
	var none T
	return none, wrongValue("--%s takes %s, and %q was given.", flag, strings.Join(words, ", "), typed)
}

// wrongValue is a flag's value that the CLI cannot write into a request, exit 2.
func wrongValue(format string, args ...any) *fail.Error {
	e := fail.Choice(fmt.Sprintf(format, args...))
	e.Code = "USAGE"
	return e
}

// sizes are the sizes a person may name, smallest first.
var sizes = []sandboxes.Size{sandboxes.SizeSmall, sandboxes.SizeDefault, sandboxes.SizeLarge, sandboxes.SizeXlarge}

// states are the states a person may list sandboxes by.
var states = []sandboxes.SandboxState{
	sandboxes.SandboxStateRunning, sandboxes.SandboxStatePaused, sandboxes.SandboxStateStopped, sandboxes.SandboxStateDeleted,
}

// stateWord is a sandbox's state, coloured on a terminal: running green, paused and stopped
// plain, deleted dim.
func stateWord(out *ui.Stream, state sandboxes.SandboxState) string {
	switch state {
	case sandboxes.SandboxStateRunning:
		return out.Green(word(state))
	case sandboxes.SandboxStateDeleted:
		return out.Dim(word(state))
	}
	return word(state)
}

// labelsWords are a sandbox's labels as a flag takes them: "user=42 team=web".
func labelsWords(labels []sandboxes.Label) string {
	words := make([]string, len(labels))
	for i, label := range labels {
		words[i] = ui.Clean(label.Key) + "=" + ui.Clean(label.Value)
	}
	return strings.Join(words, " ")
}

// labelsOf are the labels a list of "key=value" flags gives. A word without "=" is a key whose
// value is empty.
func labelsOf(typed []string) []sandboxes.Label {
	labels := make([]sandboxes.Label, len(typed))
	for i, pair := range typed {
		key, value, _ := strings.Cut(pair, "=")
		labels[i] = sandboxes.Label{Key: key, Value: value}
	}
	return labels
}

// seconds is a duration flag as the API takes it, a whole number of seconds, rounded up: "5m" is
// 300, "0" is 0, which turns a timer off.
func seconds(flag string, d time.Duration) (uint32, error) {
	if d < 0 || d.Seconds() > math.MaxUint32 {
		return 0, wrongValue("--%s takes a duration such as 90s, 5m or 2h, or 0 for never, and %s was given.", flag, d)
	}
	return uint32(math.Ceil(d.Seconds())), nil
}

// secondsWords is a number of seconds as a person reads a timer: "5 min", "never" for 0.
func secondsWords(value uint32) string {
	if value == 0 {
		return "never"
	}
	return ui.Duration(time.Duration(value) * time.Second)
}

// amountWords is an amount in millionths of a currency's unit: to the cent from 1 up, "12.37",
// and to four decimals below, as an hour's price, "0.018".
func amountWords(micros int64) string {
	if micros >= 1e6 || micros <= -1e6 {
		return fmt.Sprintf("%.2f", float64(micros)/1e6)
	}
	amount := strings.TrimRight(fmt.Sprintf("%.4f", float64(micros)/1e6), "0")
	if decimals := len(amount) - strings.Index(amount, ".") - 1; decimals < 2 {
		amount += strings.Repeat("0", 2-decimals)
	}
	return amount
}

// costWords is an amount with its currency: "$1.50" in dollars, "1.50 EUR" in another.
func costWords(micros int64, currency string) string {
	if strings.EqualFold(currency, "USD") {
		return "$" + amountWords(micros)
	}
	return amountWords(micros) + " " + strings.ToUpper(ui.Clean(currency))
}

// sizeOf is the size a sandbox runs at, from its settings in force.
func sizeOf(sandbox *sandboxes.Sandbox) sandboxes.Size {
	if sandbox.Settings == nil {
		return sandboxes.SizeUnspecified
	}
	return sandbox.Settings.Size
}

// joined are the words of a command line, as bash -lc runs them: one word as it is, which may
// be a whole shell line ("npm ci && npm test"), and several each quoted as a shell takes it, so
// that `-- sh -c 'echo $HOME'` runs as typed.
func joined(words []string) string {
	if len(words) == 1 {
		return words[0]
	}
	quoted := slices.Clone(words)
	for i, w := range quoted {
		quoted[i] = app.ShellWord(w)
	}
	return strings.Join(quoted, " ")
}

// countOf is a number of sandboxes: "1 sandbox", "10 sandboxes".
func countOf(n int) string {
	if n == 1 {
		return "1 sandbox"
	}
	return ui.Count(uint64(n)) + " sandboxes"
}
