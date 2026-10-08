package observe

import (
	"slices"

	pethost "github.com/pethost-dev/sdk/go"
)

// The lines that are already there. One answer of the API holds at most pageLines lines and
// about 64 KiB, so both readings ask page by page.

// pageLines is the most lines the API gives in one answer.
const pageLines = 500

// request is the query every page of a reading is asked with: only its page token changes.
func (l *logs) request(limit int, fromStart bool) *pethost.QueryContainerLogsRequest {
	return &pethost.QueryContainerLogsRequest{
		ProjectID:   l.project.ID,
		Filter:      l.filter(),
		StartTime:   l.since.start,
		LastSeconds: l.since.lastSeconds,
		Limit:       uint32(limit),
		FromStart:   fromStart,
	}
}

// everyLine prints every line of the range, oldest first, each page as it comes: what --since
// alone asks for.
func (l *logs) everyLine() (shown int, cursor string, err error) {
	request := l.request(pageLines, true)
	for {
		page, err := l.client.QueryContainerLogs(l.ctx, request)
		if err != nil {
			return shown, "", err
		}
		if err := l.show(page); err != nil {
			return shown, "", err
		}

		shown += len(page.Lines)
		if page.NextPageToken == "" {
			return shown, page.TailCursor, nil
		}
		request.PageToken = page.NextPageToken
	}
}

// newestLines prints the newest count lines, oldest first. The API pages from the newest line
// back, so the pages are read first and printed in turn from the oldest one.
func (l *logs) newestLines(count int) (shown int, cursor string, err error) {
	var pages []*pethost.QueryContainerLogsResponse
	request := l.request(min(count, pageLines), false)
	for shown < count {
		page, err := l.client.QueryContainerLogs(l.ctx, request)
		if err != nil {
			return 0, "", err
		}
		pages = append(pages, page)
		shown += len(page.Lines)
		if page.NextPageToken == "" {
			break
		}
		request.PageToken = page.NextPageToken
	}

	// The oldest page read may hold more than was asked for: its oldest lines are left out.
	oldest := pages[len(pages)-1]
	oldest.Lines = oldest.Lines[max(0, shown-count):]
	shown = min(shown, count)

	for _, page := range slices.Backward(pages) {
		if err := l.show(page); err != nil {
			return shown, "", err
		}
	}
	// The newest page is the one a stream goes on from.
	return shown, pages[0].TailCursor, nil
}
