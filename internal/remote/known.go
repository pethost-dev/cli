package remote

import (
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// trustOnFirstUse judges a host's key by a file of known hosts, in OpenSSH's format: a host the
// file does not list is written into it with the key it shows, and a host it lists must show a
// key the file has for it, else ErrWrongHost. Nobody is asked: the first connection is the one
// trusted, as ssh's accept-new does.
func trustOnFirstUse(file string) (ssh.HostKeyCallback, error) {
	if err := ensureFile(file); err != nil {
		return nil, err
	}
	known, err := knownhosts.New(file)
	if err != nil {
		return nil, errors.New(file + " is not a file of known hosts: " + err.Error())
	}

	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := known(hostname, remote, key)
		keyErr, isKeyErr := errors.AsType[*knownhosts.KeyError](err)
		switch {
		case err == nil:
			return nil
		case isKeyErr && len(keyErr.Want) == 0:
			return remember(file, hostname, key)
		case isKeyErr:
			return ErrWrongHost
		}
		return err
	}, nil
}

// ensureFile makes the file of known hosts, empty, where there is none yet: only its owner reads
// it.
func ensureFile(file string) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	created, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return created.Close()
}

// remember adds a host and its key to the file of known hosts.
func remember(file, hostname string, key ssh.PublicKey) error {
	out, err := os.OpenFile(file, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	line := knownhosts.Line([]string{knownhosts.Normalize(hostname)}, key) + "\n"
	if _, err := out.WriteString(line); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
