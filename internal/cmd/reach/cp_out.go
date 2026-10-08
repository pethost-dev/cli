package reach

import (
	"context"
	"errors"
	"io"
	"os"
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

// out copies a file, or a directory as a tar archive, from the machine to this computer.
func (c *copying) out(ctx context.Context, from place, to string) error {
	if to == streamed && c.a.UI.Out.Terminal {
		return fail.Failed("A file copied to - is written to stdout, which is a terminal here.").
			WithLead("Name a file, or send it on:", c.a.CommandLine()+" > FILE")
	}

	project, err := target.ForRead(ctx, c.a)
	if err != nil {
		return err
	}
	client, err := c.a.Client()
	if err != nil {
		return err
	}
	if err := from.locate(ctx, c.a, client, project, c.flags); err != nil {
		return err
	}

	c.started = time.Now()
	ticket, directory, err := askFor(ctx, client, project, from)
	if err != nil {
		return err
	}
	response, err := transfer.Do(ctx, ticket, nil, 0)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	c.a.Debugf("download from the machine  %s  %s", time.Since(c.started).Round(time.Millisecond), response.Status)

	stop := c.follow(ui.Clean(savedName(ticket)), response.ContentLength)
	saved, err := c.save(response.Body, to, savedName(ticket))
	stop()
	if err != nil {
		return err
	}

	c.copied(from.String(), saved)
	if directory {
		c.a.UI.Info("  A directory comes as a tar archive.%s", unpackWith(saved))
	}
	if to == streamed {
		// stdout holds the file alone: --json's answer after it would be read as its last bytes.
		return nil
	}
	return c.printTicket(ticket)
}

// askFor asks the machine, at once, for the download and for what the path is: a directory
// comes as an archive, and only the second answer says that it is one.
func askFor(ctx context.Context, client *pethost.Client, project target.Project, from place) (ticket *pethost.CreateTransferResponse, directory bool, err error) {
	var kind *pethost.ReadPathResponse
	err = app.Together(
		func() (err error) {
			ticket, err = client.CreateTransfer(ctx, &pethost.CreateTransferRequest{
				Transfer: pethost.CreateTransferDownload{ProjectID: project.ID, Root: from.downloadRoot(), Path: from.path},
			})
			return err
		},
		func() error {
			// Its failure is the download's too, which says it; alone it only leaves the kind unknown.
			kind, _ = client.ReadPath(ctx, &pethost.ReadPathRequest{
				ProjectID: project.ID, Root: from.readRoot(), Path: from.path, EntryLimit: 1, LengthBytes: 1,
			})
			return nil
		},
	)
	if err != nil {
		return nil, false, err
	}
	directory = kind != nil && kind.Entry != nil && kind.Entry.Type == pethost.FileTypeDirectory
	return ticket, directory, nil
}

// savedName is the name the machine gives the download, as one name in a folder: whatever it
// holds of a path is dropped, since the name decides where a file is written here.
func savedName(ticket *pethost.CreateTransferResponse) string {
	name := filepath.Base(filepath.FromSlash(ticket.FileName))
	if name == "." || name == ".." || name == string(filepath.Separator) {
		return "download"
	}
	return name
}

// save writes the download to its place here and returns where that is: to itself, or under
// the machine's name for it when to is a folder. The bytes go to a file beside it first, so a
// copy that breaks leaves the file that was there as it was.
func (c *copying) save(body io.Reader, to, name string) (saved string, err error) {
	if to == streamed {
		_, err := io.Copy(c.a.UI.Out, c.teed(body))
		return "stdout", c.broken(err)
	}

	saved = to
	if info, err := os.Stat(to); (err == nil && info.IsDir()) || endsInSeparator(to) {
		saved = filepath.Join(to, name)
	}
	part := saved + ".part"
	file, err := os.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return "", fail.Failed("%s cannot be written here: %s.", saved, fail.Innermost(err))
	}

	_, err = io.Copy(file, c.teed(body))
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(part, saved)
	}
	if err != nil {
		os.Remove(part)
		return "", c.broken(err)
	}
	return saved, nil
}

// broken is a copy that stopped midway, said without the transfer's URL.
func (c *copying) broken(err error) error {
	if err == nil || errors.Is(err, context.Canceled) {
		return err
	}
	return fail.Failed("The copy stopped after %s: %s.", ui.Size(uint64(c.moved.Load())), transfer.Cause(err))
}

func endsInSeparator(path string) bool {
	return strings.HasSuffix(path, "/") || strings.HasSuffix(path, string(filepath.Separator))
}

// unpackWith is the hint that unpacks a saved archive; nothing for one that went to stdout.
func unpackWith(saved string) string {
	if saved == "stdout" {
		return ""
	}
	return " To unpack it:  tar xf " + app.ShellWord(saved)
}
