package workspace

import (
	"cmp"
	"context"
	"fmt"
	"strings"

	pethost "github.com/pethost-dev/sdk/go"

	"github.com/pethost-dev/cli/internal/operation"
	"github.com/pethost-dev/cli/internal/say"
	"github.com/pethost-dev/cli/internal/target"
	"github.com/pethost-dev/cli/internal/ui"
)

// Where a move landed: the folder's .pethost written for the new workspace, what changed and
// what stayed behind, the project's address alone on stdout, and what to type next.

func (m *move) land(ctx context.Context, done *operation.Result) error {
	machine, err := m.a.Client().GetMachine(ctx, &pethost.GetMachineRequest{Workspace: m.to})
	if err != nil {
		return err
	}
	m.rewriteFile(machine)
	final := m.final(ctx)

	if m.a.JSON {
		if final != nil {
			return m.a.PrintJSON(final)
		}
		return m.a.PrintJSON(done.Answer)
	}

	there := m.project
	if there.Source != target.FromFile && machine.Workspace != nil {
		there.Workspace = machine.Workspace.WorkspaceID
	}
	m.say(machine, done, final)
	m.showAddress(machine)
	m.a.UI.Next("pethost status"+there.Flag(), "pethost open"+there.Flag())
	return nil
}

// final is the move's own answer now that it ended, with the projects as they are there and
// what changed, asked again by its operation id; nil when it did not come.
func (m *move) final(ctx context.Context) *pethost.MoveProjectResponse {
	answer, err := m.call(ctx, new(uint32(0)))
	if err != nil {
		m.a.Debugf("the move's answer, asked again: %s", err)
		return nil
	}
	return answer
}

// rewriteFile writes the folder's .pethost for the workspace the project is in now, when it
// named the project.
func (m *move) rewriteFile(machine *pethost.GetMachineResponse) {
	if m.project.Source != target.FromFile {
		return
	}
	file, ok := target.FileFrom(m.project.File.Dir, machine, m.project.ID)
	if !ok {
		return
	}
	if err := target.WriteFile(file); err != nil {
		m.a.UI.Problem("%s could not be written here: %s", target.FileName, err)
	}
}

// say writes how the move ended and what it changed, in the CLI's words: MoveMessage is written
// for an agent ("add the person's with RunMachineAction add_ssh_key"). The project as it is in
// the destination says its addresses there; the destination's facts say the rest.
func (m *move) say(machine *pethost.GetMachineResponse, done *operation.Result, final *pethost.MoveProjectResponse) {
	screen := m.a.UI
	where := ui.Clean(m.to)
	if machine.Workspace != nil {
		where = say.WorkspaceName(machine.Workspace)
	}
	screen.Done("%s", fmt.Sprintf("%s moved to %s in %s", ui.Clean(m.project.ID), where, done.Took()))

	if final != nil && final.Project != nil && machine.Workspace != nil {
		sayChanges(screen, final.Project, machine.Workspace)
	}
	screen.Info("  Stayed behind: the operation history, logs, incidents and uptime, and the snapshots taken before the move.")
	if m.project.Source == target.FromFile {
		screen.Info("This folder's %s names the new workspace now (commit it).", target.FileName)
	}
}

// sayChanges says the moved project's addresses under the destination's domain, the person's
// own domains, which answer there once their CNAME points at its machine, and where SSH logs in.
func sayChanges(screen *ui.Screen, project *pethost.Project, to *pethost.Workspace) {
	var ours, own []string
	for _, host := range project.Hosts {
		if to.AppsDomain != "" && strings.HasSuffix(host.Host, "."+to.AppsDomain) {
			ours = append(ours, ui.Clean(cmp.Or(host.URL, "https://"+host.Host)))
		} else {
			own = append(own, ui.Clean(host.Host))
		}
	}
	if len(ours) > 0 {
		screen.Info("  Its new addresses: %s", strings.Join(ours, ", "))
	}
	if to.Hostname == "" {
		return
	}
	if len(own) > 0 {
		screen.Info("  Your own domains answer there once their CNAME points at %s: %s.", ui.Clean(to.Hostname), strings.Join(own, ", "))
	}
	screen.Info("  SSH, SFTP and tunnels log in at %s, with that workspace's SSH keys.", ui.Clean(to.Hostname))
}

// showAddress writes the project's address there alone on stdout, when it has one.
func (m *move) showAddress(machine *pethost.GetMachineResponse) {
	for _, project := range machine.Projects {
		if project.ProjectID == m.project.ID && project.URL != "" {
			fmt.Fprintln(m.a.UI.Out, ui.Clean(project.URL))
		}
	}
}
