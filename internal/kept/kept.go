// Package kept is where and how the CLI keeps what it remembers between runs: files under
// pethost in the user's configuration directory, which only their owner reads.
package kept

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// File is where the file of that name is kept.
func File(name string) (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "pethost", name), nil
}

// Write keeps value as the JSON of the file of that name, written whole or not at all: beside
// it first, then put in its place.
func Write(name string, value any) error {
	path, err := File(name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}

	temporary := path + ".new"
	if err := os.WriteFile(temporary, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}
