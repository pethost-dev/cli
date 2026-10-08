package ui

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// Questions. Each is asked on stderr, of a person at a terminal. Where nobody can be asked a
// question is never guessed: it ends as a NeedsChoice, which names the commands that answer it.

// NeedsChoice is a question that could not be asked. As an error it ends the command with the
// exit code of "a choice is needed", and the commands are printed for the person or the program
// to run instead.
type NeedsChoice struct {
	// What was to be decided, as one sentence.
	Message string
	// The words before the commands. Empty = "Run:", or "Run one of:" before several.
	Lead string
	// The full commands that decide it, one for each answer.
	Commands []string
}

func (e *NeedsChoice) Error() string { return e.Message }

// Confirm asks a yes-or-no question whose safe answer is no: "question [y/N]". --yes answers
// yes without asking. Off a terminal without --yes it is a NeedsChoice that names rerun, the
// command that says yes.
func (s *Screen) Confirm(question, rerun string) (bool, error) {
	if s.Yes {
		return true, nil
	}
	if !s.CanAsk() {
		return false, &NeedsChoice{Message: question, Lead: "Nobody can be asked here. To say yes, run:", Commands: []string{rerun}}
	}

	answer, err := s.prompt(question + " " + s.Err.Dim("[y/N]") + " ")
	if err != nil {
		return false, err
	}
	return isYes(answer), nil
}

// Offer asks a yes-or-no question whose usual answer is yes: "question [Y/n]". It is never
// needed: off a terminal the answer is no, and nothing is written.
func (s *Screen) Offer(question string) (bool, error) {
	if !s.CanAsk() {
		return false, nil
	}

	answer, err := s.prompt(question + " " + s.Err.Dim("[Y/n]") + " ")
	if err != nil {
		return false, err
	}
	return answer == "" || isYes(answer), nil
}

// ConfirmName asks the person to type name to go on, as before a project is deleted: only the
// name is a yes. --yes answers it, and off a terminal it is a NeedsChoice, as Confirm's.
func (s *Screen) ConfirmName(question, name, rerun string) (bool, error) {
	if s.Yes {
		return true, nil
	}
	if !s.CanAsk() {
		return false, &NeedsChoice{Message: question, Lead: "Nobody can be asked here. To go on, run:", Commands: []string{rerun}}
	}

	answer, err := s.prompt(fmt.Sprintf("%s Type %s to go on: ", question, s.Err.Bold(name)))
	if err != nil {
		return false, err
	}
	return answer == name, nil
}

// Enter waits until the person presses Enter (true), or until stop is closed (false). It may
// wait beside lines redrawn in place.
func (s *Screen) Enter(stop <-chan struct{}) (bool, error) {
	_, err := s.line(stop)
	if err == errStopped {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	// The terminal echoed the Enter: the cursor is a line lower than the redrawn lines think.
	s.mu.Lock()
	if s.live != nil {
		s.live.drawn++
	}
	s.mu.Unlock()
	return true, nil
}

// prompt writes a question and reads the line typed after it. Input that ends there (Ctrl-D)
// leaves the question as Ctrl-C does.
func (s *Screen) prompt(question string) (string, error) {
	s.write(question)
	answer, err := s.line(nil)
	if err == io.EOF {
		err = ErrInterrupted
	}
	if err != nil {
		s.write("\n")
		return "", err
	}
	return strings.TrimSpace(answer), nil
}

func isYes(answer string) bool {
	answer = strings.ToLower(answer)
	return answer == "y" || answer == "yes"
}

// write writes to stderr as it is, for a question's own drawing.
func (s *Screen) write(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	io.WriteString(s.Err, text)
}

// Secret reads a value that must not be seen: a password, a variable's value. On a terminal it
// asks "label: " and shows dots in place of what is typed; elsewhere it is all of stdin, less
// its last line break, so that a program pipes it in.
func (s *Screen) Secret(label string) (string, error) {
	if !s.CanAsk() {
		value, err := io.ReadAll(s.In)
		return strings.TrimSuffix(strings.TrimSuffix(string(value), "\n"), "\r"), err
	}

	restore, err := s.raw()
	if err != nil {
		return "", err
	}
	defer restore()

	s.write(label + ": ")
	value, err := s.typedLine(func(string) {})
	if err != nil {
		s.write("\r\n")
		return "", err
	}
	if value != "" {
		s.write(strings.Repeat("•", 8))
	}
	s.write("\r\n")
	return value, nil
}

// hintColumn is where a question's hint starts when the answer is short enough.
const hintColumn = 48

// Ask asks for one line with a fallback, "label [fallback]: ", and shows hint(value) beside
// what is typed, drawn again at each key: a name beside the address it will have. An empty
// answer is the fallback. It needs a terminal (CanAsk): the caller decides what happens
// without one.
func (s *Screen) Ask(label, fallback string, hint func(value string) string) (string, error) {
	restore, err := s.raw()
	if err != nil {
		return "", err
	}
	defer restore()

	question := label + ": "
	if fallback != "" {
		question = label + " " + s.Err.Dim("["+fallback+"]") + ": "
	}
	draw := func(value string) {
		said := hint(orFallback(value, fallback))
		typed, width := Width(question+value), s.Err.Width()
		// The hint starts in its column, or nearer when the line would not hold it there.
		gap := max(3, min(hintColumn-typed, width-1-typed-Width(said)))
		line := cut(question+value+strings.Repeat(" ", gap)+s.Err.Dim(said), width)
		s.write(fmt.Sprintf("\r\x1b[K%s\r\x1b[%dC", line, Width(question+value)))
	}

	draw("")
	value, err := s.typedLine(draw)
	s.write("\r\n")
	if err != nil {
		return "", err
	}
	return orFallback(strings.TrimSpace(value), fallback), nil
}

func orFallback(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// typedLine reads a line in raw mode, calling changed with the line so far after each key that
// changes it, so that its caller echoes it as it likes.
func (s *Screen) typedLine(changed func(value string)) (string, error) {
	value := ""
	for {
		k, err := s.press()
		if err != nil {
			return "", err
		}

		switch k.kind {
		case keyEnter:
			return value, nil
		case keyLeave:
			return "", ErrInterrupted
		case keyBackspace:
			_, size := utf8.DecodeLastRuneInString(value)
			value = value[:len(value)-size]
		case keyClear:
			value = ""
		case keyChar:
			value += string(k.char)
		default:
			continue
		}
		changed(value)
	}
}
