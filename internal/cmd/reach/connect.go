package reach

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"

	pethost "github.com/pethost-dev/sdk/go"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/remote"
	"github.com/pethost-dev/cli/internal/target"
	"github.com/pethost-dev/cli/internal/ui"
)

// The shell and the tunnel reach a container over SSH, inside the binary. The API says where:
// the machine's name, the host key to expect there and the keys it takes; the login is
// "<service>.<project id>", as the API's documentation writes Service.ssh_command.

// facts is what the API says of a project and of the machine it runs on, asked for at once.
type facts struct {
	machine *pethost.Machine
	project *pethost.Project
}

func readFacts(ctx context.Context, a *app.App, project target.Project) (facts, error) {
	machine, answer, err := a.MachineAndProject(ctx, project.ID)
	if err != nil {
		return facts{}, err
	}
	if machine.Machine == nil || answer.Project == nil {
		return facts{}, fail.Failed("The API's answer has no machine or no project in it.")
	}
	return facts{machine: machine.Machine, project: answer.Project}, nil
}

// serviceOf is the project's service of that name; nil = it has none.
func serviceOf(project *pethost.Project, name string) *pethost.Service {
	for i := range project.Services {
		if project.Services[i].Service == name {
			return &project.Services[i]
		}
	}
	return nil
}

// dialService opens SSH to a service's container with this computer's key, which the machine is
// given first if it does not have it yet.
func dialService(ctx context.Context, a *app.App, f facts, service string) (*remote.Conn, error) {
	machine := f.machine
	if machine.Hostname == "" || machine.SSHHostKeyFingerprint == "" {
		return nil, fail.Failed("Your machine has not said its name and its SSH key yet, so nothing can connect to it safely.")
	}

	key, err := remote.OwnKey()
	if err != nil {
		return nil, err
	}
	if err := giveKey(ctx, a, machine, key); err != nil {
		return nil, err
	}

	a.Debugf("ssh to %s", machine.Hostname)
	conn, err := remote.Dial(ctx, remote.Target{
		Host:               machine.Hostname,
		User:               service + "." + f.project.ProjectID,
		HostKeyFingerprint: machine.SSHHostKeyFingerprint,
	}, key)
	if err != nil {
		return nil, dialFailure(machine, err)
	}
	return conn, nil
}

// giveKey adds this computer's key to the machine unless it is among the keys it takes, and
// says so: the key opens every container, and the person should know it is there.
func giveKey(ctx context.Context, a *app.App, machine *pethost.Machine, key *remote.Key) error {
	known := slices.ContainsFunc(machine.SSHKeys, func(listed pethost.SSHKey) bool {
		return listed.Fingerprint == key.Fingerprint
	})
	if known {
		return nil
	}

	client, err := a.Client()
	if err != nil {
		return err
	}
	_, err = client.RunMachineAction(ctx, &pethost.RunMachineActionRequest{
		Action: pethost.RunMachineActionAddSSHKey{PublicKey: key.PublicKey, Label: keyLabel()},
	})
	if err != nil {
		return err
	}
	a.UI.Info("Added this computer's key to your machine: it opens every container. See it: %s", a.UI.Err.Bold("pethost machine"))
	return nil
}

// keyLabel is what the machine's list of keys calls this one: the computer's name, without the
// domain a network gave it ("sams-macbook", not "sams-macbook.local").
func keyLabel() string {
	name, err := os.Hostname()
	name, _, _ = strings.Cut(name, ".")
	name = strings.Join(strings.Fields(name), " ")
	if err != nil || name == "" {
		return "pethost CLI"
	}
	return name + " (pethost CLI)"
}

// dialFailure is a connection that could not be made, in the words of what a person can do
// about it.
func dialFailure(machine *pethost.Machine, err error) error {
	failure, ok := errors.AsType[*remote.DialError](err)
	if !ok {
		return err
	}

	host := ui.Clean(machine.Hostname)
	switch failure.Kind {
	case remote.ErrWrongHost:
		return fail.Failed("What answers at %s is not your machine: its SSH key is not the one your machine reports. Nothing was sent to it.", host)
	case remote.ErrRefused:
		return fail.Failed("Your machine did not take this computer's key.").WithLead("The keys it takes:", "pethost machine")
	}
	e := fail.Failed("Your machine's SSH does not answer at %s: %s. Try again in a minute.", host, fail.Innermost(failure.Cause))
	e.Exit = fail.ExitLater
	return e
}

// lostConnection is the end of a shell or a tunnel whose connection ended under it.
func lostConnection(a *app.App, exit int, message string) error {
	return &fail.Error{Exit: exit, Code: "CONNECTION_LOST", Message: message, Next: a.CommandLine(), Lead: "To connect again, run:"}
}
