// Package signin is the sign-in a command runs with: the token its calls carry and the panel
// they go to. PETHOST_TOKEN wins over the stored sign-in, a file only its owner reads under
// the user's configuration directory; PETHOST_URL names another panel.
//
// A stored token is sent only to the panel it was given by.
package signin

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/pethost-dev/cli/internal/kept"
)

// DefaultURL is Pethost's own panel.
const DefaultURL = "https://console.pethost.dev"

// A SignIn is what a command's calls are made with.
type SignIn struct {
	// The token every call carries. Empty = not signed in.
	Token string
	// The panel the calls go to.
	URL string
	// The token is PETHOST_TOKEN's, not the stored sign-in's.
	FromEnvironment bool
	// PETHOST_TOKEN took the place of a sign-in stored on this computer: a command that changes
	// something says whose word it acts on.
	OverStored bool
}

// Stored is what a sign-in keeps on the disk between runs.
type Stored struct {
	// The panel that gave the token.
	URL string `json:"url"`
	// What the calls carry: an API token, or the token of an OAuth sign-in.
	Token string `json:"token"`
	// An OAuth sign-in: the client it registered as, which logout revokes the token with. Empty
	// for an API token, which is how a sign-in made in the browser is told from one.
	ClientID string `json:"client_id,omitempty"`
	// When the person signed in.
	SignInTime time.Time `json:"sign_in_time,omitzero"`
}

// Current is the sign-in of this run: PETHOST_TOKEN if set, else the stored one if it is of the
// panel in use.
func Current() (SignIn, error) {
	stored, err := Load()
	if err != nil {
		return SignIn{}, err
	}

	current := SignIn{URL: panelURL(stored)}
	if token := os.Getenv("PETHOST_TOKEN"); token != "" {
		current.Token, current.FromEnvironment = token, true
		current.OverStored = stored != nil && stored.Token != ""
		return current, nil
	}
	if stored != nil && samePanel(stored.URL, current.URL) {
		current.Token = stored.Token
	}
	return current, nil
}

// panelURL is the panel in use: PETHOST_URL, else the stored sign-in's unless PETHOST_TOKEN
// replaces that sign-in, else Pethost's own.
func panelURL(stored *Stored) string {
	if url := os.Getenv("PETHOST_URL"); url != "" {
		return strings.TrimRight(url, "/")
	}
	if stored != nil && stored.URL != "" && os.Getenv("PETHOST_TOKEN") == "" {
		return stored.URL
	}
	return DefaultURL
}

func samePanel(a, b string) bool {
	return strings.TrimRight(a, "/") == strings.TrimRight(b, "/")
}

// storedName is the sign-in's file: pethost/signin.json under the user's configuration
// directory.
const storedName = "signin.json"

func file() (string, error) {
	return kept.File(storedName)
}

// Load reads the stored sign-in; nil = there is none.
func Load() (*Stored, error) {
	path, err := file()
	if err != nil {
		return nil, nil // No home to keep one in: nothing is stored.
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var stored Stored
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, errors.New(path + " is not a sign-in the CLI wrote: remove it and run pethost login")
	}
	return &stored, nil
}

// Store keeps a sign-in for the next runs.
func Store(stored Stored) error {
	return kept.Write(storedName, stored)
}

// Remove forgets the stored sign-in; nil when there was none.
func Remove() error {
	path, err := file()
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
