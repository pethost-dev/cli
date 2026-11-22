package sandbox

import (
	"errors"
	"strings"

	"github.com/pethost-dev/sdk/go/sandboxes"

	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/ui"
)

// about is a failed call that named a sandbox, with the next command where the API's detail
// says what it takes: a stopped sandbox is started, a name another sandbox has leads to it, and
// an agent that cannot reach its model is given a key or signed in. Any other failure is returned as it is: internal/fail words it.
func about(id string, err error) error {
	api, ok := errors.AsType[*sandboxes.Error](err)
	if !ok {
		return err
	}
	id = ui.Clean(id)

	switch {
	case api.Stopped != nil:
		e := fail.From(api)
		e.Message = sentence(e.Message)
		e.Lead, e.Next = "Start it:", "pethost sandbox resume "+id
		return e
	case api.NameTaken != nil:
		e := fail.From(api)
		e.Message = sentence(e.Message)
		e.Lead, e.Next = "See it:", "pethost sandbox show "+ui.Clean(api.NameTaken.SandboxID)
		return e
	case api.AgentSignIn != nil:
		e := fail.From(api)
		e.Message = sentence(e.Message)
		e.Next = "pethost sandbox set " + id + " --env " + ui.Clean(api.AgentSignIn.Variable) + "=secret:<secret>\n" +
			"pethost sandbox shell " + id + " -- " + ui.Clean(api.AgentSignIn.SignInCommand)
		return e
	}
	return err
}

// sentence is the API's words as a sentence of their own, which a next command may follow.
func sentence(words string) string {
	return strings.TrimSuffix(strings.TrimSpace(words), ".") + "."
}
