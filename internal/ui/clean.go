package ui

import (
	"strings"
	"unicode/utf8"
)

// Text that came from a machine (log lines, paths, file contents, names) is other people's: its
// escape sequences could move the cursor, rewrite the lines above or set the window's title.
// These functions are what such text goes through before it is printed.

// Clean is text without escape sequences, without control characters but tab and newline, and
// without the controls that reorder letters.
func Clean(text string) string {
	return clean(text, false)
}

// Text is machine text for this stream: on a colour terminal its colours stay (a log line's are
// its program's own), every other escape sequence goes; elsewhere Clean.
func (s *Stream) Text(text string) string {
	return clean(text, s.Color)
}

func clean(text string, keepColors bool) string {
	// Most text holds nothing to remove.
	if !strings.ContainsFunc(text, Obeyed) {
		return text
	}

	var out strings.Builder
	for i := 0; i < len(text); {
		if text[i] == escape {
			end, _ := sequenceEnd(text, i)
			if keepColors && isColor(text[i:end]) {
				out.WriteString(text[i:end])
			}
			i = end
			continue
		}
		r, size := utf8.DecodeRuneInString(text[i:])
		if !Obeyed(r) {
			out.WriteString(text[i : i+size])
		}
		i += size
	}
	return out.String()
}

const escape = 0x1b

// Obeyed says whether a character is one a terminal obeys rather than shows: a control
// character but tab and newline, or a control that reorders the letters around it.
func Obeyed(r rune) bool {
	switch {
	case r == '\t' || r == '\n':
		return false
	case r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f):
		return true
	case (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069):
		return true
	}
	return false
}

// sequenceEnd is where the escape sequence that starts at text[start] ends. whole = its end is
// in text: a sequence the text cuts ends where the text does.
func sequenceEnd(text string, start int) (end int, whole bool) {
	if start+1 >= len(text) {
		return len(text), false
	}
	switch text[start+1] {
	case '[': // CSI: parameters, then one final byte.
		for i := start + 2; i < len(text); i++ {
			if text[i] >= 0x40 && text[i] <= 0x7e {
				return i + 1, true
			}
		}
		return len(text), false
	case ']', 'P', 'X', '^', '_': // A string, to BEL or to ESC \.
		for i := start + 2; i < len(text); i++ {
			if text[i] == 0x07 {
				return i + 1, true
			}
			if text[i] == escape && i+1 < len(text) && text[i+1] == '\\' {
				return i + 2, true
			}
		}
		return len(text), false
	}
	return start + 2, true
}

// isColor says whether an escape sequence only sets colours and weight (SGR).
func isColor(sequence string) bool {
	if len(sequence) < 3 || sequence[1] != '[' || sequence[len(sequence)-1] != 'm' {
		return false
	}
	for _, c := range sequence[2 : len(sequence)-1] {
		if (c < '0' || c > '9') && c != ';' && c != ':' {
			return false
		}
	}
	return true
}

// Width is how many columns text takes on a terminal, its colours not counted.
//
// ponytail: wide characters are told by a few ranges (CJK, emoji), not by Unicode's tables:
// a rare character may shift a table's column by one. A width library when that is seen.
func Width(text string) int {
	width := 0
	for _, r := range clean(text, false) {
		width += runeWidth(r)
	}
	return width
}

func runeWidth(r rune) int {
	switch {
	case r == 0x200d || (r >= 0xfe00 && r <= 0xfe0f) || (r >= 0x300 && r <= 0x36f):
		return 0 // A joiner, a variation selector, a combining mark.
	case r >= 0x1f3fb && r <= 0x1f3ff:
		return 0 // A skin tone, drawn into the emoji before it.
	case r >= 0x1100 && r <= 0x115f, r >= 0x2e80 && r <= 0xa4cf, r >= 0xac00 && r <= 0xd7a3,
		r >= 0xf900 && r <= 0xfaff, r >= 0xfe30 && r <= 0xfe4f, r >= 0xff00 && r <= 0xff60,
		r >= 0xffe0 && r <= 0xffe6, r >= 0x1f300 && r <= 0x1faff, r >= 0x20000 && r <= 0x3fffd:
		return 2
	}
	return 1
}

// cut is text cut to at most width columns, colours kept whole: a line that must not wrap.
func cut(text string, width int) string {
	if Width(text) <= width {
		return text
	}

	var out strings.Builder
	used := 0
	for i := 0; i < len(text); {
		if text[i] == escape {
			end, _ := sequenceEnd(text, i)
			out.WriteString(text[i:end])
			i = end
			continue
		}
		r, size := utf8.DecodeRuneInString(text[i:])
		if used+runeWidth(r) > width-1 {
			break
		}
		used += runeWidth(r)
		out.WriteString(text[i : i+size])
		i += size
	}
	return out.String() + "…\x1b[0m"
}
