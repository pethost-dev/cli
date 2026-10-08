package observe

import (
	"fmt"
	"strings"

	pethost "github.com/pethost-dev/sdk/go"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/say"
	"github.com/pethost-dev/cli/internal/ui"
)

// The screen of `pethost why`: each problem in the machine's words with its service's last
// lines, each failing path with what its service wrote at that moment, the slowest of the
// busiest paths, and the project's last line. Each line says what was seen, never why.

func (w *why) print() {
	out := w.a.UI.Out
	if w.calm() {
		fmt.Fprintln(out, out.Check()+w.calmLine())
	}
	for _, seen := range w.problems {
		w.printProblem(seen)
	}
	for _, seen := range w.paths {
		w.printPath(seen)
	}
	if more := len(w.failed.TopPaths) - len(w.paths); more > 0 {
		fmt.Fprintf(out, "  More paths failed:  %s\n", out.Bold("pethost requests --status 5xx"+w.project.Flag()))
	}
	if slowest := w.slowestLine(); slowest != "" {
		fmt.Fprintln(out, out.Tilde()+slowest)
	}
	fmt.Fprintln(out, say.ProjectNow(out, w.answer.Project))

	if next := w.next(); len(next) > 0 {
		w.a.UI.Next(next...)
	}
}

// calm says whether nothing wrong was seen: the machine lists no problem, and no request of
// the day failed.
func (w *why) calm() bool {
	return len(w.answer.Project.Problems) == 0 && requestCount(w.failed.Summary) == 0
}

func requestCount(summary *pethost.HTTPTrafficSummary) uint64 {
	if summary == nil {
		return 0
	}
	return summary.RequestCount
}

// calmLine is the one line of a project nothing wrong was seen in: what was looked at, and
// no more than that, since a site may be slow or wrong in ways the machine does not count.
func (w *why) calmLine() string {
	during := w.during()
	switch count := requestCount(w.day.Summary); count {
	case 0:
		return fmt.Sprintf("The machine reports no problem, and no request came %s.", during)
	case 1:
		return fmt.Sprintf("The machine reports no problem, and the one request %s did not fail.", during)
	default:
		return fmt.Sprintf("The machine reports no problem, and none of %s failed %s.", ui.Plural(count, "request"), during)
	}
}

// during is the range the requests were counted in: "in the last 24 hours".
func (w *why) during() string {
	return "in " + lastWords(w.day.EndTime.Sub(w.day.StartTime))
}

// printProblem writes a problem as the machine words it, and under it what its service wrote
// last, or where the rest is to be read.
func (w *why) printProblem(seen problemSeen) {
	out := w.a.UI.Out
	problem := seen.problem
	fmt.Fprintln(out, out.Cross()+say.Problem(out, problem))

	for _, line := range w.checkOutput(problem) {
		fmt.Fprintln(out, nearLine(out, problem.Service+" healthcheck", "", line))
	}
	for _, line := range fold(seen.lines) {
		fmt.Fprintln(out, nearLine(out, line.Service, "", line.Text)+line.times(out))
	}
	if isDeployProblem(problem.Kind) {
		fmt.Fprintf(out, "    Its log:  %s\n", out.Bold("pethost logs --deploy"+w.project.Flag()))
	}
}

// checkLineCount is how many lines of a failed healthcheck's output are shown, its last.
const checkLineCount = 3

// checkOutput is what the latest failed healthcheck of an unhealthy service printed.
func (w *why) checkOutput(problem pethost.ProjectProblem) []string {
	if problem.Kind != pethost.ProjectProblemKindServiceUnhealthy {
		return nil
	}
	for _, service := range w.answer.Project.Services {
		if service.Service == problem.Service && service.HealthCheck != nil {
			lines := strings.Split(strings.TrimSpace(service.HealthCheck.LastFailureOutput), "\n")
			if lines[0] == "" {
				return nil
			}
			return lines[max(0, len(lines)-checkLineCount):]
		}
	}
	return nil
}

func isDeployProblem(kind pethost.ProjectProblemKind) bool {
	return kind == pethost.ProjectProblemKindDeployFailed || kind == pethost.ProjectProblemKindDeployCancelled
}

