package reach

import (
	"context"
	"errors"
	"io"
	"strings"

	pethost "github.com/pethost-dev/sdk/go"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/target"
	"github.com/pethost-dev/cli/internal/transfer"
	"github.com/pethost-dev/cli/internal/ui"
)

// filesCommand is `pethost files [name:]<path>`.
func filesCommand(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "files [name:]<path>",
		Short: "List a directory or print a file: the project's, a container's, a volume's",
		Long: `Lists a directory or prints a file on your machine.

A bare path is one of the project's own files, as they were deployed. With a name before
it, the path is in that service's container, or in the volume of that name:

  /compose.yaml     the project's file
  web:/app          a directory in the container of the service web
  data:/            the root of the volume data

A file that is not text is not written to a terminal: pethost cp copies it.`,
		Example: `  pethost files /compose.yaml
  pethost files web:/app
  pethost files data:/
  pethost files web:/app/config.json > config.json`,
		Args: cobra.MaximumNArgs(1),
	}
	flags := addRootFlags(cmd)

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		project, err := target.ForRead(ctx, a)
		if err != nil {
			return err
		}
		client, err := a.Client()
		if err != nil {
			return err
		}
		where := parsePlace(firstOf(args))
		if err := where.locate(ctx, a, client, project, flags); err != nil {
			return err
		}

		f := &files{a: a, client: client, project: project, where: where, flags: flags}
		first, err := f.read(ctx, pethost.ReadPathRequest{})
		if err != nil {
			return err
		}
		return f.show(ctx, first)
	}
	return cmd
}

// files is one run of the command.
type files struct {
	a       *app.App
	client  *pethost.Client
	project target.Project
	where   place
	flags   *rootFlags
}

// The most the API gives in one answer: fewer round trips for a long file or a full directory.
const (
	textAtOnce    = 1 << 20
	entriesAtOnce = 1000
)

// read asks for the place; more says where to go on, in a file or in a directory's entries.
func (f *files) read(ctx context.Context, more pethost.ReadPathRequest) (*pethost.ReadPathResponse, error) {
	more.ProjectID, more.Root, more.Path = f.project.ID, f.where.readRoot(), f.where.path
	more.LengthBytes, more.EntryLimit = textAtOnce, entriesAtOnce
	return f.client.ReadPath(ctx, &more)
}

func (f *files) show(ctx context.Context, first *pethost.ReadPathResponse) error {
	kind := pethost.FileTypeUnspecified
	if first.Entry != nil {
		kind = first.Entry.Type
	}

	switch {
	case f.a.JSON:
		return f.eachAnswer(ctx, first, func(answer *pethost.ReadPathResponse) error { return f.a.PrintJSON(answer) })
	case kind == pethost.FileTypeDirectory:
		return f.list(ctx, first)
	case first.Binary:
		return f.binary(ctx, first)
	case kind == pethost.FileTypeFile:
		return f.print(ctx, first)
	}
	return fail.Failed("%s is neither a file nor a directory: there is nothing to show of it.", ui.Clean(f.where.String()))
}

// eachAnswer gives the first answer and every one that follows it to use: a directory's next
// pages, a file's next pieces.
func (f *files) eachAnswer(ctx context.Context, first *pethost.ReadPathResponse, use func(*pethost.ReadPathResponse) error) error {
	answer := first
	for {
		if err := use(answer); err != nil {
			return err
		}

		var more pethost.ReadPathRequest
		switch {
		case answer.NextEntryPageToken != "":
			more.EntryPageToken = answer.NextEntryPageToken
		case answer.NextOffsetBytes != 0:
			more.OffsetBytes = answer.NextOffsetBytes
		default:
			return nil
		}

		next, err := f.read(ctx, more)
		if err != nil {
			return err
		}
		answer = next
	}
}

// entriesShown bounds a listing for people: past it the directory is one to narrow, not to read.
const entriesShown = 5000

