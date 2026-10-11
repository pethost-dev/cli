// Package kept is where the CLI keeps what it remembers between runs: pethost under the user's
// configuration directory.
package kept

import (
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
