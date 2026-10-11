package reach

import (
	"fmt"
	"io"
	"path/filepath"
	"sync/atomic"
	"time"

	pethost "github.com/pethost-dev/sdk/go"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/ui"
)

// cpCommand is `pethost cp <from> <to>`.
func cpCommand(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cp <from> <to>",
		Short: "Copy a file in or out of a container or a volume",
		Long: `Copies one file between this computer and your machine, straight to the machine.

One side is a path here; the other is on the machine, as pethost files writes it: a name
and a colon before the path, a service's for its container or a volume's for that volume,
and a colon alone for the project's own files, which can only be copied out.

A directory is copied out as a tar archive. A "-" in place of the local path is stdin or
stdout. A file copied in takes the place of one already there; a destination that is a
directory, here or there, takes the file under its own name.`,
		Example: `  pethost cp web:/data/x.db ./x.db
  pethost cp ./seed.sql db:/tmp/seed.sql
  pethost cp data:/uploads ./uploads.tar
  pethost cp :/compose.yaml .
  pg_dump mydb | pethost cp - db:/tmp/dump.sql`,
		Args: cobra.ExactArgs(2),
	}
	flags := addRootFlags(cmd)

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		c := &copying{a: a, flags: flags}
		from, to := args[0], args[1]
		switch {
		case onMachine(from) && onMachine(to):
			return fail.Usage("pethost cp copies between this computer and your machine: one side is a path here", cmd.UseLine())
		case onMachine(from):
			return c.out(cmd.Context(), parsePlace(from), to)
		case onMachine(to):
			return c.in(cmd.Context(), from, parsePlace(to))
		}
		return fail.Usage("pethost cp: neither side is on your machine. Write that side as web:/path, or :/path for the project's own files", cmd.UseLine())
	}
	return cmd
}

// streamed is the local side that is stdin or stdout, not a file.
const streamed = "-"

// onMachine says whether a side of cp is written as a path on the machine: with a name and a
// colon. A Windows path's drive letter is not a name.
func onMachine(side string) bool {
	return side != streamed && filepath.VolumeName(side) == "" && !parsePlace(side).bare
}

// copying is one run of the command.
type copying struct {
	a     *app.App
	flags *rootFlags
	// When the machine was first asked: what the copy's time is counted from, after any question.
	started time.Time
	// The bytes moved so far.
	moved atomic.Int64
}

// Write counts what passes: a copy is teed through it.
func (c *copying) Write(p []byte) (int, error) {
	c.moved.Add(int64(len(p)))
	return len(p), nil
}

// follow shows how far the copy is, on a line redrawn in place, until the function it returns
// is called. total < 0 = not known.
func (c *copying) follow(name string, total int64) (stop func()) {
	title := func() string {
		moved := ui.Size(uint64(c.moved.Load()))
		if total > 0 {
			return fmt.Sprintf("Copying %s… %s of %s", name, moved, ui.Size(uint64(total)))
		}
		return fmt.Sprintf("Copying %s… %s", name, moved)
	}

	live := c.a.UI.Live(title())
	stopTitle := live.Follow(title)
	return func() {
		stopTitle()
		live.Stop()
	}
}

// copied says that the copy is done: what went where, how much and how fast.
func (c *copying) copied(from, to string) {
	c.a.UI.Done("Copied %s to %s · %s in %s", ui.Clean(from), ui.Clean(to),
		ui.Size(uint64(c.moved.Load())), ui.Duration(time.Since(c.started)))
}

// printTicket is --json's answer: the API's, less the two fields that hold the transfer's URL,
// which is a secret of that one request and has been used by now.
func (c *copying) printTicket(ticket *pethost.CreateTransferResponse) error {
	if !c.a.JSON {
		return nil
	}
	shown := *ticket
	shown.URL, shown.Command = "", ""
	return c.a.PrintJSON(&shown)
}

// teed is a reader whose bytes are counted as they are read.
func (c *copying) teed(from io.Reader) io.Reader {
	return io.TeeReader(from, c)
}