// list writes a directory's entries: directories first, then by name, as the machine orders them.
func (f *files) list(ctx context.Context, first *pethost.ReadPathResponse) error {
	out := f.a.UI.Out
	f.sayWhere(first)

	table := out.Table()
	shown := uint32(0)
	err := f.eachAnswer(ctx, first, func(answer *pethost.ReadPathResponse) error {
		for _, entry := range answer.Entries {
			table.Row(entryName(out, entry), entrySize(entry), out.Dim(out.When(entry.ModifyTime)))
		}
		shown += uint32(len(answer.Entries))
		if shown >= entriesShown {
			return errEnough
		}
		return nil
	})
	if err != nil && !errors.Is(err, errEnough) {
		return err
	}
	table.Flush()

	switch {
	case first.EntryCount == 0:
		f.a.UI.Info("%s is empty.", ui.Clean(f.where.String()))
	case shown < first.EntryCount:
		f.a.UI.Info("These are %d of its %d entries: a path further in shows fewer.", shown, first.EntryCount)
	}
	return nil
}

// errEnough ends a listing that has shown as many entries as a person reads.
var errEnough = errors.New("enough entries")

// entryName is an entry's name as a listing shows it: a directory ends in a slash, a symlink
// points at its target.
func entryName(out *ui.Stream, entry pethost.FileEntry) string {
	name := ui.Clean(entry.Name)
	switch entry.Type {
	case pethost.FileTypeDirectory:
		return out.Bold(name + "/")
	case pethost.FileTypeSymlink:
		return name + " → " + ui.Clean(entry.SymlinkTarget)
	}
	return name
}

func entrySize(entry pethost.FileEntry) string {
	if entry.Type != pethost.FileTypeFile {
		return ""
	}
	return ui.Size(entry.SizeBytes)
}

// sayWhere says, of a path in a container, whether what is there lasts.
func (f *files) sayWhere(answer *pethost.ReadPathResponse) {
	if said := lasts(answer); said != "" {
		f.a.UI.Info("%s", f.a.UI.Err.Dim(ui.Clean(f.where.String())+" is "+said+"."))
	}
}

// print writes a text file, whole. A person's terminal gets it without what a terminal would
// obey, and with its last line ended; a program gets it as it is.
func (f *files) print(ctx context.Context, first *pethost.ReadPathResponse) error {
	out := f.a.UI.Out
	f.sayWhere(first)

	endsLine := true
	text := out.Cleaned()
	err := f.eachAnswer(ctx, first, func(answer *pethost.ReadPathResponse) error {
		if answer.Text != "" {
			endsLine = strings.HasSuffix(answer.Text, "\n")
		}
		_, err := io.WriteString(text, answer.Text)
		return err
	})
	closeErr := text.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil && out.Terminal && !endsLine {
		_, err = io.WriteString(out, "\n")
	}
	return err
}

// binary is a file that is not text: the API reads none of it. A program gets its bytes, by a
// download straight from the machine; a terminal gets the command that copies it.
func (f *files) binary(ctx context.Context, first *pethost.ReadPathResponse) error {
	if f.a.UI.Out.Terminal {
		size := ""
		if first.Entry != nil {
			size = " (" + ui.Size(first.Entry.SizeBytes) + ")"
		}
		return fail.Failed("%s is not text%s: a terminal cannot show it.", ui.Clean(f.where.String()), size).
			WithLead("To copy it here, run:", "pethost cp "+f.where.asArgument()+" ."+f.flags.words()+f.project.Flag())
	}

	ticket, err := f.client.CreateTransfer(ctx, &pethost.CreateTransferRequest{
		Transfer: pethost.CreateTransferDownload{ProjectID: f.project.ID, Root: f.where.downloadRoot(), Path: f.where.path},
	})
	if err != nil {
		return err
	}
	response, err := transfer.Do(ctx, ticket, nil, 0)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if _, err := io.Copy(f.a.UI.Out, response.Body); err != nil {
		return fail.Failed("The file stopped coming from your machine: %s.", transfer.Cause(err))
	}
	return nil
}
