package kernel

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/ui"
)

// What a folder holds, read before anything is sent: the files to pack, how much they are, and
// which of the names the API leaves out were there. Nothing of it is judged: the machine says
// whether the files can run.

// contents are a folder's files as they will be packed.
type contents struct {
	// Every file, folder and link to pack, in the order walked, each with its path from the
	// folder's root, slashes forward.
	entries []entry
	// The regular files among them, and their bytes together.
	files int
	bytes uint64
	// The names left out that the folder had, in the API's order.
	leftOut []string
	// The folder's root has a .env.
	hasEnv bool
	// The names in the folder's root, for what follows when the machine finds nothing to run.
	rootNames []string
}

type entry struct {
	path string
	info fs.FileInfo
}

// scan walks the folder, leaving out every file and folder with one of the names the API's
// CreateTransfer lists, wherever it lies.
func scan(dir string, excludeNames []string) (*contents, error) {
	found := &contents{}
	met := map[string]bool{}

	err := filepath.WalkDir(dir, func(path string, item fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == dir {
			return nil
		}
		if slices.Contains(excludeNames, item.Name()) {
			met[item.Name()] = true
			if item.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		return found.add(dir, path, item)
	})
	if err != nil {
		return nil, fail.Failed("This folder could not be read: %s", err)
	}

	for _, name := range excludeNames {
		if met[name] {
			found.leftOut = append(found.leftOut, name)
		}
	}
	return found, nil
}

func (c *contents) add(dir, path string, item fs.DirEntry) error {
	info, err := item.Info()
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(dir, path)
	if err != nil {
		return err
	}
	relative = filepath.ToSlash(relative)

	if !strings.Contains(relative, "/") {
		c.rootNames = append(c.rootNames, relative)
		c.hasEnv = c.hasEnv || relative == ".env"
	}
	switch {
	case info.Mode().IsRegular():
		c.files++
		c.bytes += uint64(info.Size())
	case !info.IsDir() && info.Mode()&fs.ModeSymlink == 0:
		return nil // A socket, a device, a pipe: nothing an archive carries.
	}
	c.entries = append(c.entries, entry{path: relative, info: info})
	return nil
}

// lookAtFolder reads what the folder holds, before anything is asked about the project: a
// folder that is clearly the wrong one is asked about first, and once.
func (d *deploy) lookAtFolder(ctx context.Context) error {
	if err := d.readFolder(ctx); err != nil {
		return err
	}
	if why := d.wrongFolder(d.packed); why != "" {
		return d.confirmFolder(d.packed, why)
	}
	return nil
}

// readFolder reads what the folder holds now. The names to leave out are the API's and come
// with a transfer, which the upload then uses.
func (d *deploy) readFolder(ctx context.Context) error {
	transfer, err := d.newTransfer(ctx)
	if err != nil {
		return err
	}

	live := d.a.UI.Live("Reading this folder…")
	found, err := scan(d.dir, transfer.ExcludeNames)
	live.Stop()
	if err != nil {
		return err
	}
	d.transfer, d.packed = transfer, found
	return nil
}

// plentyBytes is the size above which a folder is asked about before it is sent: more than a
// project usually is.
const plentyBytes = 200 << 20

// wrongFolder says why this folder may not be the one the person means; empty when nothing
// says so.
func (d *deploy) wrongFolder(found *contents) string {
	if home, err := os.UserHomeDir(); err == nil && filepath.Clean(home) == d.dir {
		return "That is your home folder."
	}
	if found.bytes > plentyBytes {
		return "That is more than a project usually is."
	}
	return ""
}

// confirmFolder shows a folder that is clearly the wrong one, with its path, file count and
// size, and asks before sending it.
func (d *deploy) confirmFolder(found *contents, why string) error {
	d.a.UI.Info("%s: %s, %s. %s", d.dir, ui.Plural(uint64(found.files), "file"), ui.Size(found.bytes), why)
	yes, err := d.a.UI.Confirm("Send all of it?", d.a.CommandLine("--yes"))
	if err != nil {
		return err
	}
	if !yes {
		return fail.Declined("Nothing was sent.").WithLead("To deploy another folder:", "pethost deploy <dir>")
	}
	return nil
}

// packedWords say what was packed: "38 files, 212 KB".
func (c *contents) packedWords() string {
	return fmt.Sprintf("%s, %s", ui.Plural(uint64(c.files), "file"), ui.Size(c.bytes))
}

// packedAside says, for a first deploy, what was left out and that the folder's .env went with
// it: "(left out .git, node_modules · took .env from this folder)"; empty when neither.
func (c *contents) packedAside() string {
	var parts []string
	if len(c.leftOut) > 0 {
		parts = append(parts, "left out "+strings.Join(c.leftOut, ", "))
	}
	if c.hasEnv {
		parts = append(parts, "took .env from this folder")
	}
	if len(parts) == 0 {
		return ""
	}
	return "(" + strings.Join(parts, " · ") + ")"
}
