package target

import (
	"fmt"

	pethost "github.com/pethost-dev/sdk/go"

	"github.com/pethost-dev/cli/internal/app"
)

// AddressCommands are the commands that give a project without an address one, a command for
// each port its services listen on: "pethost domain add recipes.sam.pethost.app web:3000". The
// address is the project's id under the account's domain, a placeholder when the account has
// none. None = nothing listens, so there is nothing an address could lead to.
func (p Project) AddressCommands(project *pethost.Project, appsDomain string) []string {
	host := "<host>"
	if appsDomain != "" {
		host = app.ShellWord(project.ProjectID + "." + appsDomain)
	}

	var commands []string
	for _, service := range project.Services {
		for _, port := range service.ListeningPorts {
			to := fmt.Sprintf("%s:%d", service.Service, port)
			commands = append(commands, "pethost domain add "+host+" "+app.ShellWord(to)+p.Flag())
		}
	}
	return commands
}
