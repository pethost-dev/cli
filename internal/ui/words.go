package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// The words for sizes, counts, durations and moments, the same on every screen: "212 KB",
// "1.9k requests", "41 s", "today at 11:20". Units are the panel's: 1 KB is 1024 bytes.

const (
	kb = 1 << 10
	mb = 1 << 20
	gb = 1 << 30
)

// Size is a number of bytes for people: "17 B", "212 KB", "8.5 MB", "312 MB", "1.4 GB", "3 GB".
func Size(bytes uint64) string {
	number, unit := sized(bytes)
	return number + " " + unit
}

// SizeOf is a used part of a total, in the total's unit: "1.4 of 3 GB"; a part too small to
// show in it keeps its own, "620 KB of 2 GB".
func SizeOf(used, total uint64) string {
	number, unit := sized(total)
	part := inUnit(used, unit)
	if part == "0" && used > 0 {
		part = Size(used)
	}
	return part + " of " + number + " " + unit
}

func sized(bytes uint64) (number, unit string) {
	switch {
	case bytes < kb:
		unit = "B"
	case bytes < mb:
		unit = "KB"
	case bytes < gb:
		unit = "MB"
	default:
		unit = "GB"
	}
	return inUnit(bytes, unit), unit
}

// inUnit is a number of bytes counted in a unit: whole in B and KB, with one decimal below 10
// in MB and GB, and never with a trailing ".0".
func inUnit(bytes uint64, unit string) string {
	switch unit {
	case "B":
		return strconv.FormatUint(bytes, 10)
	case "KB":
		return decimal(float64(bytes)/kb, 0)
	case "MB":
		return few(float64(bytes) / mb)
	}
	return few(float64(bytes) / gb)
}

// few is a number with one decimal below 10 and none from there: 1.4, 3, 312.
func few(value float64) string {
	if value < 10 {
		return decimal(value, 1)
	}
	return decimal(value, 0)
}

// decimal is a number with at most digits decimals, trailing zeros dropped: 2, 3.6.
func decimal(value float64, digits int) string {
	text := strconv.FormatFloat(value, 'f', digits, 64)
	if strings.Contains(text, ".") {
		text = strings.TrimRight(strings.TrimRight(text, "0"), ".")
	}
	return text
}

// Count is a number of things for people: "392", "1.9k", "18.2k", "2.4M".
func Count(n uint64) string {
	switch {
	case n < 1000:
		return strconv.FormatUint(n, 10)
	case n < 1_000_000:
		return decimal(float64(n)/1000, 1) + "k"
	}
	return decimal(float64(n)/1_000_000, 1) + "M"
}

// Plural is a count with its noun: "1 file", "38 files", "1.9k requests".
func Plural(n uint64, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return Count(n) + " " + noun + "s"
}

// Percent is a share of a whole: "31%"; "0%" of nothing.
func Percent(part, whole uint64) string {
	if whole == 0 {
		return "0%"
	}
	return strconv.FormatFloat(float64(part)/float64(whole)*100, 'f', 0, 64) + "%"
}

// Millis is a response time: "<1 ms", "38 ms", "2.3 s".
func Millis(ms uint32) string {
	switch {
	case ms < 1:
		return "<1 ms"
	case ms < 1000:
		return strconv.FormatUint(uint64(ms), 10) + " ms"
	}
	return decimal(float64(ms)/1000, 1) + " s"
}

// Duration is how long something took or has lasted: "<1 s", "41 s", "3 min", "3 min 12 s",
// "2 h", "2 h 5 min", "3 d".
func Duration(d time.Duration) string {
	seconds := int64(d / time.Second)
	switch {
	case seconds < 1:
		return "<1 s"
	case seconds < 60:
		return fmt.Sprintf("%d s", seconds)
	case seconds < 3600:
		return two(seconds/60, "min", seconds%60, "s")
	case seconds < 86400:
		return two(seconds/3600, "h", seconds%3600/60, "min")
	}
	return two(seconds/86400, "d", seconds%86400/3600, "h")
}

// two is a duration in its two largest units, the smaller left out when it is zero.
func two(major int64, majorUnit string, minor int64, minorUnit string) string {
	if minor == 0 {
		return fmt.Sprintf("%d %s", major, majorUnit)
	}
	return fmt.Sprintf("%d %s %d %s", major, majorUnit, minor, minorUnit)
}

// Rough is a duration in its largest unit alone, as "ago" wants it: "40 s", "5 min", "2 h", "3 d".
func Rough(d time.Duration) string {
	seconds := int64(d / time.Second)
	switch {
	case seconds < 1:
		return "<1 s"
	case seconds < 60:
		return fmt.Sprintf("%d s", seconds)
	case seconds < 3600:
		return fmt.Sprintf("%d min", seconds/60)
	case seconds < 86400:
		return fmt.Sprintf("%d h", seconds/3600)
	}
	return fmt.Sprintf("%d d", seconds/86400)
}

// The moments. On a terminal each is in the person's time zone and in words; off one it is
// RFC 3339 in UTC, which a program reads. A zero time is "".

// Clock is a moment to the second, as a log line's: "14:02:11" today, "Oct 3 14:02:11" before.
func (s *Stream) Clock(t time.Time) string {
	return s.moment(t, func(local, now time.Time) string {
		if sameDay(local, now) {
			return local.Format("15:04:05")
		}
		return local.Format("Jan 2 15:04:05")
	})
}

// At is a moment to the minute: "13:58" today, "Oct 3, 13:58" before. It reads after "since".
func (s *Stream) At(t time.Time) string {
	return s.moment(t, func(local, now time.Time) string {
		if sameDay(local, now) {
			return local.Format("15:04")
		}
		return local.Format("Jan 2, 15:04")
	})
}

// When is a moment with its day in words: "today at 11:20", "yesterday at 11:20", "tomorrow at
// 03:00", "Oct 3 at 11:20".
func (s *Stream) When(t time.Time) string {
	return s.moment(t, func(local, now time.Time) string {
		switch {
		case sameDay(local, now):
			return "today at " + local.Format("15:04")
		case sameDay(local, now.AddDate(0, 0, -1)):
			return "yesterday at " + local.Format("15:04")
		case sameDay(local, now.AddDate(0, 0, 1)):
			return "tomorrow at " + local.Format("15:04")
		}
		return local.Format("Jan 2 at 15:04")
	})
}

// Ago is how long ago a moment was: "just now", "40 s ago", "2 h ago"; off a terminal the
// moment itself.
func (s *Stream) Ago(t time.Time) string {
	return s.moment(t, func(local, now time.Time) string {
		if now.Sub(local) < 5*time.Second {
			return "just now"
		}
		return Rough(now.Sub(local)) + " ago"
	})
}

func (s *Stream) moment(t time.Time, words func(local, now time.Time) string) string {
	if t.IsZero() {
		return ""
	}
	if !s.Terminal {
		return t.UTC().Format(time.RFC3339)
	}
	return words(t.Local(), time.Now())
}

func sameDay(a, b time.Time) bool {
	return a.Year() == b.Year() && a.YearDay() == b.YearDay()
}
