package detect

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"
	"unicode/utf16"

	"github.com/BurntSushi/toml"

	"github.com/pethost-dev/cli/internal/dotenv"
)

// A Folder is a project's folder as a stack reads it: files by their path from its root,
// slashes forward. Nothing in it is run, and nothing outside it is asked.
type Folder struct {
	files fs.FS
}

// readLimit bounds a file a stack reads: a manifest, a lockfile's first lines, a settings
// module. A larger file is read as if it were not there.
const readLimit = 4 << 20

// Has says whether the folder has a regular file at path.
func (f *Folder) Has(name string) bool {
	info, err := fs.Stat(f.files, name)
	return err == nil && info.Mode().IsRegular()
}

// HasDir says whether the folder has a directory at path.
func (f *Folder) HasDir(name string) bool {
	info, err := fs.Stat(f.files, name)
	return err == nil && info.IsDir()
}

// First is the first of the names the folder has as a file; empty = none.
func (f *Folder) First(names ...string) string {
	for _, name := range names {
		if f.Has(name) {
			return name
		}
	}
	return ""
}

// Read is a file's content; ok = it is a file of the folder and within what a stack reads.
func (f *Folder) Read(name string) (content []byte, ok bool) {
	if !f.Has(name) {
		return nil, false
	}
	file, err := f.files.Open(name)
	if err != nil {
		return nil, false
	}
	defer file.Close()

	content, err = io.ReadAll(io.LimitReader(file, readLimit+1))
	if err != nil || len(content) > readLimit {
		return nil, false
	}
	return plainText(content), true
}

// plainText is a text file's content without the mark an editor may begin it with, and out of
// UTF-16, which PowerShell's ">" writes and the tools that read these files take: a
// requirements.txt made there with `pip freeze > requirements.txt`, a Procfile, a version file.
func plainText(content []byte) []byte {
	if utf16LE := len(content) >= 2 && content[0] == 0xFF && content[1] == 0xFE; !utf16LE {
		return bytes.TrimPrefix(content, []byte{0xEF, 0xBB, 0xBF})
	}

	units := make([]uint16, 0, len(content)/2)
	for i := 2; i+1 < len(content); i += 2 {
		units = append(units, uint16(content[i])|uint16(content[i+1])<<8)
	}
	return []byte(string(utf16.Decode(units)))
}

// Text is a file's content as text; empty when it cannot be read.
func (f *Folder) Text(name string) string {
	content, _ := f.Read(name)
	return string(content)
}

// JSON reads a JSON file into a value; false = it is not there or is no JSON of that shape.
func (f *Folder) JSON(name string, into any) bool {
	content, ok := f.Read(name)
	return ok && json.Unmarshal(content, into) == nil
}

// TOML reads a TOML file into a value; false = it is not there or is no TOML of that shape.
func (f *Folder) TOML(name string, into any) bool {
	content, ok := f.Read(name)
	return ok && toml.Unmarshal(content, into) == nil
}

// Lines are a text file's lines without their comments (from "#" to the line's end) and the
// space around them, the empty ones left out: a requirements file's, a version file's.
func (f *Folder) Lines(name string) []string {
	content, ok := f.Read(name)
	if !ok {
		return nil
	}

	var lines []string
	scanner := bufio.NewScanner(bytes.NewReader(content))
	scanner.Buffer(nil, readLimit) // A line may be as long as the file: one of a lockfile is.
	for scanner.Scan() {
		line, _, _ := strings.Cut(scanner.Text(), "#")
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// Env are the variables of a dotenv file, as Compose reads them and the machine gives them to
// the container; none when it is not there.
func (f *Folder) Env(name string) map[string]string {
	content, _ := f.Read(name)
	return dotenv.Values(string(content))
}

// Names are the names in a directory of the folder ("." is its root), directories with a
// "/" after them; nil when it is no directory.
func (f *Folder) Names(dir string) []string {
	entries, err := fs.ReadDir(f.files, dir)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		names = append(names, name)
	}
	return names
}

// passed are the directories a walk does not enter: what a tool made or fetched, never what a
// person wrote.
var passed = []string{"node_modules", "vendor", "target", "dist", "build", "testdata", "__pycache__"}

// Walk calls visit for every file down to depth directories below the root, with its path. It
// enters no hidden directory and none that a tool fills.
func (f *Folder) Walk(depth int, visit func(name string)) {
	fs.WalkDir(f.files, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || name == "." {
			return nil
		}
		if !entry.IsDir() {
			visit(name)
			return nil
		}

		base := path.Base(name)
		hidden := strings.HasPrefix(base, ".")
		tooDeep := strings.Count(name, "/") >= depth
		if hidden || tooDeep || slices.Contains(passed, base) {
			return fs.SkipDir
		}
		return nil
	})
}
