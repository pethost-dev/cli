package sandbox

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/pethost-dev/sdk/go/sandboxes"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/target"
	"github.com/pethost-dev/cli/internal/ui"
)

// An agent's turn is followed by its conversation's stream, WatchConversation, keeping the
// events of this turn alone: its messages to stdout, its tool calls a line each on stderr, until
// the turn's end. A stream that breaks is opened again after the last event it gave; past a few
// breaks the turn is waited for by GetAgentTurn, and its last message shown.

// reopensAtMost bounds how many times a broken stream is opened again.
const reopensAtMost = 5

// turning is one turn of an agent in a sandbox.
type turning struct {
	a  *app.App
	c  *sandboxes.Client
	id string
	// The turn's id and its conversation's, once it has begun.
	turn, conversation string
}

// following prompts the agent, writes what it does as it comes, and ends as the turn did.
func (t *turning) following(ctx context.Context, request *sandboxes.PromptAgentRequest) error {
	request.WaitSeconds = new(uint32(0))
	turn, err := t.c.PromptAgent(ctx, request)
	if err != nil {
		return about(t.id, err)
	}
	t.turn, t.conversation = turn.TurnID, turn.ConversationID
	t.a.UI.Info("%s", t.a.UI.Err.Dim(word(turn.Agent)+" · conversation "+ui.Clean(turn.ConversationID)))

	ended, err := t.watch(ctx)
	if err != nil {
		return t.interrupted(ctx, err)
	}
	if ended != nil {
		return t.ended(ended, true)
	}
	for !over(turn) {
		turn, err = t.c.GetAgentTurn(ctx, &sandboxes.GetAgentTurnRequest{SandboxID: t.id, TurnID: t.turn})
		if err != nil {
			return t.interrupted(ctx, about(t.id, err))
		}
	}
	return t.ended(turn, false)
}

// watch writes the turn's events until its end, which it returns. nil and no error = the stream
// broke more often than it is opened again.
func (t *turning) watch(ctx context.Context) (*sandboxes.AgentTurn, error) {
	out := t.a.UI.Out.Cleaned()
	defer out.Close()
	var after int64
	for range reopensAtMost + 1 {
		var broke error
		request := &sandboxes.WatchConversationRequest{SandboxID: t.id, ConversationID: t.conversation, AfterSeq: after}
		for event, err := range t.c.WatchConversation(ctx, request) {
			if err != nil {
				broke = err
				break
			}
			after = event.Seq
			if event.TurnID != t.turn {
				continue
			}
			if ended := t.show(out, event); ended != nil {
				return ended, nil
			}
		}
		if ctx.Err() != nil || refused(broke) {
			return nil, cmp.Or(ctx.Err(), about(t.id, broke))
		}
		t.a.Debugf("the conversation's stream broke: %v", broke)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return nil, nil
}

// show writes one event of the turn; the turn as it ended for its last.
func (t *turning) show(out io.Writer, event *sandboxes.AgentEvent) *sandboxes.AgentTurn {
	switch e := event.Event.(type) {
	case sandboxes.AgentEventMessage:
		_, _ = io.WriteString(out, strings.TrimRight(string(e), "\n")+"\n")
	case sandboxes.AgentEventToolCall:
		line := "› " + target.OneLine(e.Name)
		if summary := firstString(e.Input); summary != "" {
			line += ": " + summary
		}
		t.a.UI.Info("%s", t.a.UI.Err.Dim(cut(line, t.a.UI.Err.Width())))
	case sandboxes.AgentEventTurnEnd:
		turn := sandboxes.AgentTurn(e)
		return &turn
	}
	return nil
}

// firstString is a tool call's input in a few words: the first string among its JSON object's
// fields, as a command's or a path, on one line.
func firstString(input string) string {
	decoder := json.NewDecoder(strings.NewReader(input))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return ""
	}
	for decoder.More() {
		if _, err := decoder.Token(); err != nil {
			return ""
		}
		var value any
		if err := decoder.Decode(&value); err != nil {
			return ""
		}
		if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
			return target.OneLine(text)
		}
	}
	return ""
}

// cut is a line within width columns, an ellipsis where it was cut.
func cut(line string, width int) string {
	runes := []rune(line)
	if width < 10 || len(runes) <= width {
		return line
	}
	return string(runes[:width-1]) + "…"
}

// refused says whether a stream ended with the API's refusal, which opening it again would get
// again, not with a broken connection.
func refused(err error) bool {
	api, ok := errors.AsType[*sandboxes.Error](err)
	if !ok || api.Unwrap() != nil {
		return false
	}
	switch api.Code {
	case sandboxes.CodeUnavailable, sandboxes.CodeUnknown, sandboxes.CodeInternal, sandboxes.CodeDeadlineExceeded:
		return false
	}
	return true
}

// interrupted is the end of a turn that was not followed to its end: on Ctrl-C the turn is
// stopped first, so that no agent works on that nobody watches.
func (t *turning) interrupted(ctx context.Context, err error) error {
	if ctx.Err() == nil || t.turn == "" {
		return err
	}
	stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), stopWait)
	defer cancel()
	if _, stopErr := t.c.StopAgentTurn(stopCtx, &sandboxes.StopAgentTurnRequest{SandboxID: t.id, TurnID: t.turn}); stopErr != nil {
		return fail.Failed("Ctrl-C did not stop the agent's turn, which may go on in the sandbox: %s", fail.From(stopErr).Message)
	}
	t.a.UI.Info("Stopped the agent's turn.")
	return fail.Interrupted()
}
