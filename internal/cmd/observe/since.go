package observe

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/ui"
)

// --since says how far back a command looks: a length of time ("30m", "1h", "2d"), or a moment
// as the CLI itself writes one off a terminal ("2026-10-07T14:00:00Z"). The machine judges the
// range; here its words only become the API's two fields.

// sinceHelp is the flag's line in a command's help.
const sinceHelp = "how far back to look: 30m, 1h, 2d, or a moment as 2026-10-07T14:00:00Z"

// A since is where a range starts: one of the two is set, or neither when the flag was not
// given.
type since struct {
	// The range's length, ending now: the API's last_seconds.
	lastSeconds uint32
	// Or the moment it starts at: the API's start_time.
	start time.Time
}

// sinceOf reads the flag's value for cmd; a value that is neither form is a wrong usage.
func sinceOf(cmd *cobra.Command, value string) (since, error) {
	if value == "" {
		return since{}, nil
	}
	if moment, err := time.Parse(time.RFC3339, value); err == nil {
		return since{start: moment}, nil
	}

	length, ok := lengthOf(value)
	if !ok {
		message := fmt.Sprintf("%s: --since %q is not a length of time such as 30m, 1h or 2d, nor a moment such as 2026-10-07T14:00:00Z", cmd.CommandPath(), value)
		return since{}, fail.Usage(message, cmd.UseLine())
	}
	return since{lastSeconds: uint32(min(length/time.Second, 1<<32-1))}, nil
}

// lengthOf reads a length of time as Go writes one ("90s", "1h30m"), or in days ("2d"): a
// second at least, the API's unit.
func lengthOf(value string) (time.Duration, bool) {
	var length time.Duration
	var err error
	if days, isDays := strings.CutSuffix(value, "d"); isDays {
		length, err = inDays(days)
	} else {
		length, err = time.ParseDuration(value)
	}
	return length, err == nil && length >= time.Second
}

// inDays reads a count of days ("2", "0.5"). One longer than a Duration holds is refused, as
// time.ParseDuration refuses such a length: a float beyond int64 converts to what the processor
// makes of it, which differs between amd64 and arm64.
func inDays(count string) (time.Duration, error) {
	days, err := strconv.ParseFloat(count, 64)
	if err != nil {
		return 0, err
	}
	nanoseconds := days * float64(24*time.Hour)
	if !(math.Abs(nanoseconds) < math.MaxInt64) { // NaN and ±Inf too
		return 0, errors.New("longer than a Duration holds")
	}
	return time.Duration(nanoseconds), nil
}

// given says whether the flag was.
func (s since) given() bool {
	return s.lastSeconds != 0 || !s.start.IsZero()
}

// words are the range in a sentence: "in the last hour", "since today at 14:00"; when the flag
// was not given, in the length the machine answered for, such as its day.
func (s since) words(stream *ui.Stream, answeredFor time.Duration) string {
	switch {
	case !s.start.IsZero():
		return "since " + stream.When(s.start)
	case s.lastSeconds != 0:
		return "in " + lastWords(time.Duration(s.lastSeconds)*time.Second)
	}
	return "in " + lastWords(answeredFor)
}

// lastWords is a length of time that ends now, in words: "the last 24 hours", "the last hour",
// "the last 90 minutes", "the last 2 days": in the largest unit that holds it whole, and never
// rounded to what was not asked for.
func lastWords(length time.Duration) string {
	length = length.Round(time.Second)
	switch {
	case length > 24*time.Hour && length%(24*time.Hour) == 0:
		return theLast(length, 24*time.Hour, "day")
	case length%time.Hour == 0:
		return theLast(length, time.Hour, "hour")
	case length%time.Minute == 0:
		return theLast(length, time.Minute, "minute")
	}
	return theLast(length, time.Second, "second")
}

func theLast(length, unit time.Duration, name string) string {
	if length == unit {
		return "the last " + name
	}
	return fmt.Sprintf("the last %d %ss", length/unit, name)
}
