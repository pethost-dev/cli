package kernel

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
)

// The archive a deploy sends: the folder's files as a gzipped tar, written straight into the
// upload, so that nothing is kept in memory or on the disk.

// pack writes the folder's contents to w as a tar.gz, adding to sent the bytes of each file as
// they go, for the progress line.
func pack(w io.Writer, dir string, found *contents, sent *atomic.Uint64) error {
	compressed := gzip.NewWriter(w)
	archive := tar.NewWriter(compressed)

	for _, item := range found.entries {
		if err := packEntry(archive, dir, item, sent); err != nil {
			return err
		}
	}
	if err := archive.Close(); err != nil {
		return err
	}
	return compressed.Close()
}

func packEntry(archive *tar.Writer, dir string, item entry, sent *atomic.Uint64) error {
	path := filepath.Join(dir, filepath.FromSlash(item.path))

	link := ""
	if item.info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		if err != nil {
			return err
		}
		link = filepath.ToSlash(target)
	}
	header, err := tar.FileInfoHeader(item.info, link)
	if err != nil {
		return err
	}
	header.Name = item.path
	if item.info.IsDir() {
		header.Name += "/"
	}
	header.Mode = int64(portableMode(item.info.Mode()))
	// Who owns a file on this computer means nothing on the machine.
	header.Uid, header.Gid, header.Uname, header.Gname = 0, 0, "", ""

	if err := archive.WriteHeader(header); err != nil {
		return err
	}
	if !item.info.Mode().IsRegular() {
		return nil
	}
	return packFile(archive, path, item, sent)
}

// portableMode is a file's permission bits as the machine gets them. Windows keeps no
// executable bit, so there every file is sent as one that may run, as Docker sends a build's
// context from Windows: a script that lost its bit would not start.
func portableMode(mode os.FileMode) os.FileMode {
	if runtime.GOOS == "windows" {
		return 0o755
	}
	return mode.Perm()
}

// packFile copies a file's bytes into the archive: as many as its header said, also when the
// file has grown meanwhile, since an archive with another count is broken. A file that has got
// shorter cannot be packed: the folder is being written to.
func packFile(archive *tar.Writer, path string, item entry, sent *atomic.Uint64) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.CopyN(io.MultiWriter(archive, counter{sent}), file, item.info.Size())
	if errors.Is(err, io.EOF) {
		return fmt.Errorf("%s changed while it was read", item.path)
	}
	return err
}

// counter counts the bytes written through it.
type counter struct {
	sent *atomic.Uint64
}

func (c counter) Write(p []byte) (int, error) {
	c.sent.Add(uint64(len(p)))
	return len(p), nil
}
