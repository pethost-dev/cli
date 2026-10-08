package reach

import (
	"context"
	"slices"
	"strings"

	pethost "github.com/pethost-dev/sdk/go"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/target"
	"github.com/pethost-dev/cli/internal/ui"
)

// A place is a path on the machine as a person writes it, "[name:]path". The API has three
// roots a path may start from, and the name says which: none is the project's own deployed
// files, a service's name its container, a volume's name that volume.
type place struct {
	// The service or the volume. Empty = the project's files.
	name string
	// From the root. Empty = the root itself, as the API reads it.
	path string
	root root
	// Written without a colon, as files takes a path of the project's own.
	bare bool
}

// A root is where a place's path starts.
type root int

const (
	projectFiles root = iota
	inService
	inVolume
)

// parsePlace reads "[name:]path". A colon after a slash is the path's own, as a name has none.
func parsePlace(argument string) place {
	name, path, named := strings.Cut(argument, ":")
	if !named || strings.ContainsAny(name, `/\`) {
		return place{path: argument, bare: true}
	}
	return place{name: name, path: path}
}

// String is the place as it is typed: "web:/app", ":/compose.yaml", or the bare path.
func (p place) String() string {
	if p.bare {
		return p.path
	}
	return p.name + ":" + p.path
}

// asArgument is the place as cp takes it, where a bare path would be this computer's.
func (p place) asArgument() string {
	return app.ShellWord(p.name + ":" + p.path)
}

// rootFlags say which of the two a name is, for the project where a service and a volume share
// one.
type rootFlags struct {
	service, volume bool
}

func addRootFlags(cmd *cobra.Command) *rootFlags {
	flags := &rootFlags{}
	cmd.Flags().BoolVar(&flags.service, "service", false, "the name is a service's, where a volume has the same")
	cmd.Flags().BoolVar(&flags.volume, "volume", false, "the name is a volume's, where a service has the same")
	return flags
}

// words is the flag given, as a command that names the same place again takes it.
func (f *rootFlags) words() string {
	switch {
	case f.service:
		return " --service"
	case f.volume:
		return " --volume"
	}
	return ""
}

// locate finds which root the place's name means. A flag says it outright; else the project's
// own lists of services and volumes do, and a name on both is a choice for the person.
func (p *place) locate(ctx context.Context, a *app.App, client *pethost.Client, project target.Project, flags *rootFlags) error {
	switch {
	case p.name == "":
		p.root = projectFiles
		return nil
	case flags.service && flags.volume:
		return fail.Choice("--service and --volume were both given: " + ui.Clean(p.name) + " is read as one of them.")
	case flags.service:
		p.root = inService
		return nil
	case flags.volume:
		p.root = inVolume
		return nil
	}

	answer, err := client.GetProject(ctx, &pethost.GetProjectRequest{ProjectID: project.ID})
	if err != nil {
		return err
	}
	services, volumes := target.ServiceNames(answer.Project), volumeNames(answer.Project)
	isService, isVolume := slices.Contains(services, p.name), slices.Contains(volumes, p.name)

	switch {
	case isService && isVolume:
		return fail.Choice(ui.Clean(p.name)+" is both a service and a volume of "+ui.Clean(project.ID)+".",
			a.CommandLine("--service"), a.CommandLine("--volume"))
	case isService:
		p.root = inService
	case isVolume:
		p.root = inVolume
	default:
		return fail.Failed("%s has no service and no volume named %s. %s", ui.Clean(project.ID), ui.Clean(p.name), ui.Clean(has(services, volumes)))
	}
	return nil
}

func volumeNames(project *pethost.Project) []string {
	names := make([]string, len(project.Volumes))
	for i, volume := range project.Volumes {
		names[i] = volume.Volume
	}
	return names
}

// has says what a project does have: "Services: web, db. Volumes: data."
func has(services, volumes []string) string {
	var parts []string
	if len(services) > 0 {
		parts = append(parts, "Services: "+strings.Join(services, ", ")+".")
	}
	if len(volumes) > 0 {
		parts = append(parts, "Volumes: "+strings.Join(volumes, ", ")+".")
	}
	return strings.Join(parts, " ")
}

// The place as each call's root: the API has a type of root for each.

func (p place) readRoot() pethost.ReadPathRoot {
	switch p.root {
	case inService:
		return pethost.ReadPathService(p.name)
	case inVolume:
		return pethost.ReadPathVolume(p.name)
	}
	return nil
}

func (p place) downloadRoot() pethost.PathDownloadRoot {
	switch p.root {
	case inService:
		return pethost.PathDownloadService(p.name)
	case inVolume:
		return pethost.PathDownloadVolume(p.name)
	}
	return nil
}

func (p place) uploadRoot() pethost.FileUploadRoot {
	switch p.root {
	case inService:
		return pethost.FileUploadService(p.name)
	case inVolume:
		return pethost.FileUploadVolume(p.name)
	}
	return nil
}

// lasts says, of a path in a service's container, whether what is there lasts: the API's
// answer to where the path lies. Empty = the API did not say.
func lasts(answer *pethost.ReadPathResponse) string {
	switch answer.Location {
	case pethost.FileLocationContainer:
		return "in the container's own files, lost when it is recreated"
	case pethost.FileLocationVolume:
		return "on the volume " + ui.Clean(answer.Volume) + ", kept across deploys"
	case pethost.FileLocationProjectFiles:
		return "one of the project's files, read-only there: a deploy changes it"
	}
	return ""
}
