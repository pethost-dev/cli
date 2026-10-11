// Package ui is what the CLI prints and asks: the three streams and whether a person reads each,
// colour and marks, the lines redrawn in place, tables, the words for times, sizes and
// durations, questions, and the cleaning of text that came from a machine.
//
// stdout holds a command's answer alone; everything else goes to stderr. Off a terminal nothing
// is asked, coloured or redrawn, and a moment is written as RFC 3339 in UTC.
package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"golang.org/x/term"
)

// A Screen is the three streams of one run of the CLI.
type Screen struct {
	// What the person types or pipes in.
	In *os.File
	// The answer alone: the URL, the lines, the names.
	Out *Stream
	// Everything else: progress, questions, errors.
	Err *Stream

	// --yes: a [y/N] question is answered yes without being asked.
	Yes bool
	// -q, and --json: only the answer and errors are written.
	Quiet bool
	// Closed when the run is interrupted: a question then ends with ErrInterrupted.
	Interrupt <-chan struct{}

	keys     chan byte
	keysOnce sync.Once

	mu   sync.Mutex // guards live and every write to Err
	live *Live
}

// A Stream is stdout or stderr, with what the CLI may do on it.
type Stream struct {
	w io.Writer
	// A person reads it: a terminal.
	Terminal bool
	// Terminal, and NO_COLOR is not set.
	Color bool

	fd int
}

// New is the screen of a process: its three files, each asked whether it is a terminal.
func New(in, out, err *os.File) *Screen {
	return &Screen{In: in, Out: newStream(out), Err: newStream(err)}
}

func newStream(file *os.File) *Stream {
	fd := int(file.Fd())
	terminal := term.IsTerminal(fd) && os.Getenv("TERM") != "dumb" && enableEscapes(file)
	_, noColor := os.LookupEnv("NO_COLOR")
	return &Stream{w: file, Terminal: terminal, Color: terminal && !noColor, fd: fd}
}

// CanAsk says whether a person can be asked: they type into a terminal and read stderr on one.
func (s *Screen) CanAsk() bool {
	return term.IsTerminal(int(s.In.Fd())) && s.Err.Terminal
}

func (s *Stream) Write(p []byte) (int, error) {
	return s.w.Write(p)
}

// Width is the terminal's width in columns; 80 when it cannot be read.
func (s *Stream) Width() int {
	if width, _, err := term.GetSize(s.fd); err == nil && width > 0 {
		return width
	}
	return 80
}

// Done writes "✓ text" to stderr: a step that ended well.
func (s *Screen) Done(format string, args ...any) {
	s.status(s.Err.Check() + fmt.Sprintf(format, args...))
}

// Problem writes "✗ text" to stderr: something that is wrong, which is not this command's
// failure. A failure is an error the command returns.
func (s *Screen) Problem(format string, args ...any) {
	s.status(s.Err.Cross() + fmt.Sprintf(format, args...))
}

// Note writes "  · text" to stderr: a line that is the machine's own, as it says it.
func (s *Screen) Note(format string, args ...any) {
	s.status("  " + s.Err.mark(s.Err.Dim("·")) + fmt.Sprintf(format, args...))
}

// Info writes a plain line to stderr.
func (s *Screen) Info(format string, args ...any) {
	s.status(fmt.Sprintf(format, args...))
}

// Next writes the last line of a screen: what to type next, the commonest first.
func (s *Screen) Next(commands ...string) {
	for i, command := range commands {
		commands[i] = s.Err.Bold(command)
	}
	s.status("Next:   " + strings.Join(commands, "   ·   "))
}

// status writes one line of progress to stderr, above what is redrawn in place. -q silences it.
func (s *Screen) status(line string) {
	if s.Quiet {
		return
	}
	s.ErrLine(line)
}

// ErrLine writes one line to stderr whatever -q says, above what is redrawn in place: an
// error's. The CLI's own lines only colour, so whatever else a terminal would obey is taken out
// here, once for every line: a word of the command line said back in an error, or a name its
// writer forgot to clean.
func (s *Screen) ErrLine(line string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.live != nil {
		s.live.erase()
	}
	fmt.Fprintln(s.Err, s.Err.Text(line))
	if s.live != nil {
		s.live.draw()
	}
}
