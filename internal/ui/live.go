package ui

import (
	"fmt"
	"strings"
	"time"
)

// A Live is a few lines at the bottom of stderr that are drawn again in place: a title with a
// spinner and the time it has taken, and under it the last lines of a log. Lines written to
// stderr meanwhile go above it. Off a terminal, and with -q, it draws nothing.
type Live struct {
	screen  *Screen // nil = it draws nothing
	title   string
	tail    []string
	started time.Time
	frame   int
	drawn   int // The lines on the screen now.
	stop    chan struct{}
}

// liveTail is how many of a log's last lines stay under the title.
const liveTail = 5

var spinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Live starts the redrawn lines with a title, such as "Building…". Stop ends them. One runs at
// a time: a second one replaces the first.
func (s *Screen) Live(title string) *Live {
	if !s.Err.Terminal || s.Quiet {
		return &Live{}
	}

	l := &Live{screen: s, title: title, started: time.Now(), stop: make(chan struct{})}
	s.mu.Lock()
	if s.live != nil {
		s.live.end()
	}
	s.live = l
	l.draw()
	s.mu.Unlock()

	go l.spin()
	return l
}

// Title changes the title.
func (l *Live) Title(title string) {
	l.change(func() { l.title = title })
}

// Line adds a line of the log shown under the title; the last few stay.
func (l *Live) Line(text string) {
	l.change(func() {
		l.tail = append(l.tail, text)
		if len(l.tail) > liveTail {
			l.tail = l.tail[len(l.tail)-liveTail:]
		}
	})
}

// Stop erases the lines: what the command writes next takes their place.
func (l *Live) Stop() {
	if l.screen == nil {
		return
	}
	l.screen.mu.Lock()
	defer l.screen.mu.Unlock()

	if l.screen.live == l {
		l.end()
	}
}

// end erases the lines and stops the spinner; the screen's lock is held.
func (l *Live) end() {
	l.erase()
	close(l.stop)
	l.screen.live = nil
}

func (l *Live) change(apply func()) {
	if l.screen == nil {
		return
	}
	l.screen.mu.Lock()
	defer l.screen.mu.Unlock()

	apply()
	if l.screen.live == l {
		l.erase()
		l.draw()
	}
}

func (l *Live) spin() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-l.stop:
			return
		case <-ticker.C:
			l.change(func() { l.frame++ })
		}
	}
}

// erase removes the lines drawn; the screen's lock is held.
func (l *Live) erase() {
	if l.drawn > 0 {
		// From the line's start: a ^C the terminal echoed has moved the cursor along it.
		fmt.Fprintf(l.screen.Err, "\r\x1b[%dA\x1b[J", l.drawn)
		l.drawn = 0
	}
}

// draw writes the lines; the screen's lock is held. No line may wrap, or erase would leave part
// of it: each is cut to the terminal's width.
func (l *Live) draw() {
	err := l.screen.Err
	width := err.Width()

	elapsed := Duration(time.Since(l.started))
	lines := []string{err.Cyan(spinner[l.frame%len(spinner)]) + " " + l.title + " " + err.Dim(elapsed)}
	for _, line := range l.tail {
		lines = append(lines, "    "+err.Dim(line))
	}

	var out strings.Builder
	for _, line := range lines {
		out.WriteString(cut(line, width) + "\n")
	}
	fmt.Fprint(err, out.String())
	l.drawn = len(lines)
}
