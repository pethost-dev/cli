// Package dotenv is a .env file as text, in Docker Compose's format (NAME=value, quotes,
// comments, `export`): it finds the statements as Compose does, reads the value each gives as
// Compose does, and writes a value so that Compose reads exactly it back. The API takes the
// project's /.env whole, so a change to one variable is an edit of the text: that variable's
// statement is written anew, and every other byte of the file stays as it is. Whether a file is
// a good one is the machine's to say, when it deploys it.
package dotenv

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// An assignment is one variable's statement in a .env text.
type assignment struct {
	name string
	// text[start:end] is the statement from its name to the end of its value: what an edit
	// replaces. An `export` before it and a comment after it are outside.
	start, end int
	// text[value:end] is the value as written, after the separator; -1 = the line holds the name
	// alone, which sets nothing.
	value int
}

var exportPrefix = regexp.MustCompile(`^export\s+`)

// assignments finds the statements of a .env text, in its order.
func assignments(text string) []assignment {
	var found []assignment
	for at := 0; at < len(text); {
		at += len(text[at:]) - len(strings.TrimLeftFunc(text[at:], unicode.IsSpace))
		if at == len(text) {
			break
		}
		if text[at] == '#' {
			at = lineEnd(text, at)
			continue
		}
		at += len(exportPrefix.FindString(text[at:]))

		// A name ends at its separator.
		nameEnd := at + len(text[at:])
		if i := strings.IndexAny(text[at:], "=:\n"); i >= 0 {
			nameEnd = at + i
		}
		end, value := nameEnd, -1
		if nameEnd < len(text) && text[nameEnd] != '\n' {
			value = nameEnd + 1
			end = valueEnd(text, value)
		}

		found = append(found, assignment{name: strings.TrimRightFunc(text[at:nameEnd], unicode.IsSpace), start: at, end: end, value: value})
		at = max(end, at+1)
	}
	return found
}

// valueEnd is where the value that starts at or after from ends: a quoted one after its closing
// quote, which may be lines below; a bare one at its line's end, or before the comment on it.
func valueEnd(text string, from int) int {
	from = valueStart(text, from)
	if from < len(text) && (text[from] == '"' || text[from] == '\'') {
		end, _ := quoted(text, from)
		return end
	}

	line := text[from:lineEnd(text, from)]
	if comment := strings.Index(line, " #"); comment >= 0 {
		line = line[:comment]
	}
	return from + len(strings.TrimRight(line, " \t\r"))
}

// valueStart is where a value written from on begins, past the spaces before it.
func valueStart(text string, from int) int {
	return from + len(text[from:]) - len(strings.TrimLeft(text[from:], " \t"))
}

// quoted reads the quotes that open at text[open]: the index after the quote that closes them,
// and what they hold. A backslash takes the character after it out of play, and is left out
// before the quote it escapes; a quote never closed runs to the text's end.
func quoted(text string, open int) (end int, held string) {
	var chars strings.Builder
	escaped := false
	for i := open + 1; i < len(text); i++ {
		switch c := text[i]; {
		case escaped:
			escaped = false
			if c != text[open] {
				chars.WriteByte('\\')
			}
			chars.WriteByte(c)
		case c == '\\':
			escaped = true
		case c == text[open]:
			return i + 1, chars.String()
		default:
			chars.WriteByte(c)
		}
	}
	return len(text), chars.String()
}

// lineEnd is the index of the line break that ends the line at holds, or the text's end.
func lineEnd(text string, at int) int {
	if i := strings.IndexByte(text[at:], '\n'); i >= 0 {
		return at + i
	}
	return len(text)
}

// valueOf is the value a statement gives: a bare one as it stands, a quoted one without its
// quotes, and in double quotes with the escapes read (\n, \t, \", \\, \$ …). A variable that a
// value names ($NAME, ${NAME}) is left as written: Compose puts the value of that name in its
// place.
func (s assignment) valueOf(text string) string {
	from := valueStart(text, s.value)
	if from == s.end || (text[from] != '"' && text[from] != '\'') {
		return text[from:s.end]
	}
	_, held := quoted(text, from)
	if text[from] == '\'' {
		return held
	}
	return escapeSequence.ReplaceAllStringFunc(held, unescaped)
}

