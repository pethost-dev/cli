package sandbox

import (
	"context"
	"io"
	"strings"

	"github.com/pethost-dev/sdk/go/sandboxes"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/ui"
)

// agents are the coding agents a person may name.
var agents = []sandboxes.Agent{sandboxes.AgentClaudeCode, sandboxes.AgentCodex, sandboxes.AgentOpencode}

type agentOptions struct {
	agent, model, conversation, cwd string
}

// agentCommand is `pethost sandbox agent <id> <prompt>`.
func agentCommand(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent <id> <prompt>...",
		Short: "Give a coding agent in a sandbox a prompt, and follow its turn",
		Long: `Gives a coding agent in the sandbox a prompt, in a new conversation or the one
--conversation names, and follows its turn: what it writes goes to stdout, each tool it
uses a line on stderr. The prompt is the words after the id, or stdin for "-".

The agent signs in with a secret bound to its variable (ANTHROPIC_API_KEY for Claude Code,
OPENAI_API_KEY for Codex), or as you signed it in once in the sandbox's terminal. pethost
exits 0 when the turn is done, and 1 when it failed or was stopped. Ctrl-C stops the turn.`,
		Example: `  pethost sandbox agent k7f3q2xy0a1b2c3d 'Add a test for hello.py'
  pethost sandbox agent k7f3q2xy0a1b2c3d --agent codex --cwd /home/user/app 'Fix the build'
  pethost sandbox agent k7f3q2xy0a1b2c3d --conversation c_8f2k3j 'Now run it'
  pethost sandbox agent k7f3q2xy0a1b2c3d - < task.md`,
		Args:              cobra.MinimumNArgs(2),
		ValidArgsFunction: completeSandboxes(a),
	}
	var options agentOptions
	cmd.Flags().StringVar(&options.agent, "agent", "", "claude-code, codex or opencode (default: the conversation's, or claude-code)")
	cmd.Flags().StringVar(&options.model, "model", "", "the model, as the agent names it (default: the agent's)")
	cmd.Flags().StringVar(&options.conversation, "conversation", "", "go on in this conversation (default: a new one)")
	cmd.Flags().StringVar(&options.cwd, "cwd", "", "a new conversation's directory (default /home/user)")

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		request, err := promptRequest(a, args, options)
		if err != nil {
			return err
		}
		c, err := clientForChange(a)
		if err != nil {
			return err
		}
		request.SandboxID = idOf(cmd.Context(), c, args[0])
		t := &turning{a: a, c: c, id: request.SandboxID}
		if a.JSON {
			return t.forJSON(cmd.Context(), request)
		}
		return t.following(cmd.Context(), request)
	}
	return cmd
}

// promptRequest is the request the command line makes.
func promptRequest(a *app.App, args []string, options agentOptions) (*sandboxes.PromptAgentRequest, error) {
	request := &sandboxes.PromptAgentRequest{
		SandboxID: args[0], Prompt: strings.Join(args[1:], " "),
		ConversationID: options.conversation, Model: options.model, Cwd: options.cwd,
	}
	if options.agent != "" {
		agent, err := fromWord("agent", options.agent, agents...)
		if err != nil {
			return nil, err
		}
		request.Agent = agent
	}
	if request.Prompt == streamed {
		piped, err := io.ReadAll(io.LimitReader(a.UI.In, stdinAtMost+1))
		if err != nil {
			return nil, err
		}
		if len(piped) > stdinAtMost {
			return nil, fail.Usage("The prompt piped in is over 1 MiB: put the long part in a file in the sandbox (pethost sandbox cp), and name it in the prompt.", "pethost sandbox agent <id> -")
		}
		request.Prompt = string(piped)
	}
	return request, nil
}

// forJSON waits for the turn's end by the API's own waits, and prints the turn as it ended.
func (t *turning) forJSON(ctx context.Context, request *sandboxes.PromptAgentRequest) error {
	turn, err := t.c.PromptAgent(ctx, request)
	if err != nil {
		return about(t.id, err)
	}
	t.turn = turn.TurnID
	for !over(turn) {
		turn, err = t.c.GetAgentTurn(ctx, &sandboxes.GetAgentTurnRequest{SandboxID: t.id, TurnID: t.turn})
		if err != nil {
			return t.interrupted(ctx, about(t.id, err))
		}
	}
	if err := t.a.PrintJSON(turn); err != nil {
		return err
	}
	return t.ended(turn, false)
}

// over says whether a turn has ended.
func over(turn *sandboxes.AgentTurn) bool {
	return turn.State != sandboxes.AgentTurnStateQueued && turn.State != sandboxes.AgentTurnStateRunning
}

// ended is how the command ends for a turn that ended: 0 when it is done, else 1 with why.
// shown says whether its words were written as they came.
func (t *turning) ended(turn *sandboxes.AgentTurn, shown bool) error {
	if !shown && turn.Reply != "" && !t.a.JSON {
		out := t.a.UI.Out.Cleaned()
		_, _ = io.WriteString(out, strings.TrimRight(turn.Reply, "\n")+"\n")
		_ = out.Close()
	}
	t.a.UI.Info("%s", t.a.UI.Err.Dim(turnWords(turn)))

	switch turn.State {
	case sandboxes.AgentTurnStateDone:
		return nil
	case sandboxes.AgentTurnStateFailed:
		return fail.Failed("The agent's turn failed: %s", ui.Clean(turn.FailureMessage))
	case sandboxes.AgentTurnStateStopped:
		return fail.Failed("The agent's turn was stopped.")
	case sandboxes.AgentTurnStateLost:
		return fail.Failed("The agent's turn's end is not known: the sandbox stopped while it ran.")
	}
	return fail.Failed("The sandbox answered with a state of the turn this pethost does not know: update it (pethost update).")
}

// turnWords is a turn's end in a line: "done · claude-code · conversation c_8f2k3j · 12,345
// tokens in, 1,234 out".
func turnWords(turn *sandboxes.AgentTurn) string {
	parts := []string{word(turn.State), word(turn.Agent), "conversation " + ui.Clean(turn.ConversationID)}
	if turn.InputTokens > 0 || turn.OutputTokens > 0 {
		parts = append(parts, ui.Count(uint64(turn.InputTokens))+" tokens in, "+ui.Count(uint64(turn.OutputTokens))+" out")
	}
	return strings.Join(parts, " · ")
}
