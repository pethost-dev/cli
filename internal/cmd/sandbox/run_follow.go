package sandbox

import (
	"context"
	"io"
	"time"

	"github.com/pethost-dev/sdk/go/sandboxes"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/ui"
)

// A command that runs in a sandbox is followed by its output's stream, WatchCommand, its bytes
// written as they come. A stream that breaks before the command's end is followed by
// GetCommand's waits instead; the sandbox keeps the last 32 KiB of each stream, so a long wait
// may cut the output between.
//
// The stream's messages say where in the output they are from in nothing: it begins at the
// output's last 64 KiB and skips ahead past what its reader fell behind, so the bytes it gave
// are no offset GetCommand could go on from. The waits go on from what the command has written
// by the time the stream broke, as the sandbox counts it, and what the stream did not give is
// said, never written twice: the command's own counts, against the bytes written here.

// running is one command run in a sandbox.
type running struct {
	a  *app.App
	c  *sandboxes.Client
	id string
	// The command's id, once it has started.
	command string
	// The bytes of each stream written so far: the stream's, then, once the waits follow the
	// command, the offsets GetCommand answered last, which count what was not written too.
	stdout, stderr int64
	// The output was said to be cut.
	saidCut bool
}

// stopWait bounds the call that stops a command after Ctrl-C: the sandbox kills it 10 s after
// asking it to end.
const stopWait = 15 * time.Second

// following starts the command, writes its output as it comes, and ends as it did.
func (r *running) following(ctx context.Context, request *sandboxes.RunCommandRequest) error {
	request.WaitSeconds = new(uint32(0))
	command, err := r.c.RunCommand(ctx, request)
	if err != nil {
		return about(r.id, err)
	}
	r.command = command.CommandID

	out, errOut := r.a.UI.Out.Cleaned(), r.a.UI.Err.Cleaned()
	command, err = r.watch(ctx, out, errOut)
	if err == nil && command == nil {
		command, err = r.wait(ctx, out, errOut)
	}
	_ = out.Close()
	_ = errOut.Close()
	if err != nil {
		return r.interrupted(ctx, err)
	}
	return r.ended(command)
}

// watch writes the command's output from its stream until its end, which it returns. nil and no
// error = the stream broke first; an error is only Ctrl-C's.
func (r *running) watch(ctx context.Context, out, errOut io.Writer) (*sandboxes.Command, error) {
	request := &sandboxes.WatchCommandRequest{SandboxID: r.id, CommandID: r.command}
	for output, err := range r.c.WatchCommand(ctx, request) {
		if err != nil {
			r.a.Debugf("the command's stream broke: %v", err)
			return nil, ctx.Err()
		}
		switch piece := output.Output.(type) {
		case sandboxes.CommandOutputStdout:
			r.stdout += int64(len(piece))
			_, _ = out.Write(piece)
		case sandboxes.CommandOutputStderr:
			r.stderr += int64(len(piece))
			_, _ = errOut.Write(piece)
		case sandboxes.CommandOutputEnded:
			ended := sandboxes.Command(piece)
			r.sayUnwritten(&ended, "the stream skipped it")
			return &ended, nil
		}
	}
	return nil, ctx.Err()
}

// wait follows the command by GetCommand's waits until its end, from what it has written by now:
// that is said, not written, since the stream's bytes are no offset to go on from.
func (r *running) wait(ctx context.Context, out, errOut io.Writer) (*sandboxes.Command, error) {
	command, err := r.c.GetCommand(ctx, &sandboxes.GetCommandRequest{SandboxID: r.id, CommandID: r.command, WaitSeconds: new(uint32(0))})
	if err != nil {
		return nil, about(r.id, err)
	}
	r.sayUnwritten(command, "its stream broke")
	r.stdout, r.stderr = command.StdoutBytes, command.StderrBytes

	for command.State == sandboxes.CommandStateRunning {
		command, err = r.c.GetCommand(ctx, &sandboxes.GetCommandRequest{
			SandboxID: r.id, CommandID: r.command, StdoutAfterBytes: r.stdout, StderrAfterBytes: r.stderr,
		})
		if err != nil {
			return nil, about(r.id, err)
		}
		if command.OutputTruncated && !r.saidCut {
			r.saidCut = true
			r.a.UI.Info("Some of the output was cut: the sandbox keeps the last 32 KiB of it.")
		}
		_, _ = io.WriteString(out, command.Stdout)
		_, _ = io.WriteString(errOut, command.Stderr)
		r.stdout, r.stderr = command.StdoutBytes, command.StderrBytes
	}
	return command, nil
}

// sayUnwritten says how much of the output the stream did not write here, and why: what the
// command wrote, as the sandbox counts it, past the bytes the stream gave, each given once.
func (r *running) sayUnwritten(command *sandboxes.Command, why string) {
	unwritten := command.StdoutBytes - r.stdout + command.StderrBytes - r.stderr
	if unwritten > 0 {
		r.a.UI.Info("%s of the output is not shown: %s.", ui.Size(uint64(unwritten)), why)
	}
}

// write writes the output a command's answer holds, for a command that ended before it was
// followed; --json printed it already.
func (r *running) write(command *sandboxes.Command) {
	if r.a.JSON {
		return
	}
	out, errOut := r.a.UI.Out.Cleaned(), r.a.UI.Err.Cleaned()
	_, _ = io.WriteString(out, command.Stdout)
	_, _ = io.WriteString(errOut, command.Stderr)
	_ = out.Close()
	_ = errOut.Close()
}

// interrupted is the end of a command that was not followed to its end: on Ctrl-C the command
// is stopped first, so that nothing runs on that nobody watches.
func (r *running) interrupted(ctx context.Context, err error) error {
	if ctx.Err() == nil || r.command == "" {
		return err
	}

	stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), stopWait)
	defer cancel()
	_, stopErr := r.c.StopCommand(stopCtx, &sandboxes.StopCommandRequest{SandboxID: r.id, CommandID: r.command})
	if stopErr != nil {
		return fail.Failed("Ctrl-C did not stop the command, which may run on in the sandbox: %s", fail.From(stopErr).Message)
	}
	r.a.UI.Info("Stopped the command.")
	return fail.Interrupted()
}

// ended is how run ends for a command that ended: as the command did.
func (r *running) ended(command *sandboxes.Command) error {
	code := 0
	if command.ExitCode != nil {
		code = int(*command.ExitCode)
	}
	switch command.State {
	case sandboxes.CommandStateTimedOut:
		if !r.a.JSON {
			r.a.UI.ErrLine(r.a.UI.Err.Cross() + "The command was stopped at its timeout.")
		}
		return fail.TimedOut("The command was stopped at its timeout.")
	case sandboxes.CommandStateLost:
		return &fail.Error{Exit: fail.ExitOwn, Code: "COMMAND_LOST",
			Message: "The command's end is not known: the sandbox stopped while it ran, or what ran it restarted."}
	case sandboxes.CommandStateExited, sandboxes.CommandStateStopped:
		if code != 0 {
			return fail.Ended(code, "The command exited with code %d.", code)
		}
		return nil
	}
	return &fail.Error{Exit: fail.ExitOwn, Code: "COMMAND_UNKNOWN", Message: "The sandbox answered with a state of the command this pethost does not know: update it (pethost update)."}
}
