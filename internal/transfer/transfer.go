// Package transfer moves one file straight between this computer and the machine: the one HTTP
// request the API gives a URL for (CreateTransfer), streamed, never through the panel.
//
// The URL is a secret of that request: it is never printed, and no error of this package holds
// it.
package transfer

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	pethost "github.com/pethost-dev/sdk/go"

	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/ui"
)

// Do makes a transfer's request and returns the machine's answer once it is a yes: for a
// download its body is the file, which the caller reads and closes. body is what an upload
// sends, nil for a download, and length its size in bytes, -1 when it is not known.
func Do(ctx context.Context, transfer *pethost.CreateTransferResponse, body io.Reader, length int64) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, transfer.HTTPMethod, transfer.URL, body)
	if err != nil {
		return nil, fail.Failed("The machine gave an address the file cannot go by.")
	}
	if body != nil {
		request.ContentLength = length
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, notAnswered(ctx, err, body != nil)
	}
	if response.StatusCode != http.StatusOK {
		defer response.Body.Close()
		return nil, refused(response, body != nil)
	}
	return response, nil
}

// notAnswered is a request that got no answer, said without its URL.
func notAnswered(ctx context.Context, err error, upload bool) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if upload {
		return fail.Failed("The upload did not get through to your machine: %s. Nothing of it is kept there.", Cause(err))
	}
	return fail.Failed("The download did not come from your machine: %s.", Cause(err))
}

// Cause is what went wrong in a request, without the request's URL, which Go's error holds.
func Cause(err error) error {
	if wrapped, ok := errors.AsType[*url.Error](err); ok {
		return wrapped.Err
	}
	return err
}

// refusalBytes bounds how much of the machine's answer to a refused transfer is read.
const refusalBytes = 4 << 10

// refused is the machine's refusal of a transfer: its answer is plain text for people, a
// reason a line.
func refused(response *http.Response, upload bool) error {
	answer, _ := io.ReadAll(io.LimitReader(response.Body, refusalBytes))
	reason := strings.TrimSpace(ui.Clean(string(answer)))
	if reason == "" {
		reason = response.Status
	}
	if upload {
		return fail.Failed("Your machine did not take the upload: %s", reason)
	}
	return fail.Failed("Your machine refused the download: %s", reason)
}
