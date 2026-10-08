package reach

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	pethost "github.com/pethost-dev/sdk/go"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/target"
	"github.com/pethost-dev/cli/internal/transfer"
	"github.com/pethost-dev/cli/internal/ui"
)

// in copies one file from this computer into a container or a volume.
func (c *copying) in(ctx context.Context, from string, to place) error {
	body, size, err := c.open(from)
	if err != nil {
		return err
	}
	defer body.Close()

	project, err := target.ForChange(ctx, c.a)
	if err != nil {
		return err
	}
	client, err := c.a.Client()
	if err != nil {
		return err
	}
	if err := to.locate(ctx, c.a, client, project, c.flags); err != nil {
		return err
	}
	// A path that ends in a slash names the folder: the file keeps its own name there.
	if from != streamed && (to.path == "" || strings.HasSuffix(to.path, "/")) {
		to.path = path.Join("/", to.path, filepath.Base(from))
	}

	c.started = time.Now()
	ticket, err := askToUpload(ctx, client, project, to)
	if err != nil && from != streamed && isDirectory(ctx, client, project, to, err) {
		// As cp does with a folder for its destination: the file goes into it, under its own name.
		to.path = path.Join("/", to.path, filepath.Base(from))
		ticket, err = askToUpload(ctx, client, project, to)
	}
	if err != nil {
		return err
	}

	stop := c.follow(ui.Clean(path.Base(to.path)), size)
	response, err := transfer.Do(ctx, ticket, c.teed(body), size)
	stop()
	if err != nil {
		return err
	}
	response.Body.Close()
	c.a.Debugf("upload to the machine  %s  %s", time.Since(c.started).Round(time.Millisecond), response.Status)

	c.copied(localName(from), to.String())
	if ticket.Replaces {
		c.a.UI.Info("  It took the place of the file that was there.")
	}
	c.sayWhetherItLasts(ctx, client, project, to)
	return c.printTicket(ticket)
}

// open opens the local side of a copy in, and says how many bytes it holds; -1 for stdin,
// whose end nobody knows yet.
func (c *copying) open(from string) (body io.ReadCloser, size int64, err error) {
	if from == streamed {
		return io.NopCloser(c.a.UI.In), -1, nil
	}

	file, err := os.Open(from)
	if err != nil {
		return nil, 0, notReadable(from, err)
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, 0, notReadable(from, err)
	}
	if info.IsDir() {
		file.Close()
		return nil, 0, fail.Failed("%s is a folder, and pethost cp copies one file.", from).
			WithLead("Pack it first, then copy the archive:", "tar czf "+app.ShellWord(folderName(from)+".tgz")+" "+app.ShellWord(from))
	}
	return file, info.Size(), nil
}

// notReadable is a local file that cannot be copied, said without Go's own wording of it.
func notReadable(from string, err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return fail.Failed("There is no %s on this computer.", from)
	}
	return fail.Failed("%s cannot be read: %s.", from, fail.Innermost(err))
}

// folderName is a folder's own name, for the archive packed of it: "." is named after the
// folder it is.
func folderName(folder string) string {
	if absolute, err := filepath.Abs(folder); err == nil {
		folder = absolute
	}
	return filepath.Base(folder)
}

func askToUpload(ctx context.Context, client *pethost.Client, project target.Project, to place) (*pethost.CreateTransferResponse, error) {
	return client.CreateTransfer(ctx, &pethost.CreateTransferRequest{
		Transfer: pethost.CreateTransferUploadFile{ProjectID: project.ID, Root: to.uploadRoot(), Path: to.path},
	})
}

// isDirectory says whether the machine refused a copy in because its destination is a
// directory. The refusal has no typed reason, so the machine is asked what is at the path.
func isDirectory(ctx context.Context, client *pethost.Client, project target.Project, to place, refusal error) bool {
	if e, ok := errors.AsType[*pethost.Error](refusal); !ok || e.Code != pethost.CodeFailedPrecondition {
		return false
	}
	there, err := client.ReadPath(ctx, &pethost.ReadPathRequest{ProjectID: project.ID, Root: to.readRoot(), Path: to.path, EntryLimit: 1})
	return err == nil && there.Entry != nil && there.Entry.Type == pethost.FileTypeDirectory
}

func localName(from string) string {
	if from == streamed {
		return "stdin"
	}
	return from
}

// sayWhetherItLasts says, of a file copied into a container, where it lies: in a volume it
// stays, in the container's own files the next deploy loses it. The machine knows which.
func (c *copying) sayWhetherItLasts(ctx context.Context, client *pethost.Client, project target.Project, to place) {
	if to.root != inService {
		return
	}
	answer, err := client.ReadPath(ctx, &pethost.ReadPathRequest{
		ProjectID: project.ID, Root: to.readRoot(), Path: to.path, LengthBytes: 1,
	})
	if err != nil {
		return
	}
	if said := lasts(answer); said != "" {
		c.a.UI.Info("  It is %s.", said)
	}
}
