package change

import (
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// A .env file as text. The API takes the project's /.env whole, so a change to one variable is
// an edit of the text: that variable's statement is written anew, and every other byte of the
// file stays as it is. The format is Docker Compose's (NAME=value, quotes, comments, `export`):
// this file finds the statements as Compose does, and writes a value so that Compose reads
// exactly it back. Whether a file is a good one is the machine's to say, when it deploys it.

// An assignment is one variable's statement in a .env text.
type assignment struct {
	name string
	// text[start:end] is the statement from its name to the end of its value: what an edit
	// replaces. An `export` before it and a comment after it are outside.
	start, end int
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

		// A name ends at its separator; a line that holds a name alone sets nothing here.
		nameEnd := at + len(text[at:])
		if i := strings.IndexAny(text[at:], "=:\n"); i >= 0 {
			nameEnd = at + i
		}
		end := nameEnd
		if nameEnd < len(text) && text[nameEnd] != '\n' {
			end = valueEnd(text, nameEnd+1)
		}

		found = append(found, assignment{name: strings.TrimRightFunc(text[at:nameEnd], unicode.IsSpace), start: at, end: end})
		at = max(end, at+1)
	}
	return found
}

// valueEnd is where the value that starts at or after from ends: a quoted one after its closing
// quote, which may be lines below; a bare one at its line's end, or before the comment on it.
func valueEnd(text string, from int) int {
	from += len(text[from:]) - len(strings.TrimLeft(text[from:], " \t"))
	if from < len(text) && (text[from] == '"' || text[from] == '\'') {
		return quoteEnd(text, from)
	}

	line := text[from:lineEnd(text, from)]
	if comment := strings.Index(line, " #"); comment >= 0 {
		line = line[:comment]
	}
	return from + len(strings.TrimRight(line, " \t\r"))
}

// quoteEnd is the index after the quote that closes the one at text[open]. A backslash takes
// the character after it out of play; a quote never closed runs to the text's end.
func quoteEnd(text string, open int) int {
	escaped := false
	for i := open + 1; i < len(text); i++ {
		switch {
		case escaped:
			escaped = false
		case text[i] == '\\':
			escaped = true
		case text[i] == text[open]:
			return i + 1
		}
	}
	return len(text)
}

// lineEnd is the index of the line break that ends the line at holds, or the text's end.
func lineEnd(text string, at int) int {
	if i := strings.IndexByte(text[at:], '\n'); i >= 0 {
		return at + i
	}
	return len(text)
}

// variableNames are the names a .env text sets, each once, in the text's order.
func variableNames(text string) []string {
	var names []string
	for _, statement := range assignments(text) {
		if !slices.Contains(names, statement.name) {
			names = append(names, statement.name)
		}
	}
	return names
}

// statementOf is name's statement as the text writes it, the last one, which is the one that
// counts; false = the text does not set it.
func statementOf(text, name string) (string, bool) {
	all := assignments(text)
	for i := len(all) - 1; i >= 0; i-- {
		if all[i].name == name {
			return text[all[i].start:all[i].end], true
		}
	}
	return "", false
}

// withVariable is the text with name set to value: its last statement, the one that counts,
// written anew in place, or a new last line.
func withVariable(text, name, value string) string {
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

// withoutVariable is the text without name's statements. A statement goes with its whole line
// (an `export` before it, a comment after it) unless the line holds another statement too.
func withoutVariable(text, name string) string {
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
