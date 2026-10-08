// Package browser opens an address in the person's browser.
package browser

import (
	"errors"
	"net/url"
	"os/exec"
	"runtime"
)

// Open opens address in the person's browser, by the operating system's own command. Only an
// http or https address is opened: the address may come from the API, and nothing else of the
// system's opener is for it to reach. An error = it could not be started, as on a machine with
// no desktop: the caller prints the address for the person to open.
func Open(address string) error {
	parsed, err := url.Parse(address)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return errors.New("not a web address")
	}

	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", address)
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", address)
	default:
		command = exec.Command("xdg-open", address)
	}
	if err := command.Start(); err != nil {
		return err
	}
	go command.Wait() // The opener ends by itself; nobody waits for the browser.
	return nil
}
