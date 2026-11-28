package sandbox

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/pethost-dev/sdk/go/sandboxes"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/transfer"
	"github.com/pethost-dev/cli/internal/ui"
)

// cpCommand is `pethost sandbox cp <from> <to>`.
func cpCommand(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "cp <from> <to>",
		Short: "Copy a file or a directory into a sandbox or out of it",
		Long: `Copies between this computer and a sandbox. The sandbox's side is its id or its name, a
colon and a path there, absolute or under /home/user: k7f3q2xy0a1b2c3d:/data/x.csv.

A file of any size is copied, straight to the sandbox's server. A directory's contents go
into the path on the other side, which is made if it is missing. A "-" in place of the
local path is stdin or stdout: a directory comes out of a sandbox as a tar archive. A file
copied in takes the place of one already there; a destination that is a directory takes it
under its own name.`,
		Example: `  pethost sandbox cp ./app k7f3q2xy0a1b2c3d:/home/user/app
  pethost sandbox cp k7f3q2xy0a1b2c3d:/home/user/out.csv .
  pethost sandbox cp k7f3q2xy0a1b2c3d:/home/user/results ./results
  pg_dump mydb | pethost sandbox cp - k7f3q2xy0a1b2c3d:/tmp/dump.sql`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			from, to := sideOf(args[0]), sideOf(args[1])
			switch {
			case from.sandbox != "" && to.sandbox != "":
				return fail.Usage("pethost sandbox cp copies between this computer and a sandbox: one side is a path here", cmd.UseLine())
			case from.sandbox != "":
				return copyOut(cmd.Context(), a, from, to.path)
			case to.sandbox != "":
				return copyIn(cmd.Context(), a, from.path, to)
			}
			return fail.Usage("pethost sandbox cp: neither side is in a sandbox. Write that side as <id>:/path", cmd.UseLine())
		},
	}
}

// streamed is the local side that is stdin or stdout, not a file.
const streamed = "-"

// A side of cp: a path here, or a sandbox's id and a path there.
type side struct {
	sandbox string // Empty = this computer.
	path    string
}

// sideOf reads a side as typed: "<id>:<path>" is in a sandbox, by its id or its name (idOf). A
// Windows path's drive letter is not an id, and a word with a slash before its colon is a path
// here.
func sideOf(typed string) side {
	id, path, found := strings.Cut(typed, ":")
	if !found || typed == streamed || filepath.VolumeName(typed) != "" || id == "" || strings.ContainsAny(id, `/\.`) {
		return side{path: typed}
	}
	return side{sandbox: id, path: path}
}

// linkFor is the link a copy goes by: one request, for one path, made for this copy alone. Its
// URL holds its token, so it is never printed, and no error here holds it.
func linkFor(ctx context.Context, c *sandboxes.Client, id string, target sandboxes.CreateLinkTarget) (*sandboxes.Link, error) {
	link, err := c.CreateLink(ctx, &sandboxes.CreateLinkRequest{SandboxID: id, Target: target, TTLSeconds: 3600})
	if err != nil {
		return nil, about(id, err)
	}
	return link, nil
}

// refusalBytes bounds how much of a server's refusal of a link's request is read.
const refusalBytes = 4 << 10

// overLink makes a link's one request: body is an upload's bytes, nil for a download, and
// length its size, -1 when it is not known. A download's answer is the file, which the caller
// reads and closes.
func overLink(ctx context.Context, link *sandboxes.Link, method string, body io.Reader, length int64) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, method, link.URL, body)
	if err != nil {
		return nil, fail.Failed("The sandbox's server gave a link that cannot be used.")
	}
	if body != nil {
		request.ContentLength = length
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fail.Failed("The copy did not get through to the sandbox's server: %v.", transfer.Cause(err))
	}
	if response.StatusCode/100 != 2 {
		defer response.Body.Close()
		answer, _ := io.ReadAll(io.LimitReader(response.Body, refusalBytes))
		reason := strings.TrimSpace(ui.Clean(string(answer)))
		if reason == "" {
			reason = response.Status
		}
		return nil, fail.Failed("The sandbox's server refused the copy: %s", reason)
	}
	return response, nil
}

// A copy's progress: the bytes moved so far, on a line redrawn in place, and what is said once
// it is done.
type progress struct {
	a       *app.App
	started time.Time
	moved   atomic.Int64
	live    *ui.Live
}

// follow starts showing how far a copy of name is; total < 0 = not known.
func follow(a *app.App, name string, total int64) *progress {
	p := &progress{a: a, started: time.Now()}
	title := func() string {
		moved := ui.Size(uint64(p.moved.Load()))
		if total > 0 {
			return fmt.Sprintf("Copying %s… %s of %s", name, moved, ui.Size(uint64(total)))
		}
		return fmt.Sprintf("Copying %s… %s", name, moved)
	}
	p.live = a.UI.Live(title())
	p.live.Follow(title)
	return p
}

// Write counts what passes: a copy is teed through it.
func (p *progress) Write(b []byte) (int, error) {
	p.moved.Add(int64(len(b)))
	return len(b), nil
}

// teed is a reader whose bytes are counted as they are read.
func (p *progress) teed(from io.Reader) io.Reader {
	return io.TeeReader(from, p)
}

// done stops the line, and says what went where, how much and how fast.
func (p *progress) done(from, to string) {
	p.live.Stop()
	p.a.UI.Done("Copied %s to %s · %s in %s", ui.Clean(from), ui.Clean(to), ui.Size(uint64(p.moved.Load())), ui.Duration(time.Since(p.started)))
}

// kindOf says whether a path in a sandbox is a directory, for a large file copied in. ListFiles
// lists a file's path as that file alone, and a directory's as what is in it, each under its own
// path: a directory that holds one file of its own name, asked for by a relative path, reads as
// that file, and the upload to it is then refused by the server.
func kindOf(ctx context.Context, c *sandboxes.Client, id, asked string) (sandboxes.FileKind, error) {
	list, err := c.ListFiles(ctx, &sandboxes.ListFilesRequest{SandboxID: id, Path: asked})
	if err != nil {
		return sandboxes.FileKindUnspecified, about(id, err)
	}
	if len(list.Files) == 1 && list.NextPageToken == "" && list.Files[0].Kind != sandboxes.FileKindDirectory && isPath(list.Files[0].Path, asked) {
		return sandboxes.FileKindFile, nil
	}
	return sandboxes.FileKindDirectory, nil
}

// isPath says whether a path a listing gave is the one asked for, which may be relative.
func isPath(listed, asked string) bool {
	asked = path.Clean(asked)
	return listed == asked || (!path.IsAbs(asked) && strings.HasSuffix(listed, "/"+asked))
}
