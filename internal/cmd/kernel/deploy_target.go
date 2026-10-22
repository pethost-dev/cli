package kernel

import (
	"cmp"
	"errors"
	"path/filepath"
	"slices"
	"strings"

	pethost "github.com/pethost-dev/sdk/go"

	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/target"
	"github.com/pethost-dev/cli/internal/ui"
)

// Which project a deploy means, and whether it is new. A folder with a .pethost is that
// project's new version; -p or PETHOST_PROJECT names the project outright; a folder with neither
// is always a new project, named once, on the one thing that never changes: its id, which
// becomes its address.

func (d *deploy) chooseProject() error {
	if named, ok := target.NamedOutright(d.a); ok {
		target.Announce(d.a, named)
		d.settle(named)
		return nil
	}
	if d.file == nil {
		d.writeFile = true
		return d.nameNew("")
	}

	state := d.file.On(d.machine)
	if state == target.Current {
		d.project = target.Project{ID: d.file.ProjectID, Source: target.FromFile, File: d.file}
		target.Announce(d.a, d.project)
		d.sayFolderAbove()
		return nil
	}
	d.sayFolderAbove()
	return d.staleFile(state)
}

// sayFolderAbove says which folder is deployed when it is not the one the person stands in.
func (d *deploy) sayFolderAbove() {
	if d.fromAbove {
		d.a.UI.Info("Deploying %s, the folder of the %s above this one.", d.dir, target.FileName)
	}
}

// settle takes a project named outright: a new one is created without a question. The folder
// gets a .pethost if it has none.
func (d *deploy) settle(named target.Project) {
	d.project = named
	d.create = !d.exists(named.ID)
	d.writeFile = d.file == nil
	d.sayNew()
}

// sayNew says that the deploy makes a new project, where no question has said so.
func (d *deploy) sayNew() {
	if d.create {
		d.a.UI.Info("A new project: %s", d.a.UI.Err.Bold(ui.Clean(d.project.ID)))
	}
}

// exists says whether the machine has a project of this id, by the list it gave when the
// deploy began. A project made or deleted since is the machine's to refuse.
func (d *deploy) exists(id string) bool {
	return slices.ContainsFunc(d.machine.Projects, func(project pethost.ProjectSummary) bool {
		return project.ProjectID == id
	})
}

// staleFile handles a .pethost that does not stand for a project of this machine: it is asked
// about, never deployed over a namesake by itself.
func (d *deploy) staleFile(state target.State) error {
	says := d.file.Says(state, d.machine)
	asNamed := d.a.CommandLine("-p", d.file.ProjectID)
	d.writeFile = true

	if state == target.Namesake {
		yes, err := d.a.UI.Confirm(says+" Is this folder its new version?", asNamed)
		if err != nil {
			return err
		}
		if yes {
			d.project = target.Project{ID: d.file.ProjectID, Source: target.Picked}
			return nil
		}
		return d.nameNew("")
	}

	yes, err := d.a.UI.Confirm(says+" Deploy this folder as a new project?", asNamed)
	if err != nil {
		return err
	}
	if !yes {
		return fail.Declined("Nothing was deployed.")
	}
	return d.nameNew(d.file.ProjectID)
}

// nameNew asks what a new project is called. A name that is taken is asked about: this folder
// may be that project's new version.
func (d *deploy) nameNew(suggested string) error {
	if suggested == "" {
		suggested = nameFor(d.dir)
	}
	for {
		id, err := d.askName(suggested)
		if err != nil {
			return err
		}

		if !d.exists(id) {
			d.project, d.create = target.Project{ID: id, Source: target.Picked}, true
			return nil
		}

		yes, err := d.a.UI.Confirm(ui.Clean(id)+" is already on your machine. Is this folder its new version?", d.a.CommandLine("-p", id))
		if err != nil {
			return err
		}
		if yes {
			d.project = target.Project{ID: id, Source: target.Picked}
			return nil
		}
		suggested = ""
	}
}

// refusedName is the machine's sentence when it cannot take a request's values, as a new
// project's id; empty for any other end. A failure below the API may carry the same code: it
// has a cause, which a refusal has not.
func refusedName(err error) string {
	if api, ok := errors.AsType[*pethost.Error](err); ok && api.Code == pethost.CodeInvalidArgument && api.Unwrap() == nil {
		return ui.Clean(api.Message)
	}
	return ""
}

// askName asks for the new project's id, shown with the address it will have. Off a terminal
// nothing is asked: --yes takes the folder's name, and without it the command to run is named.
func (d *deploy) askName(suggested string) (string, error) {
	if d.a.UI.CanAsk() {
		return d.a.UI.Ask("Name it", suggested, d.addressHint)
	}
	if d.a.UI.Yes && suggested != "" {
		after := "this folder"
		if suggested != nameFor(d.dir) {
			after = "its " + target.FileName
		}
		d.a.UI.Info("A new project, named after %s: %s", after, d.a.UI.Err.Bold(suggested))
		return suggested, nil
	}

	choice := fail.Choice("This folder is not a project yet. Its name becomes its address and never changes: give it one.",
		d.a.CommandLine("-p", cmp.Or(suggested, "<name>")))
	choice.Lead = "Run:"
	return "", choice
}

// addressHint says where a project of this id will live, when the machine gives it its first
// address.
func (d *deploy) addressHint(id string) string {
	domain := appsDomain(d.machine.Machine)
	if d.private || id == "" || domain == "" {
		return ""
	}
	return "it will live at https://" + id + "." + domain
}

// appsDomain is the account's domain, cleaned to be printed; empty without a machine.
func appsDomain(machine *pethost.Machine) string {
	if machine == nil {
		return ""
	}
	return ui.Clean(machine.AppsDomain)
}

// nameFor suggests an id from a folder's name: its letters and digits in lower case, anything
// else a hyphen. It is a suggestion only: the machine holds the rule and checks what is typed.
func nameFor(dir string) string {
	var id strings.Builder
	for _, r := range strings.ToLower(filepath.Base(dir)) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			id.WriteRune(r)
		case id.Len() > 0 && !strings.HasSuffix(id.String(), "-"):
			id.WriteByte('-')
		}
	}
	return strings.TrimRight(id.String(), "-")
}
