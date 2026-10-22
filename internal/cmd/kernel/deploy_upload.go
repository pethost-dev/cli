package kernel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	pethost "github.com/pethost-dev/sdk/go"

	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/transfer"
	"github.com/pethost-dev/cli/internal/ui"
)

// The folder goes straight to the machine: the API gives a URL for one request
// (internal/transfer), and the archive is written into that request as it is packed.

// send packs the folder and uploads it, and returns the upload's id for the deploy.
func (d *deploy) send(ctx context.Context) (string, error) {
	transfer, err := d.transferToUse(ctx)
	if err != nil {
		return "", err
	}
	if err := d.upload(ctx, transfer); err != nil {
		return "", err
	}

	if d.create {
		d.a.UI.Done("%s", strings.TrimSpace("Packed "+d.packed.packedWords()+"   "+d.packed.packedAside()))
		d.a.UI.Done("Uploaded straight to your machine")
	}
	return transfer.UploadID, nil
}

// transferToUse is a transfer whose request can still start: the one the folder was looked at
// with, unless an upload has used it or a question outlasted it.
func (d *deploy) transferToUse(ctx context.Context) (*pethost.CreateTransferResponse, error) {
	transfer := d.transfer
	d.transfer = nil
	if transfer != nil && time.Until(transfer.ExpireTime) > startMargin {
		return transfer, nil
	}
	return d.newTransfer(ctx)
}

// startMargin is how long before a transfer's end its request is still started: the request
// has to reach the machine in that time.
const startMargin = 15 * time.Second

func (d *deploy) newTransfer(ctx context.Context) (*pethost.CreateTransferResponse, error) {
	return d.client.CreateTransfer(ctx, &pethost.CreateTransferRequest{
		Transfer: pethost.CreateTransferUploadArchive{FileName: filepath.Base(d.dir) + ".tgz"},
	})
}

// upload sends the archive in one request, with a line that says how far it is.
func (d *deploy) upload(ctx context.Context, ticket *pethost.CreateTransferResponse) error {
	live := d.a.UI.Live("Packing and uploading…")
	defer live.Stop()

	var sent atomic.Uint64
	stop := live.Follow(func() string {
		return fmt.Sprintf("Packing and uploading… %s of %s", ui.Size(sent.Load()), ui.Size(d.packed.bytes))
	})
	defer stop()

	started := time.Now()
	body, packing := io.Pipe()
	packed := make(chan error, 1)
	go func() {
		err := pack(packing, d.dir, d.packed, &sent)
		packing.CloseWithError(err)
		packed <- err
	}()

	response, err := transfer.Do(ctx, ticket, body, -1)
	// The request is over, however it ended: a packing that still writes ends with it.
	body.Close()
	packErr := <-packed
	if packErr != nil && !errors.Is(packErr, io.ErrClosedPipe) {
		return notPacked(response, packErr)
	}
	if err != nil {
		return err
	}
	response.Body.Close()

	d.a.Debugf("upload to the machine  %s  %s", time.Since(started).Round(time.Millisecond), response.Status)
	return nil
}

// notPacked is a folder this computer could not read to its end. It is said in place of
// whatever the machine made of half an archive: the cause is here.
func notPacked(response *http.Response, cause error) error {
	if response != nil {
		response.Body.Close()
	}
	return fail.Failed("This folder could not be packed: %s. Nothing was deployed.", cause)
}
