package ui

import (
	"cmp"
	"fmt"
	"strings"
)

// A Choice is one line of a picker: what is chosen, and a few words about it.
type Choice struct {
	Label  string
	Detail string
	// The full command that makes this choice where nobody can be asked.
	Command string
}

// Pick asks the person to choose one of a few things with the arrow keys and Enter (or its
// number), and returns its index. Off a terminal it is a NeedsChoice that names each choice's
// Command. Esc or Ctrl-C leaves it with ErrInterrupted.
func (s *Screen) Pick(question string, choices []Choice) (int, error) {
	if !s.CanAsk() {
		return 0, needsPick(question, choices)
	}

	restore, err := s.rawInput()
	if err != nil {
		return 0, err
	}
	defer restore()

	at := 0
	s.write(s.pickerLines(question, choices, at))
	for {
		k, err := s.press()
		if err != nil || k.kind == keyLeave {
			s.write("\r\n")
			return 0, cmp.Or(err, ErrInterrupted)
		}

		switch {
		case k.kind == keyEnter:
			s.write(rewind(len(choices)+1) + question + " " + s.Err.Bold(choices[at].Label) + "\r\n")
			return at, nil
		case k.kind == keyUp || k.char == 'k':
			at = (at + len(choices) - 1) % len(choices)
		case k.kind == keyDown || k.char == 'j':
			at = (at + 1) % len(choices)
		case k.char >= '1' && int(k.char-'1') < len(choices):
			at = int(k.char - '1')
		default:
			continue
		}
		s.write(rewind(len(choices)+1) + s.pickerLines(question, choices, at))
	}
}

// needsPick is the question that could not be asked, with the command for each choice.
func needsPick(question string, choices []Choice) *NeedsChoice {
	commands := make([]string, len(choices))
	for i, choice := range choices {
		commands[i] = choice.Command
	}
	return &NeedsChoice{Message: question, Commands: commands}
}

// rewind moves the cursor up over lines drawn before and clears them.
func rewind(lines int) string {
	return fmt.Sprintf("\r\x1b[%dA\x1b[J", lines)
}

// pickerLines draws the question and its choices, the one the cursor is at marked.
func (s *Screen) pickerLines(question string, choices []Choice, at int) string {
	width := 0
	for _, choice := range choices {
		width = max(width, Width(choice.Label))
	}

	var out strings.Builder
	out.WriteString(question + " " + s.Err.Dim("(↑ ↓, then Enter)") + "\r\n")
	for i, choice := range choices {
		line := "  " + choice.Label
		if i == at {
			line = s.Err.Cyan("❯ ") + s.Err.Bold(choice.Label)
		}
		if choice.Detail != "" {
			line += strings.Repeat(" ", width-Width(choice.Label)+columnGap) + s.Err.Dim(choice.Detail)
		}
		out.WriteString(cut(line, s.Err.Width()) + "\r\n")
	}
	return out.String()
}
