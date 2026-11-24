package sandbox

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/pethost-dev/sdk/go/sandboxes"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/fail"
)

// Copying in. A file of up to 16 MiB goes in WriteFiles' request, whose bound is 17 MiB; a
// larger one, or stdin past that, by an upload link, streamed. A directory goes as a tar by a
// link into /tmp, which a command then unpacks where it was sent, and deletes.

// smallAtMost is the largest file sent within WriteFiles' request.
const smallAtMost = 16 << 20

func copyIn(ctx context.Context, a *app.App, local string, to side) error {
	c, err := clientForChange(a)
	if err != nil {
		return err
	}
	if to.sandbox, err = idOf(ctx, c, to.sandbox); err != nil {
		return err
	}
	if local == streamed {
		return copyStdinIn(ctx, a, c, to)
	}

	info, err := os.Stat(local)
	if err != nil {
		return fail.Failed("%s cannot be read: %v", local, fail.Innermost(err))
	}
	if info.IsDir() {
		return copyDirectoryIn(ctx, a, c, local, to)
	}
	return copyFileIn(ctx, a, c, local, info, to)
}

// copyFileIn copies one file in: into the path given, or under its own name into the directory
// the path is.
func copyFileIn(ctx context.Context, a *app.App, c *sandboxes.Client, local string, info os.FileInfo, to side) error {
	if strings.HasSuffix(to.path, "/") {
		to.path = path.Join(to.path, filepath.Base(local))
	}
	file, err := os.Open(local)
	if err != nil {
		return fail.Failed("%s cannot be read: %v", local, fail.Innermost(err))
	}
	defer file.Close()

	p := follow(a, filepath.Base(local), info.Size())
	defer p.live.Stop()
	if info.Size() <= smallAtMost {
		data, err := io.ReadAll(p.teed(file))
		if err != nil {
			return err
		}
		to.path, err = writeSmall(ctx, c, to, data, modeOf(info), filepath.Base(local))
		if err != nil {
			return err
		}
	} else {
		if kind, err := kindOf(ctx, c, to.sandbox, to.path); err == nil && kind == sandboxes.FileKindDirectory {
			to.path = path.Join(to.path, filepath.Base(local))
		}
		if err := upload(ctx, c, to, p.teed(file), info.Size()); err != nil {
			return err
		}
	}
	p.done(local, to.sandbox+":"+to.path)
	return nil
}

// copyStdinIn copies what is piped in into the path given.
func copyStdinIn(ctx context.Context, a *app.App, c *sandboxes.Client, to side) error {
	p := follow(a, "stdin", -1)
	defer p.live.Stop()
	head, err := io.ReadAll(io.LimitReader(p.teed(a.UI.In), smallAtMost+1))
	if err != nil {
		return err
	}
	if len(head) <= smallAtMost {
		if _, err := writeSmall(ctx, c, to, head, "", ""); err != nil {
			return err
		}
	} else if err := upload(ctx, c, to, io.MultiReader(bytes.NewReader(head), p.teed(a.UI.In)), -1); err != nil {
		return err
	}
	p.done("stdin", to.sandbox+":"+to.path)
	return nil
}

// writeSmall writes a file within one WriteFiles, and returns the path it went to: the path
// given, or the name under it when the path is a directory and a name is given.
func writeSmall(ctx context.Context, c *sandboxes.Client, to side, data []byte, mode, name string) (string, error) {
	for {
		answer, err := c.WriteFiles(ctx, &sandboxes.WriteFilesRequest{SandboxID: to.sandbox, Changes: []sandboxes.FileChange{
			{Path: to.path, Change: sandboxes.FileChangeData(data), Mode: mode},
		}})
		if err != nil {
			return "", about(to.sandbox, err)
		}
		switch {
		case answer.DoneCount == 1:
			return to.path, nil
		case answer.Failure == sandboxes.FileFailureIsADirectory && name != "":
			to.path, name = path.Join(to.path, name), ""
			continue
		}
		return "", fail.Failed("The sandbox did not take the file: %s", answer.FailureMessage)
	}
}

// upload streams a file to a path of the sandbox by an upload link. length < 0 = not known.
func upload(ctx context.Context, c *sandboxes.Client, to side, body io.Reader, length int64) error {
	link, err := linkFor(ctx, c, to.sandbox, sandboxes.CreateLinkUploadPath(to.path))
	if err != nil {
		return err
	}
	response, err := overLink(ctx, link, http.MethodPut, body, length)
	if err != nil {
		return err
	}
	return response.Body.Close()
}

// copyDirectoryIn copies a directory's contents into the path given, which is made if missing.
func copyDirectoryIn(ctx context.Context, a *app.App, c *sandboxes.Client, local string, to side) error {
	archive := "/tmp/pethost-" + randomName() + ".tar"
	p := follow(a, filepath.Base(filepath.Clean(local)), -1)
	defer p.live.Stop()

	reader, writer := io.Pipe()
	go func() { writer.CloseWithError(writeTar(writer, local)) }()
	err := upload(ctx, c, side{sandbox: to.sandbox, path: archive}, p.teed(reader), -1)
	reader.Close()
	if err != nil {
		return err
	}

	dir := shellQuoted(to.path)
	script := fmt.Sprintf("mkdir -p %s && tar -xf %s -C %s; code=$?; rm -f %s; exit $code", dir, archive, dir, archive)
	if err := runToEnd(ctx, c, to.sandbox, script); err != nil {
		return err
	}
	p.done(local, to.sandbox+":"+to.path)
	return nil
}

// runToEnd runs a command of the CLI's own in a sandbox and waits for its end: a failure is its
// stderr.
func runToEnd(ctx context.Context, c *sandboxes.Client, id, script string) error {
	command, err := c.RunCommand(ctx, &sandboxes.RunCommandRequest{SandboxID: id, Command: script})
	for err == nil && command.State == sandboxes.CommandStateRunning {
		command, err = c.GetCommand(ctx, &sandboxes.GetCommandRequest{SandboxID: id, CommandID: command.CommandID})
	}
	if err != nil {
		return about(id, err)
	}
	if command.State != sandboxes.CommandStateExited || command.ExitCode == nil || *command.ExitCode != 0 {
		return fail.Failed("The sandbox could not unpack the copy: %s", strings.TrimSpace(command.Stderr))
	}
	return nil
}

// modeOf is a file's permissions as WriteFiles takes them, "0755"; empty on Windows, whose files
// have none to keep.
func modeOf(info os.FileInfo) string {
	if runtime.GOOS == "windows" {
		return ""
	}
	return fmt.Sprintf("%04o", info.Mode().Perm())
}

// randomName is a name no other copy takes.
func randomName() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// shellQuoted is a path as bash takes it whole.
func shellQuoted(p string) string {
	return "'" + strings.ReplaceAll(p, "'", `'\''`) + "'"
}
