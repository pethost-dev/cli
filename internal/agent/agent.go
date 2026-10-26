// Package agent is the coding agents a person may have on this computer, Claude Code, Codex and
// pi: which are installed, the one the person chose to write a project's files where pethost
// cannot tell how the project runs, and one run of it in a folder.
//
// An agent runs as its own program, as the person set it up and signed it in, with the
// narrowest rights its program has for the job, which is to read the folder and write files in
// it; what each can still reach beyond the folder is said where its arguments are. Its task
// goes in on stdin, so that no command line has to carry a long text.
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/pethost-dev/cli/internal/kept"
)

// An Agent is a coding agent's program.
type Agent struct {
	// What it is called on the screen: "Claude Code".
	Name string
	// Its program, as PATH has it: "claude".
	Program string
	// The arguments of a run that takes its task from stdin, works in the current directory
	// and ends by itself.
	Arguments []string
}

// known are the agents pethost can run, in the order they are offered.
var known = []Agent{
	{
		Name:    "Claude Code",
		Program: "claude",
		// It writes files in the folder unasked, and runs no command: nobody is there to be
		// asked about one. Of settings it takes the person's own alone, and no MCP server:
		// a folder's own settings can name commands to run, and the folder may be anybody's.
		Arguments: []string{"-p", "--permission-mode", "acceptEdits", "--disallowedTools", "Bash",
			"--setting-sources", "user", "--strict-mcp-config"},
	},
	{
		Name:    "Codex",
		Program: "codex",
		// Its sandbox lets what it runs write in the folder alone and reach no network.
		Arguments: []string{"exec", "--sandbox", "workspace-write", "--skip-git-repo-check", "-"},
	},
	{
		Name:    "pi",
		Program: "pi",
		// pi has no sandbox: it gets the tools that read and write files, and no shell.
		Arguments: []string{"-p", "--tools", "read,grep,find,ls,edit,write"},
	},
}

// Variable names the agent for this run, by its program, whatever was chosen before; "none"
// = no agent is run and none is offered.
const Variable = "PETHOST_AGENT"

// Installed are the agents whose program PATH has, in the order they are offered.
func Installed() []Agent {
	var found []Agent
	for _, agent := range known {
		if _, err := exec.LookPath(agent.Program); err == nil {
			found = append(found, agent)
		}
	}
	return found
}

// Off says whether PETHOST_AGENT turns the agents off: none is run and none is offered.
func Off() bool {
	return os.Getenv(Variable) == "none"
}

// Chosen is the agent that writes without a question: the one PETHOST_AGENT names, else the
// one the person chose before, if this computer still has it. byVariable = PETHOST_AGENT named
// it.
func Chosen() (agent Agent, ok, byVariable bool) {
	program := os.Getenv(Variable)
	byVariable = program != ""
	if !byVariable {
		program = remembered()
	}
	for _, agent := range Installed() {
		if agent.Program == program {
			return agent, true, byVariable
		}
	}
	return Agent{}, false, byVariable
}

// CommandLine is the command a run of the agent is, as a person would type it: the task, which
// goes in on stdin, is not part of it.
func (a Agent) CommandLine() string {
	return a.Program + " " + strings.Join(a.Arguments, " ")
}

// saidLimit bounds what is kept of an agent's output: its last words are a few lines.
const saidLimit = 64 << 10

// leftAlone is how long a run that was interrupted is waited for, once its program was told
// to end: a program it started may hold its output open for longer.
const leftAlone = 5 * time.Second

// Run gives the agent a task in a folder and waits for its end. said is what it wrote to
// stdout, which for each of them is its own account of what it did. The agent gets this
// process's environment less Pethost's token: it reads a folder that may be anybody's, and
// has no call to make.
func (a Agent) Run(ctx context.Context, dir, task string) (said string, err error) {
	command := exec.CommandContext(ctx, a.Program, a.Arguments...)
	command.Dir = dir
	command.Stdin = strings.NewReader(task)
	command.Env = slices.DeleteFunc(os.Environ(), func(variable string) bool {
		return strings.HasPrefix(variable, "PETHOST_TOKEN=")
	})
	command.WaitDelay = leftAlone
	answer, complaints := &tail{}, &tail{}
	command.Stdout, command.Stderr = answer, complaints

	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("%s did not finish (%w): %s", a.Program, err, lastLine(complaints.String(), answer.String()))
	}
	return strings.TrimSpace(answer.String()), nil
}

// lastLine is the last line that says anything, of the first text that has one.
func lastLine(texts ...string) string {
	for _, text := range texts {
		lines := strings.Split(strings.TrimSpace(text), "\n")
		if last := strings.TrimSpace(lines[len(lines)-1]); last != "" {
			return last
		}
	}
	return "it said nothing"
}

// A tail keeps the end of what is written to it, saidLimit at most.
type tail struct {
	kept bytes.Buffer
}

func (t *tail) Write(p []byte) (int, error) {
	t.kept.Write(p)
	if over := t.kept.Len() - saidLimit; over > 0 {
		t.kept.Next(over)
	}
	return len(p), nil
}

func (t *tail) String() string { return t.kept.String() }

// choice is what the CLI remembers of the person's choice, between runs: a file beside the
// sign-in.
type choice struct {
	Program string `json:"program"`
}

const choiceName = "agent.json"

func choiceFile() (string, error) {
	return kept.File(choiceName)
}

// ChoiceFile is where the choice is kept, to tell the person; empty where nothing can be kept.
func ChoiceFile() string {
	path, _ := choiceFile()
	return path
}

// remembered is the program of the person's choice; empty = none is kept.
func remembered() string {
	path, err := choiceFile()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	var chosen choice
	json.Unmarshal(data, &chosen)
	return chosen.Program
}

// Remember keeps the agent as the person's choice for the next runs.
func (a Agent) Remember() error {
	return kept.Write(choiceName, choice{Program: a.Program})
}
