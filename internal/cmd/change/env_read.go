package change

import (
	"context"
	"errors"
	"slices"
	"strings"

	pethost "github.com/pethost-dev/sdk/go"

	"github.com/pethost-dev/cli/internal/fail"
)

// envPath is the project's file of variables, as the API names it.
const envPath = "/.env"

// A projectEnv is the project's /.env as the machine holds it.
type projectEnv struct {
	// Its text; empty when the project has none.
	text   string
	exists bool
	// The deploy whose files it was read from: a change is refused if the project was deployed
	// since (DeployProjectRequest.BaseDeployID).
	deployID string
}

// readChunk is how much of the file one call reads: the most the API gives.
const readChunk = 1 << 20

// readEnv reads the project's /.env whole. A project without one has an empty text.
func readEnv(ctx context.Context, client *pethost.Client, projectID string) (*projectEnv, error) {
	var text strings.Builder
	request := &pethost.ReadPathRequest{ProjectID: projectID, Path: envPath, LengthBytes: readChunk}
	for {
		answer, err := client.ReadPath(ctx, request)
		if isNotFound(err) {
			return withoutEnv(ctx, client, projectID)
		}
		if err != nil {
			return nil, err
		}
		if answer.Binary {
			return nil, fail.Failed("The project's %s is not text: it cannot be edited here.", envPath)
		}

		text.WriteString(answer.Text)
		if answer.NextOffsetBytes == 0 {
			return &projectEnv{text: text.String(), exists: true, deployID: answer.DeployID}, nil
		}
		request.OffsetBytes = answer.NextOffsetBytes
	}
}

// withoutEnv is the project's /.env when ReadPath found none: the files' root says which deploy
// that is true of, and fails in the machine's words when it is the project that is not there.
func withoutEnv(ctx context.Context, client *pethost.Client, projectID string) (*projectEnv, error) {
	root, err := client.ReadPath(ctx, &pethost.ReadPathRequest{ProjectID: projectID, Path: "/", EntryLimit: 1})
	if err != nil {
		return nil, err
	}
	return &projectEnv{deployID: root.DeployID}, nil
}

func isNotFound(err error) bool {
	api, ok := errors.AsType[*pethost.Error](err)
	return ok && api.Code == pethost.CodeNotFound
}

// readers are the services that the machine says take a variable from /.env: by its name
// through `env_file`, or as part of a value compose.yaml builds. known is false when a service
// has a variable of that name whose source the machine cannot tell: it may read it.
func readers(project *pethost.Project, name string) (services []string, known bool) {
	known = true
	for _, service := range project.Services {
		reads := false
		for _, variable := range service.Environment {
			if (variable.Name == name && variable.SourceFile == envPath) || slices.Contains(variable.FromEnvFileVariables, name) {
				reads = true
			}
			if variable.Name == name && variable.SourceFile == "" {
				known = false
			}
		}
		if reads {
			services = append(services, service.Service)
		}
	}
	return services, known
}

// nobodyReads says whether the machine states that no service takes anything from /.env: none
// names an env file, and no variable of any comes from one or is built from .env's. A variable
// whose source the machine cannot tell leaves it unsaid.
func nobodyReads(project *pethost.Project) bool {
	if project == nil || len(project.Services) == 0 {
		return false
	}
	for _, service := range project.Services {
		if len(service.EnvFiles) > 0 {
			return false
		}
		for _, variable := range service.Environment {
			if variable.SourceFile != "/compose.yaml" || len(variable.FromEnvFileVariables) > 0 {
				return false
			}
		}
	}
	return true
}

// nobodyReadsLine says, under a change of /.env, that it reaches no service yet and what to
// write so that it does; empty unless the machine states it.
func nobodyReadsLine(project *pethost.Project) string {
	if !nobodyReads(project) {
		return ""
	}
	return "No service reads " + envPath + " yet: add  env_file: .env  to the service in compose.yaml."
}
