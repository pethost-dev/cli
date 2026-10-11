package observe

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	pethost "github.com/pethost-dev/sdk/go"

	"github.com/pethost-dev/cli/internal/ui"
)

// The screen of `pethost requests`: the range's totals in a line, the busiest paths, and the
// latest requests, the newest last, where -f goes on.

// printTotals writes the range's totals: "1.9k requests in the last 24 hours · 41 failed · half
// answer within 38 ms, 95% within 2.3 s".
func (r *requests) printTotals(answer *pethost.QueryHTTPTrafficResponse) {
	out := r.a.UI.Out
	during := r.since.words(out, answer.EndTime.Sub(answer.StartTime))
	summary := answer.Summary
	if summary == nil || summary.RequestCount == 0 {
		fmt.Fprintf(out, "No requests%s %s.\n", r.filterWords(), during)
		return
	}

	parts := []string{out.Bold(ui.Plural(summary.RequestCount, "request")) + r.filterWords() + " " + during}
	if r.status.class == 0 {
		parts = append(parts, failedWords(out, summary))
	}
	parts = append(parts, "half answer within "+ui.Millis(summary.LatencyP50Ms)+", 95% within "+ui.Millis(summary.LatencyP95Ms))
	fmt.Fprintln(out, strings.Join(parts, " · "))
}

// filterWords say which requests were asked for, after "requests": " that answered 5xx, with
// /api in the path"; nothing when all were.
func (r *requests) filterWords() string {
	var words []string
	if r.status.class != 0 {
		words = append(words, "that answered "+r.status.words())
	}
	if r.flags.path != "" {
		words = append(words, "with "+ui.Clean(r.flags.path)+" in the path")
	}
	if len(words) == 0 {
		return ""
	}
	return " " + strings.Join(words, ", ")
}

// failedWords say how many requests failed: answered 5xx, as the machine counts them.
func failedWords(out *ui.Stream, summary *pethost.HTTPTrafficSummary) string {
	if summary.ServerErrorCount == 0 {
		return "none failed"
	}
	return out.Red(ui.Count(summary.ServerErrorCount) + " failed")
}

// printPaths writes the busiest paths, busiest first, a row each.
func (r *requests) printPaths(paths []pethost.HTTPPathTraffic) {
	if len(paths) == 0 {
		return
	}
	out := r.a.UI.Out
	fmt.Fprintln(out, out.Bold("Busiest paths"))

	table := out.Table()
	for _, path := range paths {
		table.Row(r.pathCells(out, path)...)
	}
	table.Flush()
}

func (r *requests) pathCells(out *ui.Stream, path pethost.HTTPPathTraffic) []string {
	name := pathName(path.Method, path.PathPattern)
	if out.Terminal {
		name = "  " + clip(name, longestPath)
	}
	summary := path.Summary
	if summary == nil {
		return []string{name}
	}

	cells := []string{name, ui.Plural(summary.RequestCount, "request")}
	if r.status.class == 0 {
		failed := ""
		if summary.ServerErrorCount > 0 {
			failed = out.Red(ui.Count(summary.ServerErrorCount) + " failed")
		}
		cells = append(cells, failed)
	}
	return append(cells, "half within "+ui.Millis(summary.LatencyP50Ms), "95% within "+ui.Millis(summary.LatencyP95Ms))
}

// pathName is a method and a path as one name: "GET /recipes/*". Both come from a request
// anyone may send.
func pathName(method, path string) string {
	return ui.Clean(method) + " " + ui.Clean(path)
}

// printLatest writes the latest requests, the newest last; titled when the totals stand above
// them. With none to write it says so: looked and all say how far a search for one status went.
func (r *requests) printLatest(answer *pethost.QueryHTTPTrafficResponse, titled bool, looked int, all bool) {
	out := r.a.UI.Out
	if len(answer.Requests) == 0 {
		if !titled {
			r.sayNone(answer, looked, all)
		}
		return
	}
	if titled {
		fmt.Fprintln(out, out.Bold("Latest requests"))
	}

	latest := slices.Clone(answer.Requests)
	slices.Reverse(latest)
	r.printer.page(latest)
}

