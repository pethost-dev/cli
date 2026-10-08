// Command pethost is Pethost's command-line tool: it puts a folder online and shows what runs
// on the account's machine.
package main

import (
	"os"

	"github.com/pethost-dev/cli/internal/root"
)

// version is the release's, stamped by its build: -ldflags "-X main.version=0.1.0". A build
// without it says what the Go toolchain knows of it.
var version string

func main() {
	os.Exit(root.Run(version))
}
