// Package remote is SSH into a service's container, inside the binary: this computer's own key,
// the connection to the machine's gateway, checked against the host key the machine reports and
// kept alive, a program's session on a terminal or a pipe, and a local port forwarded to the
// container.
//
// It knows SSH and nothing of the API: who to log in as, where, and which host key to expect are
// given to it.
package remote

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/pethost-dev/cli/internal/kept"
)

// A Key is this computer's SSH key: the CLI's own, which no other program uses.
type Key struct {
	signer ssh.Signer
	// The public half as authorized_keys writes it: "ssh-ed25519 AAAA...".
	PublicKey string
	// "SHA256:...", as the machine lists the keys it takes.
	Fingerprint string
}

// keyFile is where the key is kept: pethost/ssh_key under the user's configuration directory,
// beside the sign-in.
func keyFile() (string, error) {
	return kept.File("ssh_key")
}

// OwnKey is this computer's key, made on first use: an ed25519 key in a file only its owner
// reads.
func OwnKey() (*Key, error) {
	path, err := keyFile()
	if err != nil {
		return nil, err
	}

	data, err := readKey(path)
	if errors.Is(err, fs.ErrNotExist) {
		data, err = makeKey(path)
	}
	if err != nil {
		return nil, err
	}

	signer, err := ssh.ParsePrivateKey(data)
	if err != nil {
		return nil, errors.New(path + " is not a key the CLI wrote: remove it, and the next command makes a new one")
	}
	public := signer.PublicKey()
	return &Key{
		signer:      signer,
		PublicKey:   strings.TrimSpace(string(ssh.MarshalAuthorizedKey(public))),
		Fingerprint: ssh.FingerprintSHA256(public),
	}, nil
}

// makeKey writes a new key to path and returns the file's contents. Two commands that start at
// once must end with one key: the file is made only if it is not there, and the one who lost
// reads the winner's.
func makeKey(path string) ([]byte, error) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	block, err := ssh.MarshalPrivateKey(private, "pethost")
	if err != nil {
		return nil, err
	}
	data := pem.EncodeToMemory(block)

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return readKey(path)
	}
	if err != nil {
		return nil, err
	}

	if _, err := file.Write(data); err != nil {
		file.Close()
		os.Remove(path)
		return nil, err
	}
	return data, file.Close()
}

// readKey reads the key at path once it is whole: the command that makes it makes the file empty,
// and writes the key into it a moment later, while another command may be reading. A file still
// no key after a second is returned as it is, for the caller to refuse.
func readKey(path string) ([]byte, error) {
	deadline := time.Now().Add(time.Second)
	for {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if _, err := ssh.ParsePrivateKey(data); err == nil || time.Now().After(deadline) {
			return data, nil
		}
		time.Sleep(10 * time.Millisecond)
	}
}
