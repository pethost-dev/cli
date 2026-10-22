package observe

import (
	"time"

	pethost "github.com/pethost-dev/sdk/go"

	"github.com/pethost-dev/cli/internal/app"
)

// What stands near a problem or a failing path: the lines its service wrote at that moment,
// and for a path its numbers of the day. Every question here depends only on the first three
// answers, so all are asked at once.

// A problemSeen is one of the project's problems with its service's last words.
type problemSeen struct {
	problem pethost.ProjectProblem
	lines   []pethost.LogLine
}

// A pathSeen is a path whose requests failed.
type pathSeen struct {
	path pethost.HTTPPathTraffic
	// Every request to it in the day, failed or not.
	all *pethost.HTTPTrafficSummary
	// Its newest failed request; nil = it is not among the latest failures asked for.
	last *pethost.HTTPRequest
	// What the service that answered that request wrote while it did.
	lines []pethost.LogLine
}

const (
	// shownPaths is how many failing paths get lines of their own, the most failing first.
	shownPaths = 3
	// problemLineCount and requestLineCount are how many log lines stand under a problem and
	// under a failing path: the screen stays one screen.
	problemLineCount = 5
	requestLineCount = 6
	// nearRequest is how far around a request a log line still counts as written for it: the
	// proxy's clock for the request and Docker's for the line are read a moment apart.
	nearRequest = 250 * time.Millisecond
)

// lookNear asks for what stands near each problem and each of the paths that fail most.
func (w *why) lookNear() error {
	project := w.answer.Project
	failing := w.failed.TopPaths[:min(len(w.failed.TopPaths), shownPaths)]
	w.problems = make([]problemSeen, len(project.Problems))
	w.paths = make([]pathSeen, len(failing))

	// A service's last lines stand under the first problem that names it.
	var calls []func() error
	asked := map[string]bool{}
	for i, problem := range project.Problems {
		seen := &w.problems[i]
		seen.problem = problem
		if problem.Service == "" || asked[problem.Service] {
			continue
		}
		asked[problem.Service] = true
		calls = append(calls, func() (err error) {
			seen.lines, err = w.lastLines(problem.Service)
			return err
		})
	}
	for i, path := range failing {
		seen := &w.paths[i]
		seen.path = path
		seen.last = w.lastFailure(path)
		calls = append(calls,
			func() (err error) {
				seen.all, err = w.pathTotals(path)
				return err
			},
			func() (err error) {
				seen.lines, err = w.requestLines(seen.last)
				return err
			},
		)
	}
	return app.Together(calls...)
}

// lastLines are the last lines a service with a problem wrote.
func (w *why) lastLines(service string) ([]pethost.LogLine, error) {
	return w.lines(&pethost.QueryContainerLogsRequest{
		ProjectID: w.project.ID,
		Filter:    &pethost.ContainerLogFilter{Service: service},
		EndTime:   w.restartedAt(service),
		Limit:     problemLineCount,
	})
}

// restartedAt is when the run that goes on began, for a service that Docker restarted and that
// runs again: the machine gives it as that problem's moment, and the service's last words were
// written before it. Zero = the newest lines are its last words.
func (w *why) restartedAt(name string) time.Time {
	project := w.answer.Project
	for _, service := range project.Services {
		if service.Service == name && !service.FinishTime.IsZero() {
			return time.Time{}
		}
	}
	for _, problem := range project.Problems {
		if problem.Service == name && problem.Kind == pethost.ProjectProblemKindServiceRestarting {
			return problem.SinceTime
		}
	}
	return time.Time{}
}

// lastFailure is the path's newest failed request among the latest failures.
func (w *why) lastFailure(path pethost.HTTPPathTraffic) *pethost.HTTPRequest {
	for _, request := range w.failed.Requests {
		if request.Method == path.Method && request.PathPattern == path.PathPattern {
			return &request
		}
	}
	return nil
}

// pathTotals counts every request to the path in the day the first answers are of.
func (w *why) pathTotals(path pethost.HTTPPathTraffic) (*pethost.HTTPTrafficSummary, error) {
	answer, err := w.client.QueryHTTPTraffic(w.ctx, &pethost.QueryHTTPTrafficRequest{
		ProjectID: w.project.ID,
		Filter:    &pethost.HTTPTrafficFilter{Method: path.Method, PathPattern: path.PathPattern},
		StartTime: w.day.StartTime,
		EndTime:   w.day.EndTime,
	})
	if err != nil {
		return nil, err
	}
	return answer.Summary, nil
}

// requestLines are what the service that answered a request wrote from the request's start to
// its end, and a moment around both.
func (w *why) requestLines(request *pethost.HTTPRequest) ([]pethost.LogLine, error) {
	if request == nil || request.Service == "" {
		return nil, nil
	}
	took := time.Duration(request.DurationMs) * time.Millisecond
	return w.lines(&pethost.QueryContainerLogsRequest{
		ProjectID: w.project.ID,
		Filter:    &pethost.ContainerLogFilter{Service: request.Service},
		StartTime: request.FinishTime.Add(-took - nearRequest),
		EndTime:   request.FinishTime.Add(nearRequest),
		Limit:     requestLineCount,
	})
}

// lines asks for log lines; a moment the machine no longer keeps output of has none.
func (w *why) lines(request *pethost.QueryContainerLogsRequest) ([]pethost.LogLine, error) {
	answer, err := w.client.QueryContainerLogs(w.ctx, request)
	if isOutOfRange(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return answer.Lines, nil
}
