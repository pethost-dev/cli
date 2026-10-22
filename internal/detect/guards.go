package detect

import (
	"cmp"
	"io/fs"
	"maps"
	"path"
	"slices"
	"strings"
)

// What holds for every stack. A row reads how one project is built and started, and the
// Dockerfile it fills runs that project alone. Three things in a folder say that this is not
// the whole of it: a Dockerfile the project keeps where the machine does not look, which is
// its authors' own answer and better than any reading of the folder; a second project in the
// same folder; and a database on the person's computer.

// aside are the folders whose Dockerfiles are not the project's, by what everyone calls
// them: the documentation, the examples, the tests.
var aside = []string{"docs", "doc", "examples", "example", "test", "tests", "e2e"}

// dockerfileDepth is how many directories below the root a project's own Dockerfile is looked
// for: build/package/Dockerfile is the deepest place people keep one.
const dockerfileDepth = 2

// ownDockerfiles are the Dockerfiles a project keeps where the machine does not read them:
// below the root, or at the root under another name (Dockerfile.prod).
func ownDockerfiles(f *Folder) []string {
	var found []string
	fs.WalkDir(f.files, ".", func(name string, entry fs.DirEntry, err error) error {
		base := path.Base(name)
		switch {
		case err != nil || name == ".":
			return nil
		case entry.IsDir():
			tools := strings.HasPrefix(base, ".") || base == "node_modules" || base == "vendor"
			if tools || slices.Contains(aside, base) || strings.Count(name, "/") >= dockerfileDepth {
				return fs.SkipDir
			}
		case isDockerfile(base) && name != "Dockerfile":
			found = append(found, name)
		}
		return nil
	})
	return found
}

// isDockerfile says whether a file's name is a Dockerfile's: the name itself, Podman's, or
// either way people tell several apart (Dockerfile.prod, api.Dockerfile). The capital is part
// of the name: dockerfile.go is a program's source.
func isDockerfile(base string) bool {
	if strings.HasSuffix(base, ".dockerignore") {
		return false
	}
	return base == "Dockerfile" || base == "dockerfile" || base == "Containerfile" ||
		strings.HasPrefix(base, "Dockerfile.") || strings.HasSuffix(base, ".Dockerfile") || strings.HasSuffix(base, ".dockerfile")
}

// namedDockerfiles is how many of a project's own Dockerfiles a person is told by name.
const namedDockerfiles = 3

// keptElsewhere is the finding of a folder whose project has Dockerfiles of its own that the
// machine does not read: pethost writes none beside them.
func keptElsewhere(finding Finding, dockerfiles []string) Finding {
	named := strings.Join(dockerfiles[:min(len(dockerfiles), namedDockerfiles)], ", ")
	if len(dockerfiles) > namedDockerfiles {
		named += " and others"
	}
	return Finding{
		Project:  cmp.Or(finding.Project, "Docker project"),
		Evidence: append(slices.Clone(finding.Evidence), "it has a Dockerfile of its own ("+named+")"),
		Leaves: "The project keeps a Dockerfile of its own where the machine does not read it (" + named + "), so pethost writes none beside it. " +
			"Where that one builds and runs the project, a compose.yaml at the folder's root that builds from it puts it online; " +
			"where it is for development or for one part alone, the project needs a Dockerfile at its root.",
	}
}

// several is the finding of a folder that more than one stack knows. Which of the projects
// runs, and whether they run together, is in no file, and a Dockerfile that builds one of two
// halves is a site without its pages or pages without their API.
func several(found []*Finding) Finding {
	var all []string
	for _, finding := range found {
		all = append(all, finding.Project+" ("+finding.manifest+")")
	}
	return Finding{
		Project:  "several projects",
		Evidence: all,
		Leaves: "This folder holds more than one project, and pethost writes a Dockerfile for a folder that is one: " +
			"a Dockerfile of your own builds what belongs together, or a compose.yaml runs them side by side.",
	}
}

// What a variable's name has when its value is where a database is, and what the value has
// when that is the person's computer.
var (
	databaseNames = []string{"DATABASE", "DB_HOST", "DB_URL", "MONGO", "POSTGRES", "MYSQL", "PGHOST"}
	thisComputer  = []string{"localhost", "127.0.0.1", "host.docker.internal"}
)

// databaseHere is the variable of the folder's .env that places a database on the person's
// computer; empty = none does. The machine gives the container that file, and the address in
// it is the container itself there: the deploy succeeds, and every page that reads a row fails.
func databaseHere(f *Folder) string {
	env := f.Env(".env")
	has := func(text string, parts []string) bool {
		return slices.ContainsFunc(parts, func(part string) bool { return strings.Contains(text, part) })
	}
	for _, name := range slices.Sorted(maps.Keys(env)) {
		if has(strings.ToUpper(name), databaseNames) && has(env[name], thisComputer) {
			return name
		}
	}
	return ""
}

// lookPlain is the folder that is a site as it stands: pages, and nothing that builds them.
// Jekyll's pages are sources, which its build turns into the site.
func lookPlain(f *Folder) *Finding {
	if !f.Has("index.html") || f.Has("_config.yml") || strings.HasPrefix(f.Text("index.html"), "---") {
		return nil
	}
	p := &plan{
		project:  "site of files",
		manifest: "index.html",
		evidence: []string{"index.html is at the folder's root", "nothing here builds it"},
		serves:   ".",
		notes:    []string{"Every file of the folder is public at its address, but for the Dockerfile and names that begin with a dot."},
	}
	return p.finding(f)
}
