package observe

import (
	"fmt"
	"strings"
	"time"

	pethost "github.com/pethost-dev/sdk/go"

	"github.com/pethost-dev/cli/internal/ui"
)

// A log line on the screen: "14:02:11 web  TypeError: …", its time, its service and what the
// container wrote. On a terminal the services stand in one column, each in a colour of its own;
// off one the three are separated by tabs.

// A logPrinter writes log lines, and keeps what makes them line up: the widest service's name
// so far, and each service's colour.
type logPrinter struct {
	out    *ui.Stream
	width  int
	colors map[string]int
}

// page writes lines that came together: the column is as wide as their widest service before
// the first of them is written.
func (p *logPrinter) page(lines []pethost.LogLine) {
	for _, line := range lines {
		p.width = max(p.width, ui.Width(ui.Clean(line.Service)))
	}
	for _, line := range lines {
		p.line(line)
	}
}

func (p *logPrinter) line(line pethost.LogLine) {
	service := ui.Clean(line.Service)
	if !p.out.Terminal {
		fmt.Fprintf(p.out, "%s\t%s\t%s\n", p.out.Clock(line.Time), service, ui.Clean(line.Text))
		return
	}

	p.width = max(p.width, ui.Width(service))
	gap := strings.Repeat(" ", p.width-ui.Width(service)+2)
	fmt.Fprintf(p.out, "%s %s%s%s\n", p.out.Dim(p.out.Clock(line.Time)), p.out.Tint(p.color(service), service), gap, programText(p.out, line.Text))
}

// color is a service's colour: the services take the colours in the order they first wrote.
func (p *logPrinter) color(service string) int {
	if p.colors == nil {
		p.colors = map[string]int{}
	}
	if _, known := p.colors[service]; !known {
		p.colors[service] = len(p.colors)
	}
	return p.colors[service]
}

// programText is what a program wrote, for the stream: its colours stay on a colour terminal,
// and end with the line, so that one left open does not paint what is written next.
func programText(stream *ui.Stream, text string) string {
	text = stream.Text(text)
	if strings.Contains(text, "\x1b") {
		text += "\x1b[0m"
	}
	return text
}

// operationLine writes a line of an operation's log: its time and what Compose, the build or
// the machine wrote.
func operationLine(out *ui.Stream, moment time.Time, text string) {
	if !out.Terminal {
		fmt.Fprintf(out, "%s\t%s\n", out.Clock(moment), ui.Clean(text))
		return
	}
	fmt.Fprintf(out, "%s  %s\n", out.Dim(out.Clock(moment)), programText(out, text))
}
