package ui

import (
	"errors"
	"io"
	"time"
	"unicode/utf8"

	"golang.org/x/term"
)

// What the person types. One goroutine reads stdin for the whole run, so that a question nobody
// answered (Enter to open a link, while the CLI went on by itself) does not take the answer to
// the next one.

// ErrInterrupted ends a question the person left with Ctrl-C or Esc, or that the run's
// interruption cut short.
var ErrInterrupted = errors.New("interrupted")

// typed is the channel of the bytes typed, started on first use; it is closed when stdin ends.
// It holds one read's bytes, so that a key which sends several, as an arrow does, is there whole
// for whoever takes its first byte.
func (s *Screen) typed() <-chan byte {
	s.keysOnce.Do(func() {
		s.keys = make(chan byte, readAtOnce)
		go s.readKeys()
	})
	return s.keys
}

// readAtOnce is how many bytes one read of stdin takes.
const readAtOnce = 256

func (s *Screen) readKeys() {
	defer close(s.keys)
	buffer := make([]byte, readAtOnce)
	for {
		n, err := s.in.Read(buffer)
		for _, b := range buffer[:n] {
			s.keys <- b
		}
		if typedCtrlZ(s.in, n, err) {
			s.keys <- 0x1a
			continue
		}
		if err != nil {
			return
		}
	}
}

// nextByte waits for a byte, the run's interruption or stop.
func (s *Screen) nextByte(stop <-chan struct{}) (byte, error) {
	select {
	case b, open := <-s.typed():
		if !open {
			return 0, io.EOF
		}
		return b, nil
	case <-s.Interrupt:
		return 0, ErrInterrupted
	case <-stop:
		return 0, errStopped
	}
}

var errStopped = errors.New("stopped")

// line reads what the person types up to Enter, while the terminal itself echoes and edits it.
func (s *Screen) line(stop <-chan struct{}) (string, error) {
	var line []byte
	for {
		b, err := s.nextByte(stop)
		switch {
		case err == io.EOF && len(line) > 0:
			return string(line), nil
		case err != nil:
			return "", err
		case b == '\n':
			return string(trimCR(line)), nil
		}
		line = append(line, b)
	}
}

func trimCR(line []byte) []byte {
	if n := len(line); n > 0 && line[n-1] == '\r' {
		return line[:n-1]
	}
	return line
}

// A key is one press on a terminal that the CLI reads itself (raw mode): a character, or one of
// the keys below.
type key struct {
	char rune // 0 = one of the special keys
	kind keyKind
}

type keyKind int

const (
	keyChar keyKind = iota
	keyEnter
	keyBackspace
	keyClear // Ctrl-U
	keyUp
	keyDown
	keyLeave // Ctrl-C, Ctrl-D, Esc
	keyOther
)

// raw puts the terminal into raw mode, where each press arrives as it is made and nothing is
// echoed; the function it returns puts it back.
func (s *Screen) raw() (restore func(), err error) {
	fd := int(s.in.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return nil, err
	}
	return func() { _ = term.Restore(fd, state) }, nil
}

// press reads one key in raw mode.
func (s *Screen) press() (key, error) {
	b, err := s.nextByte(nil)
	if err != nil {
		return key{}, err
	}

	switch {
	case b == '\r' || b == '\n':
		return key{kind: keyEnter}, nil
	case b == 0x7f || b == 0x08:
		return key{kind: keyBackspace}, nil
	case b == 0x15:
		return key{kind: keyClear}, nil
	case b == 0x03 || b == 0x04:
		return key{kind: keyLeave}, nil
	case b == escape:
		return s.afterEscape(), nil
	case b < 0x20:
		return key{kind: keyOther}, nil
	}
	return s.character(b)
}

// escapeWait is how long after Esc the rest of an arrow key's sequence may come: later, the
// person pressed Esc itself.
const escapeWait = 50 * time.Millisecond

// afterEscape reads what follows an Esc byte: nothing (the Esc key), or the rest of the
// sequence a key such as an arrow sends.
func (s *Screen) afterEscape() key {
	second, ok := s.soon()
	if !ok {
		return key{kind: keyLeave}
	}
	if second != '[' && second != 'O' {
		return key{kind: keyOther}
	}

	for {
		b, ok := s.soon()
		if !ok {
			return key{kind: keyOther}
		}
		if b >= 0x40 && b <= 0x7e {
			return arrow(b)
		}
	}
}

func arrow(final byte) key {
	switch final {
	case 'A':
		return key{kind: keyUp}
	case 'B':
		return key{kind: keyDown}
	}
	return key{kind: keyOther}
}

// soon is the next byte if it comes within escapeWait.
func (s *Screen) soon() (byte, bool) {
	select {
	case b, open := <-s.typed():
		return b, open
	case <-time.After(escapeWait):
		return 0, false
	}
}

// character reads the rest of a UTF-8 character whose first byte is given.
func (s *Screen) character(first byte) (key, error) {
	encoded := []byte{first}
	for !utf8.FullRune(encoded) {
		b, err := s.nextByte(nil)
		if err != nil {
			return key{}, err
		}
		encoded = append(encoded, b)
	}
	r, _ := utf8.DecodeRune(encoded)
	return key{char: r}, nil
}
