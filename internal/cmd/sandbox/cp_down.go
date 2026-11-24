package sandbox

import (
	"context"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"

	"github.com/pethost-dev/sdk/go/sandboxes"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/fail"
)

// Copying out: a download link, streamed into the file here, or a directory's tar unpacked into
// the path here.

func copyOut(ctx context.Context, a *app.App, from side, local string) error {
	c, err := client(a)
	if err != nil {
		return err
	}
	if from.sandbox, err = idOf(ctx, c, from.sandbox); err != nil {
		return err
	}
	kind, err := kindOf(ctx, c, from.sandbox, from.path)
	if err != nil {
		return err
	}
	link, err := linkFor(ctx, c, from.sandbox, sandboxes.CreateLinkDownloadPath(from.path))
	if err != nil {
		return err
	}
	response, err := overLink(ctx, link, http.MethodGet, nil, -1)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	name := path.Base(path.Clean(from.path))
	p := follow(a, name, response.ContentLength)
	defer p.live.Stop()
	body := p.teed(response.Body)
	switch {
	case local == streamed:
		err = toStdout(a, body)
	case kind == sandboxes.FileKindDirectory:
		err = extractTar(body, local)
	default:
		local, err = toFile(body, local, name)
	}
	if err != nil {
		return fail.Failed("The copy did not finish: %v", fail.Innermost(err))
	}
	if local != streamed {
		p.done(from.sandbox+":"+from.path, local)
	}
	return nil
}

// toStdout writes a download to stdout: as it is for a program, and for a person without what a
// terminal would obey.
func toStdout(a *app.App, body io.Reader) error {
	out := a.UI.Out.Cleaned()
	if _, err := io.Copy(out, body); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// toFile writes a download into the file local, or under name into it when it is a directory,
// and returns where it went. The bytes go to a file beside it first, which takes its place once
// they are all written: a copy that breaks leaves the file that was there as it was, and takes
// out what it wrote.
func toFile(body io.Reader, local, name string) (string, error) {
	if info, err := os.Stat(local); err == nil && info.IsDir() {
		local = filepath.Join(local, name)
	}
	part := local + ".part"
	file, err := os.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return local, err
	}

	_, err = io.Copy(file, body)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(part, local)
	}
	if err != nil {
		os.Remove(part)
	}
	return local, err
}
