// Package release is the CLI's own releases: where they are, which is the newest, a release's
// binary for this computer with its checksum checked, the replacing of the running program by
// it, and the memory of the daily look for a newer one.
//
// What a release is made of is defined once, by the workflow that makes it
// (.github/workflows/release.yml); this package, install.sh and install.ps1 read the same
// three addresses:
//
//	<releases>/latest/download/version              the newest release's version: "0.4.0"
//	<releases>/download/v<version>/<binary>         pethost-<os>-<cpu>, with .exe for Windows
//	<releases>/download/v<version>/checksums.txt    sha256sum's line for each binary
package release

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// defaultURL is where the releases are; PETHOST_RELEASES_URL names another place, as
// PETHOST_URL does another panel.
const defaultURL = "https://github.com/pethost-dev/cli/releases"

func baseURL() string {
	if url := os.Getenv("PETHOST_RELEASES_URL"); url != "" {
		return strings.TrimRight(url, "/")
	}
	return defaultURL
}

// GoInstall is the command that brings a release to a program that came by `go install`.
func GoInstall(version string) string {
	return "go install github.com/pethost-dev/cli/cmd/pethost@v" + version
}

// Binary is the name of a release's program for this computer.
func Binary() string {
	name := "pethost-" + runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

// Newest is the newest release's version, "0.4.0".
func Newest(ctx context.Context) (string, error) {
	answer, err := fetch(ctx, baseURL()+"/latest/download/version")
	if err != nil {
		return "", err
	}
	defer answer.Close()

	// A version is a few characters: whatever else answers there is not read far.
	text, err := io.ReadAll(io.LimitReader(answer, 64))
	if err != nil {
		return "", err
	}
	version := strings.TrimSpace(string(text))
	if _, ok := numbers(version); !ok {
		return "", errors.New("the releases do not say which is the newest")
	}
	return version, nil
}

// Newer says whether version is a later release than than. What is no release's version, as a
// developer's own build's "dev", is neither newer nor older than anything.
func Newer(version, than string) bool {
	a, okA := numbers(version)
	b, okB := numbers(than)
	if !okA || !okB {
		return false
	}
	for i := range max(len(a), len(b)) {
		if x, y := at(a, i), at(b, i); x != y {
			return x > y
		}
	}
	return false
}

// numbers is a version as its numbers: "0.4.0" is 0, 4, 0. Only digits and dots make one: the
// version becomes part of an address and of a line on the screen.
func numbers(version string) ([]int, bool) {
	var parsed []int
	for part := range strings.SplitSeq(version, ".") {
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 || strings.TrimLeft(part, "0123456789") != "" {
			return nil, false
		}
		parsed = append(parsed, number)
	}
	return parsed, true
}

func at(numbers []int, i int) int {
	if i < len(numbers) {
		return numbers[i]
	}
	return 0
}

// fetchWait bounds one file of a release, from the request to the last byte of its answer: a
// server that stops answering must not keep `pethost update` waiting for ever, least of all in
// a script. A binary is some twenty megabytes: a slow line has the time.
const fetchWait = 10 * time.Minute

// fetch asks for a file of the releases and returns its content to read.
func fetch(ctx context.Context, address string) (io.ReadCloser, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	response, err := (&http.Client{Timeout: fetchWait}).Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf("%s answered %s", address, response.Status)
	}
	return response.Body, nil
}