// escapeSequence is an escape that Compose reads in double quotes.
var escapeSequence = regexp.MustCompile(`\\(?:[abcfnrtv$"\\]|0\d{0,3})`)

// unescaped is the character an escape stands for, as Go reads it in a string literal, and
// "\0" before an octal number; one that Go does not read stays as written.
func unescaped(sequence string) string {
	if sequence == `\$` {
		return "$"
	}
	sequence = strings.Replace(sequence, `\0`, `\`, 1)
	char, _, _, err := strconv.UnquoteChar(sequence, '"')
	if err != nil {
		return sequence
	}
	return string(char)
}

// Names are the names a .env text sets, each once, in the text's order.
func Names(text string) []string {
	var names []string
	for _, statement := range assignments(text) {
		if !slices.Contains(names, statement.name) {
			names = append(names, statement.name)
		}
	}
	return names
}

// Values are the variables a .env text sets and the value of each, as Compose reads them: a
// name's last statement is the one that counts.
func Values(text string) map[string]string {
	values := map[string]string{}
	for _, statement := range assignments(text) {
		if statement.value >= 0 {
			values[statement.name] = statement.valueOf(text)
		}
	}
	return values
}

// Statement is name's statement as the text writes it, the last one, which is the one that
// counts; false = the text does not set it.
func Statement(text, name string) (string, bool) {
	all := assignments(text)
	for i := len(all) - 1; i >= 0; i-- {
		if all[i].name == name {
			return text[all[i].start:all[i].end], true
		}
	}
	return "", false
}

// With is the text with name set to value: its last statement, the one that counts, written
// anew in place, or a new last line.
func With(text, name, value string) string {
	statement := name + "=" + encoded(value)

	all := assignments(text)
	for i := len(all) - 1; i >= 0; i-- {
		if all[i].name != name {
			continue
		}
		// An empty value before a comment needs its quotes, or the comment is read as the value.
		after := text[all[i].end:lineEnd(text, all[i].end)]
		if value == "" && strings.TrimRight(after, " \t\r") != "" {
			statement = name + "=''"
		}
		return text[:all[i].start] + statement + text[all[i].end:]
	}
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return text + statement + "\n"
}

// Without is the text without name's statements. A statement goes with its whole line (an
// `export` before it, a comment after it) unless the line holds another statement too.
func Without(text, name string) string {
	all := assignments(text)
	for i := len(all) - 1; i >= 0; i-- {
		if all[i].name != name {
			continue
		}

		from, to := all[i].start, all[i].end
		lineFrom := strings.LastIndexByte(text[:from], '\n') + 1
		lineTo := min(lineEnd(text, to)+1, len(text))
		shared := slices.ContainsFunc(all, func(other assignment) bool {
			return other != all[i] && other.start >= lineFrom && other.start < lineTo
		})
		if !shared {
			from, to = lineFrom, lineTo
		}
		text = text[:from] + text[to:]
	}
	return text
}

// encoded is a value as a .env line holds it so that a service gets exactly it: bare when
// nothing in it could be read as syntax; else in single quotes, which take everything as it
// is; else, for a value with a single quote or a last backslash, in double quotes with what
// they would read escaped.
func encoded(value string) string {
	switch {
	case !strings.ContainsFunc(value, isSyntax):
		return value
	case !strings.Contains(value, "'") && !strings.HasSuffix(value, `\`):
		return "'" + value + "'"
	}
	return `"` + inDoubleQuotes.Replace(value) + `"`
}

// isSyntax says whether a character may mean something to Compose in a bare value: a space
// ends it, `#` starts a comment, `$` reads a variable, a quote opens a quoted value.
func isSyntax(r rune) bool {
	return unicode.IsSpace(r) || unicode.IsControl(r) || strings.ContainsRune("#$'\"\\`", r)
}

var inDoubleQuotes = strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`, "\n", `\n`, "\r", `\r`)
