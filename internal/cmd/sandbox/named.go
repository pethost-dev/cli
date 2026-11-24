package sandbox

import (
	"context"
	"regexp"

	"github.com/pethost-dev/sdk/go/sandboxes"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/ui"
)

// idShape is the shape of a sandbox_id, 16 of a-z and 0-9, as the API's Sandbox.sandbox_id says,
// which no name may have (CreateSandboxRequest.name).
var idShape = regexp.MustCompile(`^[a-z0-9]{16}$`)

// idOf is the id of the sandbox that typed names. Typed in the shape of an id, it is that id, and
// is never looked up as a name: no sandbox's name has that shape, so it is never another sandbox's.
// Else it is a name: the workspace's sandbox of exactly that name, which ListSandboxes finds. The
// API takes a name nowhere it takes an id: letting a person type a name is this command line's
// presentation alone, so it asks the one call that knows names, and looks a name up in one
// workspace, the one -w names or the only one, as any list does. A lookup error (such as several
// workspaces with no -w) is returned directly instead of falling through to NOT_FOUND; a lookup
// that finds nothing leaves typed as given so the caller's RPC reports it. A name is unique among
// the sandboxes that are not deleted, which the lookup lists alone, so it finds one at most.
func idOf(ctx context.Context, c *sandboxes.Client, typed string) (string, error) {
	if idShape.MatchString(typed) {
		return typed, nil
	}
	found, err := c.ListSandboxes(ctx, &sandboxes.ListSandboxesRequest{Name: typed, PageSize: 1})
	if err != nil {
		return "", err
	}
	if len(found.Sandboxes) == 0 || found.Sandboxes[0].Name != typed {
		return typed, nil
	}
	return found.Sandboxes[0].SandboxID, nil
}

// sandboxesOf reads, at once, the sandbox each of typed names (idOf), in their order, for a
// command that says what it will act on before it does: the first of typed that names none, or
// whose read fails, ends it, with nothing done.
func sandboxesOf(ctx context.Context, c *sandboxes.Client, typed []string) ([]*sandboxes.Sandbox, error) {
	found := make([]*sandboxes.Sandbox, len(typed))
	calls := make([]func() error, len(typed))
	for i, words := range typed {
		calls[i] = func() error {
			id, err := idOf(ctx, c, words)
			if err != nil {
				return err
			}
			sandbox, err := c.GetSandbox(ctx, &sandboxes.GetSandboxRequest{SandboxID: id})
			if err != nil {
				return about(words, err)
			}
			found[i] = sandbox
			return nil
		}
	}
	if err := app.Together(calls...); err != nil {
		return nil, err
	}
	return found, nil
}

// nameWords is a sandbox as a person reads it: its name and id, or its id alone when it has none.
func nameWords(sandbox *sandboxes.Sandbox) string {
	if sandbox.Name == "" {
		return ui.Clean(sandbox.SandboxID)
	}
	return ui.Clean(sandbox.Name) + " (" + ui.Clean(sandbox.SandboxID) + ")"
}
