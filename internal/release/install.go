package release

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Install puts a release's program in the place of the one that runs, and returns where that
// is. The new file is written beside the old one and takes its place whole, once its checksum
// is the release's: a download that was cut short or altered on the way replaces nothing.
// A *NotReplaced = the program's folder is not this user's to write.
func Install(ctx context.Context, version string) (path string, err error) {
	path, err = running()
	if err != nil {
		return "", err
	}

	fresh, err := os.CreateTemp(filepath.Dir(path), freshPattern)
	if err != nil {
		return path, &NotReplaced{Cause: err}
	}
	defer os.Remove(fresh.Name()) // Nothing is left behind when a step fails; after the swap there is nothing to remove.

	err = download(ctx, version, fresh)
	if err == nil {
		err = fresh.Chmod(0o755)
	}
	if closeErr := fresh.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return path, err
	}
	if err := swap(fresh.Name(), path); err != nil {
		return path, &NotReplaced{Cause: err}
	}
	return path, nil
}

// NotReplaced is a release that could not be put in the program's place: the folder, or the
// file, is not this user's to write.
type NotReplaced struct {
	Cause error
}

func (e *NotReplaced) Error() string { return "the program could not be replaced: " + e.Cause.Error() }

func (e *NotReplaced) Unwrap() error { return e.Cause }

// running is the file of the program that runs, behind any link to it.
func running() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(path)
}

// download writes the release's program for this computer into a file, and checks what was
// written against the release's checksums.
func download(ctx context.Context, version string, into io.Writer) error {
	release := baseURL() + "/download/v" + version + "/"
	want, err := checksum(ctx, release+"checksums.txt", Binary())
	if err != nil {
		return err
	}

	program, err := fetch(ctx, release+Binary())
	if err != nil {
		return err
	}
	defer program.Close()

	sum := sha256.New()
	if _, err := io.Copy(io.MultiWriter(into, sum), program); err != nil {
		return err
	}
	if got := hex.EncodeToString(sum.Sum(nil)); got != want {
		return ErrChecksum
	}
	return nil
}

// ErrChecksum is a download that is not what the release says it published.
var ErrChecksum = errors.New("the download's checksum is not the release's")

// checksum finds a file's SHA-256 in the release's checksums, which are sha256sum's lines:
// the sum in hex, two spaces, the name.
func checksum(ctx context.Context, address, name string) (string, error) {
	list, err := fetch(ctx, address)
	if err != nil {
		return "", err
	}
	defer list.Close()

	lines := bufio.NewScanner(io.LimitReader(list, 64<<10))
	for lines.Scan() {
		sum, file, found := strings.Cut(lines.Text(), "  ")
		if found && file == name {
			return strings.ToLower(sum), nil
		}
	}
	if err := lines.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("the release has no %s", name)
}

// asideSuffix names the program an update moved out of the way.
const asideSuffix = ".old"

// swap puts the fresh file in the program's place. Windows does not let a running program's
// file be replaced or deleted, but lets it be renamed: there the old one is moved aside first,
// and deleted by the next update, or by Tidy on a later run.
func swap(fresh, path string) error {
	aside := path + asideSuffix
	os.Remove(aside)
	if runtime.GOOS != "windows" {
		return os.Rename(fresh, path)
	}

	if err := os.Rename(path, aside); err != nil {
		return err
	}
	if err := os.Rename(fresh, path); err != nil {
		os.Rename(aside, path) // The program stays where it was.
		return err
	}
	return nil
}

const (
	// freshPattern names a download until it takes the program's place: install.sh writes its
	// own under a name of this pattern too.
	freshPattern = ".pethost-*"
	// staleAfter is how old such a file is before it is nobody's download any more.
	staleAfter = time.Hour
)

// Tidy deletes what an earlier update left beside the program: the one Windows moved aside
// while it ran, and a download that was killed before it took the program's place.
func Tidy() {
	path, err := running()
	if err != nil {
		return
	}
	os.Remove(path + asideSuffix)

	left, _ := filepath.Glob(filepath.Join(filepath.Dir(path), freshPattern))
	for _, file := range left {
		if info, err := os.Stat(file); err == nil && time.Since(info.ModTime()) > staleAfter {
			os.Remove(file)
		}
	}
}