// printPath writes a failing path: "GET /api/search failed 41 of 44 times in the last 24
// hours, last at 14:02:11", and under it what the service that answered wrote at that moment.
func (w *why) printPath(seen pathSeen) {
	out := w.a.UI.Out
	line := pathName(seen.path.Method, seen.path.PathPattern) + " " + failedTimes(seen) + " " + w.during()
	if seen.last != nil {
		line += ", last at " + out.Clock(seen.last.FinishTime)
	}
	fmt.Fprintln(out, out.Cross()+line)

	if seen.last == nil || seen.last.Service == "" {
		return
	}
	if len(seen.lines) == 0 {
		fmt.Fprintln(out, "    "+out.Dim(ui.Clean(seen.last.Service)+" wrote nothing at that moment"))
		return
	}
	shown := ""
	for _, line := range fold(seen.lines) {
		clock := out.Clock(line.Time)
		if clock == shown {
			clock = spaces(len(clock))
		} else {
			shown = clock
		}
		fmt.Fprintln(out, nearLine(out, line.Service, clock, line.Text)+line.times(out))
	}
}

// A foldedLine is a log line that came count times in a row: a service that fails over and
// over writes the same words each time, and the screen says them once.
type foldedLine struct {
	pethost.LogLine
	count int
}

func fold(lines []pethost.LogLine) []foldedLine {
	var folded []foldedLine
	for _, line := range lines {
		if last := len(folded) - 1; last >= 0 && folded[last].Text == line.Text && folded[last].Service == line.Service {
			folded[last].count++
			continue
		}
		folded = append(folded, foldedLine{LogLine: line, count: 1})
	}
	return folded
}

// times says how often a folded line came, after it: " (5 times)"; nothing for once.
func (l foldedLine) times(out *ui.Stream) string {
	if l.count == 1 {
		return ""
	}
	return out.Dim(fmt.Sprintf(" (%d times)", l.count))
}

// failedTimes says how often a path failed, of how often it was asked for.
func failedTimes(seen pathSeen) string {
	if seen.all == nil {
		return "failed " + ui.Plural(requestCount(seen.path.Summary), "time")
	}
	if seen.all.RequestCount == 1 {
		return "failed the only time it was asked for"
	}
	return fmt.Sprintf("failed %s of %s", ui.Count(seen.all.ServerErrorCount), ui.Plural(seen.all.RequestCount, "time"))
}

// nearLine is a line a service wrote, under what it stands near: "    web | 14:02:11 TypeError…".
// clock is empty where the moment is said above.
func nearLine(out *ui.Stream, service, clock, text string) string {
	label := ui.Clean(service) + " | "
	if clock != "" {
		label += clock + " "
	}
	return "    " + out.Dim(label) + programText(out, text)
}

// slowestLine says which of the busiest paths answers slowest: "Of the 10 busiest paths the
// slowest is GET /recipes/*: 95% answer within 2.3 s (840 requests)". It compares, and judges
// nothing: whether that is slow is the reader's to say.
func (w *why) slowestLine() string {
	var slowest *pethost.HTTPPathTraffic
	for _, path := range w.day.TopPaths {
		if path.Summary != nil && (slowest == nil || path.Summary.LatencyP95Ms > slowest.Summary.LatencyP95Ms) {
			slowest = &path
		}
	}
	if slowest == nil {
		return ""
	}

	name := pathName(slowest.Method, slowest.PathPattern)
	numbers := fmt.Sprintf("95%% answer within %s (%s)", ui.Millis(slowest.Summary.LatencyP95Ms), ui.Plural(slowest.Summary.RequestCount, "request"))
	if len(w.day.TopPaths) == 1 {
		return fmt.Sprintf("The only path asked for is %s: %s", name, numbers)
	}
	return fmt.Sprintf("Of the %d busiest paths the slowest is %s: %s", len(w.day.TopPaths), name, numbers)
}

// next are the commands that show more of what the screen named, the likeliest first.
func (w *why) next() []string {
	flag := w.project.Flag()
	var next []string
	for _, problem := range w.answer.Project.Problems {
		// A failed deploy's own log is named under it; its service may never have run.
		if problem.Service != "" && !isDeployProblem(problem.Kind) {
			next = append(next, "pethost logs "+app.ShellWord(problem.Service)+flag)
			break
		}
	}
	if len(w.paths) > 0 {
		next = append(next, "pethost requests --status 5xx"+flag)
	}
	return next
}
