package ui

import (
	"io"
	"strings"
	"sync"
	"unicode/utf8"
)

// Machine text that comes in pieces, as a program's output does while it runs: a piece may end
// inside an escape sequence or inside a character, and cleaning each piece alone would let the
// two halves through.

// Cleaned is the stream as a writer of such text. On a terminal every piece is cleaned as Text
// cleans a line, and what the end of a piece cut waits for its rest; Close writes what still
// waits. Off a terminal the bytes pass as they are: a program reads them.
func (s *Stream) Cleaned() io.WriteCloser {
	if !s.Terminal {
		return passed{s}
	}
	return &cleaned{stream: s}
}

type passed struct{ io.Writer }

func (passed) Close() error { return nil }

type cleaned struct {
	stream *Stream
	// A command that is interrupted closes the writer while its output may still be coming.
	mu sync.Mutex
	// The start of an escape sequence or of a character whose rest has not come.
	held string
}

// heldAtMost bounds what waits for its rest: a sequence that never ends is dropped where it
// stands, not kept for ever.
const heldAtMost = 4 << 10

func (c *cleaned) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	text := c.held + string(p)
	whole := len(text) - unfinished(text)
	if len(text)-whole > heldAtMost {
		whole = len(text)
	}

	c.held = text[whole:]
	if _, err := io.WriteString(c.stream, c.stream.Text(text[:whole])); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *cleaned) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	_, err := io.WriteString(c.stream, c.stream.Text(c.held))
	c.held = ""
	return err
}

// unfinished is how many bytes at the end of text are the start of something whose rest may
// still come: an escape sequence without its end, else a character without its last bytes.
func unfinished(text string) int {
	if start := strings.LastIndexByte(text, escape); start >= 0 {
		if _, whole := sequenceEnd(text, start); !whole {
			return len(text) - start
		}
	}

	for back := 1; back <= min(utf8.UTFMax, len(text)); back++ {
		if tail := text[len(text)-back:]; utf8.RuneStart(tail[0]) {
			if utf8.FullRuneInString(tail) {
				return 0
			}
			return back
		}
	}
	return 0
}
