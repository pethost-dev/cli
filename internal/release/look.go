package release

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// A Look is what the CLI remembers of looking for a newer release, between runs: a file
// beside the sign-in.
type Look struct {
	// When a newer release was last looked for: the next look is a day later.
	Time time.Time `json:"look_time"`
	// The release that is not brought up again: the person said no to it, or was told how to
	// get it.
	Settled string `json:"settled_version,omitempty"`
}

func lookFile() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "pethost", "update.json"), nil
}

// LastLook reads the memory; a zero Look when there is none.
func LastLook() Look {
	var look Look
	if path, err := lookFile(); err == nil {
		if data, err := os.ReadFile(path); err == nil {
			json.Unmarshal(data, &look)
		}
	}
	return look
}

// Remember writes the memory. An error = it could not be kept, as in a home that is read-only:
// whoever looks only once a day must then not look at all, or it would look at every command.
func (l Look) Remember() error {
	path, err := lookFile()
	if err != nil {
		return err
	}
	data, err := json.Marshal(l)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}
