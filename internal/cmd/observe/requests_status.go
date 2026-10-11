package observe

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	pethost "github.com/pethost-dev/sdk/go"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/fail"
)

// --status takes a class of statuses, "5xx", which is the API's own filter, or one status,
// "404", which the API has no filter for: its class is asked for, and the requests with that
// status are picked from the answer here.
//
// ponytail: one status is looked for among the newest exactPages pages of its class, and the
// totals and busiest paths cannot be had for it. When the API filters by status, pass the
// status to it and delete the picking.

// A statusFilter is which statuses `pethost requests` shows.
type statusFilter struct {
	// The API's status_class, 1 to 5; 0 = any.
	class uint32
	// The one status picked from the class; 0 = the whole class.
	code uint32
}

// statusOf reads the flag's value for cmd: "5xx" or "404". The machine judges the class.
func statusOf(cmd *cobra.Command, value string) (statusFilter, error) {
	if value == "" {
		return statusFilter{}, nil
	}

	lower := strings.ToLower(value)
	if len(lower) == 3 && lower[0] >= '1' && lower[0] <= '9' {
		class := uint32(lower[0] - '0')
		if lower[1:] == "xx" {
			return statusFilter{class: class}, nil
		}
		if code, err := strconv.ParseUint(lower, 10, 32); err == nil {
			return statusFilter{class: class, code: uint32(code)}, nil
		}
	}

	message := fmt.Sprintf("%s: --status %q is neither a class of statuses such as 5xx nor one status such as 404", cmd.CommandPath(), value)
	return statusFilter{}, fail.Usage(message, cmd.UseLine())
}

// exact says whether one status alone was asked for.
func (f statusFilter) exact() bool {
	return f.code != 0
}

// takes says whether a request is one to show.
func (f statusFilter) takes(request pethost.HTTPRequest) bool {
	return f.code == 0 || request.StatusCode == f.code
}

// words name the filter in a sentence: "5xx", "404".
func (f statusFilter) words() string {
	if f.exact() {
		return fmt.Sprint(f.code)
	}
	return fmt.Sprintf("%dxx", f.class)
}

// exactPageSize is how many requests of a class one page brings to pick a status from: the
// most the API gives. exactPages is how many pages are read for it at most.
const (
	exactPageSize = 200
	exactPages    = 5
)

// pageSize is how many requests to ask for at once when want are to be shown.
func (f statusFilter) pageSize(want int) uint32 {
	if f.exact() {
		return exactPageSize
	}
	return uint32(want)
}

// pick leaves in answer the newest want requests with the one status asked for, reading older
// pages of the class while it has fewer. It returns how many requests of the class it looked
// at, and whether those were all the range holds.
func (f statusFilter) pick(ctx context.Context, client *pethost.Client, request *pethost.QueryHTTPTrafficRequest, answer *pethost.QueryHTTPTrafficResponse, want int) (looked int, all bool, err error) {
	if !f.exact() {
		return len(answer.Requests), answer.NextPageToken == "", nil
	}

	looked = len(answer.Requests)
	picked := f.matching(answer.Requests)
	token := answer.NextPageToken
	for page := 1; len(picked) < want && token != "" && page < exactPages; page++ {
		request.PageToken = token
		older, err := client.QueryHTTPTraffic(ctx, request)
		if err != nil {
			return looked, false, err
		}
		looked += len(older.Requests)
		picked = append(picked, f.matching(older.Requests)...)
		token = older.NextPageToken
	}

	// The token was of the class's pages, not of what is left here.
	answer.Requests, answer.NextPageToken = picked[:min(len(picked), want)], ""
	return looked, token == "", nil
}

func (f statusFilter) matching(requests []pethost.HTTPRequest) []pethost.HTTPRequest {
	var matching []pethost.HTTPRequest
	for _, request := range requests {
		if f.takes(request) {
			matching = append(matching, request)
		}
	}
	return matching
}
