// Package kept is where and how the CLI keeps what it remembers between runs, and reads it back:
// files under pethost in the user's configuration directory, which only their owner reads.
package kept

import (
	"encoding/json"
	"errors"
	"io/fs"
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

// Read reads the JSON of the file of that name into value; false = there is none, or no
// directory to keep one in.
func Read(name string, value any) (bool, error) {
	path, err := File(name)
	if err != nil {
		return false, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	if err := json.Unmarshal(data, value); err != nil {
		return false, errors.New(path + " is not a file pethost wrote: remove it")
	}
	return true, nil
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