// sayNone says that there is no request to list, on stderr. Under the totals, which said so
// already, the caller skips it.
func (r *requests) sayNone(answer *pethost.QueryHTTPTrafficResponse, looked int, all bool) {
	said := "No requests" + r.filterWords() + " " + r.since.words(r.a.UI.Err, answer.EndTime.Sub(answer.StartTime))
	if r.status.exact() && !all {
		said = fmt.Sprintf("None of the newest %d requests that answered %dxx answered %d", looked, r.status.class, r.status.code)
	}
	if r.flags.follow {
		said += ": new ones show here. Ctrl-C leaves"
	}
	r.a.UI.Info("%s.", said)
}

// A requestPrinter writes requests, one a line: "14:02:11  500  GET /api/search?q=soup   38 ms
// web  203.0.113.7". It keeps what makes the lines of a terminal stand in columns: the widest
// path and service so far.
type requestPrinter struct {
	out     *ui.Stream
	path    int
	service int
}

// The path's column on a terminal is as wide as the page's widest path, within usualPath and
// widePath: a longer path moves its own line and no other. A path is anyone's to send, and one
// of 4 KiB would fill the screen: it is shown up to longestPath.
const (
	usualPath   = 28
	widePath    = 44
	longestPath = 100
)

// durationWidth is the width of "999 ms".
const durationWidth = 6

// page writes requests that came together: the columns are as wide as their widest cells before
// the first of them is written.
func (p *requestPrinter) page(requests []pethost.HTTPRequest) {
	for _, request := range requests {
		p.fit(request)
	}
	for _, request := range requests {
		p.row(request)
	}
}

func (p *requestPrinter) fit(request pethost.HTTPRequest) {
	p.path = max(p.path, usualPath, min(widePath, ui.Width(pathName(request.Method, request.Path))))
	p.service = max(p.service, ui.Width(ui.Clean(request.Service)))
}

// row writes one request.
func (p *requestPrinter) row(request pethost.HTTPRequest) {
	out := p.out
	name, took, service := pathName(request.Method, request.Path), ui.Millis(request.DurationMs), ui.Clean(request.Service)
	if !out.Terminal {
		cells := []string{out.Clock(request.FinishTime), strconv.FormatUint(uint64(request.StatusCode), 10), name, took, service, ui.Clean(request.ClientIPAddress)}
		fmt.Fprintln(out, strings.Join(cells, "\t"))
		return
	}

	// A request that came alone keeps the columns as they are.
	p.path = max(p.path, usualPath)
	name = clip(name, longestPath)
	fmt.Fprintf(out, "%s  %s  %s  %s  %s  %s\n",
		out.Dim(out.Clock(request.FinishTime)),
		statusCell(out, request.StatusCode),
		name+spaces(p.path-ui.Width(name)),
		spaces(durationWidth-ui.Width(took))+took,
		service+spaces(p.service-ui.Width(service)),
		out.Dim(ui.Clean(request.ClientIPAddress)))
}

// statusCell is a status in its colour: a failure red, a refusal yellow, a redirect dim. The
// API logs a WebSocket, which has no status, as 0.
func statusCell(out *ui.Stream, status uint32) string {
	text := strconv.FormatUint(uint64(status), 10)
	switch {
	case status == 0:
		return out.Cyan(" ws")
	case status >= 500:
		return out.Red(text)
	case status >= 400:
		return out.Yellow(text)
	case status >= 300:
		return out.Dim(text)
	}
	return out.Green(text)
}

func spaces(count int) string {
	return strings.Repeat(" ", max(0, count))
}

// clip is text cut to width characters, the last of them "…" when it was longer.
func clip(text string, width int) string {
	runes := []rune(text)
	if len(runes) <= width {
		return text
	}
	return string(runes[:width-1]) + "…"
}
