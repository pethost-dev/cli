package observe

import (
	"errors"
	"iter"

	pethost "github.com/pethost-dev/sdk/go"
)

// follow prints each new line as the containers write it, from the line after cursor on, until
// the person leaves. A stream that breaks goes on from the last line it brought.
func (l *logs) follow(cursor string) error {
	return follow(l.ctx, l.a,
		func() iter.Seq2[*pethost.TailContainerLogsResponse, error] { return l.tail(&cursor) },
		func(response *pethost.TailContainerLogsResponse) error {
			if response.Cursor != "" {
				cursor = response.Cursor
			}
			return l.showNew(response)
		})
}

func (l *logs) showNew(response *pethost.TailContainerLogsResponse) error {
	if l.a.JSON {
		return l.a.PrintJSON(response)
	}
	if response.Line != nil {
		l.printer.line(*response.Line)
	}
	return nil
}

// tail is one stream of the lines after cursor. When the machine no longer keeps the cursor's
// line, the stream ends and cursor is the newest line's: the next one goes on from there.
func (l *logs) tail(cursor *string) iter.Seq2[*pethost.TailContainerLogsResponse, error] {
	request := &pethost.TailContainerLogsRequest{ProjectID: l.project.ID, Filter: l.filter(), AfterCursor: *cursor}

	return func(yield func(*pethost.TailContainerLogsResponse, error) bool) {
		for response, err := range l.client.TailContainerLogs(l.ctx, request) {
			if isOutOfRange(err) {
				newest, err := l.startOver()
				if err != nil {
					yield(nil, err)
					return
				}
				*cursor = newest
				return
			}
			if !yield(response, err) {
				return
			}
		}
	}
}

// startOver finds where the log ends now, for a stream that fell behind what the machine keeps,
// and says that lines are missing from the screen.
func (l *logs) startOver() (cursor string, err error) {
	newest, err := l.client.QueryContainerLogs(l.ctx, l.request(1, false))
	if err != nil {
		return "", err
	}
	l.a.UI.Info("The machine no longer keeps the lines after the last one shown: some are missing here. Going on from the newest.")
	return newest.TailCursor, nil
}

func isOutOfRange(err error) bool {
	api, ok := errors.AsType[*pethost.Error](err)
	return ok && api.Code == pethost.CodeOutOfRange
}
