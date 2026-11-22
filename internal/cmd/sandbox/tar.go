package sandbox

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// A directory crosses between this computer and a sandbox as a tar of what is in it, each entry
// under its path within the directory, as `tar -C <dir> -cf - .` writes it. What a sandbox sends
// is other people's bytes: it is unpacked inside the destination alone (os.Root), whatever its
// names and links say.

// writeTar writes a directory's contents as a tar: its directories, files and symbolic links.
// Anything else, as a socket, is left out.
func writeTar(w io.Writer, dir string) error {
	archive := tar.NewWriter(w)
	err := filepath.WalkDir(dir, func(at string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name, err := filepath.Rel(dir, at)
		if err != nil || name == "." {
			return err
		}
		return addToTar(archive, at, filepath.ToSlash(name), entry)
	})
	if err != nil {
		return err
	}
	return archive.Close()
}

func addToTar(archive *tar.Writer, at, name string, entry fs.DirEntry) error {
	info, err := entry.Info()
	if err != nil {
		return err
	}
	link := ""
	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		if link, err = os.Readlink(at); err != nil {
			return err
		}
	case !info.Mode().IsRegular() && !info.IsDir():
		return nil
	}

	header, err := tar.FileInfoHeader(info, filepath.ToSlash(link))
	if err != nil {
		return err
	}
	header.Name = name
	if info.IsDir() {
		header.Name += "/"
	}
	header.Uname, header.Gname, header.Uid, header.Gid = "", "", 0, 0
	if err := archive.WriteHeader(header); err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	file, err := os.Open(at)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(archive, file)
	return err
}

// extractTar unpacks a tar into dir, which it makes if missing: its directories, files and
// symbolic links. A name or a link that leads out of dir is refused by os.Root.
func extractTar(r io.Reader, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()

	archive := tar.NewReader(r)
	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := extractEntry(root, header, archive); err != nil {
			return fmt.Errorf("%s: %w", header.Name, err)
		}
	}
}

func extractEntry(root *os.Root, header *tar.Header, content io.Reader) error {
	name := path.Clean(strings.TrimPrefix(header.Name, "./"))
	if name == "." {
		return nil
	}
	if parent := path.Dir(name); parent != "." {
		if err := root.MkdirAll(parent, 0o755); err != nil {
			return err
		}
	}

	mode := fs.FileMode(header.Mode).Perm()
	switch header.Typeflag {
	case tar.TypeDir:
		return root.MkdirAll(name, mode|0o700)
	case tar.TypeSymlink:
		_ = root.Remove(name)
		return root.Symlink(header.Linkname, name)
	case tar.TypeReg:
		file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
		if err != nil {
			return err
		}
		if _, err := io.Copy(file, content); err != nil {
			file.Close()
			return err
		}
		return file.Close()
	}
	return nil
}
