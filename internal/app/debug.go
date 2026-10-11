package app

import (
	"fmt"
	"net/http"
	"path"
	"time"
)

// --debug: one line on stderr for each call to the API, with its name, how long its answer took
// to start and the HTTP status it started with. A request's headers hold the token and are never
// written.

// debugClient is the HTTP client the SDK calls through with --debug; nil without it, and the
// SDK then uses its own.
func (a *App) debugClient() *http.Client {
	if !a.Debug {
		return nil
	}
	return &http.Client{Transport: debugTransport{app: a}}
}

type debugTransport struct {
	app *App
}

func (t debugTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	started := time.Now()
	response, err := http.DefaultTransport.RoundTrip(request)

	outcome := "not reached"
	if err == nil {
		outcome = response.Status
	}
	t.app.Debugf("%s  %s  %s", path.Base(request.URL.Path), time.Since(started).Round(time.Millisecond), outcome)
	return response, err
}

// Debugf writes a line of --debug's to stderr. Never give it a token, a transfer's URL, a
// password or a value of /.env.
func (a *App) Debugf(format string, args ...any) {
	if a.Debug {
		a.UI.ErrLine(a.UI.Err.Dim("debug  " + fmt.Sprintf(format, args...)))
	}
}
