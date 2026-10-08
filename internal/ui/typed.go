package ui

import (
	"errors"
	"io"
	"sync"

	"golang.org/x/term"
)

// The keyboard handed to a program elsewhere, as a shell in a container: the terminal in raw
// mode, so that every key goes to that program and it draws the screen itself, and what is typed
// as a reader. The bytes still come through the run's one reader of stdin, so a key pressed
// after that program ended is the next question's.

// ErrNoTerminal is Raw's answer where nobody types into a terminal.
var ErrNoTerminal = errors.New("stdin is not a terminal")

// Raw puts the terminal the person types into in raw mode: nothing is echoed, Ctrl-C and
// Ctrl-Z are bytes like any other, and what is written is shown as it comes. The function it
// returns puts everything back; it may be called more than once, and from any goroutine.
// ErrNoTerminal = stdin is piped or a file.
func (s *Screen) Raw() (restore func(), err error) {
	if s.in == nil || !term.IsTerminal(int(s.in.Fd())) {
		return nil, ErrNoTerminal
	}
	restoreIn, err := s.raw()
	if err != nil {
		return nil, err
	}
	restoreOut, restoreErr := rawOutput(s.Out), rawOutput(s.Err)

	var once sync.Once
	return func() {
		once.Do(func() {
			// In reverse: where both are one console, each put back what it found.
			restoreErr()
			restoreOut()
			restoreIn()
		})
	}, nil
}

// Window is the size of the terminal's window now, in columns and rows, read from whichever of
// the three streams is a terminal; 80 by 24 when none says.
func (s *Screen) Window() (columns, rows int) {
	fds := []int{s.Out.fd, s.Err.fd}
	if s.in != nil {
		fds = append(fds, int(s.in.Fd()))
	}
	for _, fd := range fds {
		if columns, rows, err := term.GetSize(fd); err == nil && columns > 0 && rows > 0 {
			return columns, rows
		}
	}
	return 80, 24
}

// Typed is what the person types from now on, as a reader. It ends when stdin does, or when stop
// is closed: the keys after that are for whoever reads next.
func (s *Screen) Typed(stop <-chan struct{}) io.Reader {
	return typedReader{screen: s, stop: stop}
}

type typedReader struct {
	screen *Screen
	stop   <-chan struct{}
}

// Read waits for one byte and takes with it those that came beside it: a key that sends several,
// as an arrow does, stays in one piece.
func (r typedReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	keys := r.screen.typed()
	select {
	case b, open := <-keys:
		if !open {
			return 0, io.EOF
		}
		p[0] = b
	case <-r.stop:
		return 0, io.EOF
	}

	for n := 1; n < len(p); n++ {
		select {
		case b, open := <-keys:
			if !open {
				return n, nil
			}
			p[n] = b
		default:
			return n, nil
		}
	}
	return len(p), nil
}
