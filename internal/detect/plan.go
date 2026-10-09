package detect

import "strings"

// A plan is how a project is built and started, as its row says: plain facts, from which the
// Dockerfile is written (dockerfile.go), so that every stack's file reads alike and a row
// writes no line of one itself.
//
// The image is the project's folder after its build, on the language's full official image:
// the project's files where they were, everything installed, nothing pruned. A Dockerfile for
// a pet project is judged by whether it works, then by whether a person can read it, and last
// by its size. The one other shape is a site that is files once it is built: its build's
// output on a file server (serve.go).
type plan struct {
	// What the project is, as a person calls it: "VitePress site".
	project string
	// The file that makes the folder this project, and the other stacks' manifests it builds
	// with.
	manifest string
	uses     []string
	// Why this is known and how it is built and started, each a clause with its file.
	evidence []string

	// The image it is built on: "node:24".
	image string
	// What the image lacks, before the project's files are copied in.
	setup []step
	// Whose the project's files are in the image, where its web server runs as a user of its
	// own and the site writes into its folder: "www-data". Empty = as they are copied.
	owner string
	// What makes the copied files run: the install, then the build.
	steps []step
	// The variables the running container is given, set after the build.
	env []variable
	// A directory whose files a new deploy must not lose.
	volume string

	// How it starts: arguments, run as they are, or a line that a shell runs (a Procfile's,
	// with its $PORT).
	start []string
	shell string
	// What the start runs first, each time the container starts: a framework's migrations, a
	// variable set over the folder's .env, which the machine gives the container and which
	// wins over the image's variables.
	first []string
	// The port the start itself names; 0 = whichever the program listens on, which the machine
	// sees for itself.
	port int

	// A site that is files once built: the folder its build writes, from the project's root
	// ("." = the folder itself, which has no build). app = an address that is no file answers
	// the site's index.html, as an app that routes in the browser needs; else its 404.html.
	serves string
	app    bool

	// The project keeps its data where its row put it, or keeps none: the folder's .env says
	// nothing of a database that matters (guards.go).
	ownData bool
	// What a tool made on the person's computer and the image makes again, for .dockerignore.
	ignore []string
	// What no Dockerfile can give the project.
	notes []string
}

// A step is one instruction of a Dockerfile, with the reason a person reads above it where it
// needs one: a command to run, or the instruction as it is written.
type step struct {
	run  string
	line string
	why  string
	// The directories the command keeps between builds, which are in no image, with a space
	// between them: a package manager's downloads, a compiler's cache. A deploy then fetches
	// and compiles only what changed.
	cache string
}

// A variable is one variable of the container.
type variable struct {
	name, value string
}

// finding is the plan as Look answers it: the files, and what to say of them. A project that
// runs a program is left where the folder's .env places its database on the person's computer.
func (p *plan) finding(f *Folder) *Finding {
	if name := databaseHere(f); name != "" && p.serves == "" && !p.ownData {
		return p.leave("The folder's .env places the project's database on this computer (" + name + "), and the machine " +
			"runs no database beside a Dockerfile: the project needs a compose.yaml with the site and its database, " +
			"or the address of a database elsewhere in .env.")
	}
	return &Finding{
		Project:  p.project,
		Evidence: p.evidence,
		Files:    []File{dockerfileOf(p)},
		Summary:  p.summary(),
		Notes:    p.notes,
		manifest: p.manifest,
		uses:     p.uses,
		ignore:   p.ignore,
	}
}

// summary says what the Dockerfile does, in the words of its steps: "npm install, npm run
// docs:build, then serves docs/.vitepress/dist".
func (p *plan) summary() string {
	var does []string
	for _, step := range p.steps {
		if step.run != "" {
			does = append(does, step.run)
		}
	}
	switch {
	case p.serves == ".":
		does = append(does, "serves this folder")
	case p.serves != "":
		does = append(does, "then serves "+p.serves)
	case p.shell != "":
		does = append(does, "then runs "+p.shell)
	default:
		does = append(does, "then runs "+strings.Join(p.start, " "))
	}
	if len(does) == 1 {
		return strings.TrimPrefix(does[0], "then ")
	}
	return strings.Join(does, ", ")
}

// leave is the answer of a stack that knows the folder and has no row for it. Why, as a
// sentence: the person writes the file, or their coding agent does with this said to it.
func (p *plan) leave(why string) *Finding {
	return &Finding{Project: p.project, Evidence: p.evidence, Leaves: why, manifest: p.manifest, uses: p.uses}
}

// cannot is the answer of a stack that sees that no file makes the folder run as it stands:
// why, as a sentence to the person that says what to change in it.
func (p *plan) cannot(why string) *Finding {
	return &Finding{Project: p.project, Evidence: p.evidence, Cannot: why, manifest: p.manifest, uses: p.uses}
}

// webLine is the Procfile's web line: how a project says, in a file every host reads, what
// its visitors are served by. Empty = the folder has none.
func webLine(f *Folder) string {
	for _, line := range f.Lines("Procfile") {
		if command, is := strings.CutPrefix(line, "web:"); is {
			return strings.TrimSpace(command)
		}
	}
	return ""
}

// procfilePort is the PORT a Procfile's line is given: such a line reads it, as on the host
// the file was written for.
const procfilePort = "8080"

// startByProcfile starts the project with its Procfile's web line.
func (p *plan) startByProcfile(line string) {
	p.shell = line
	p.env = append(p.env, variable{"PORT", procfilePort})
	p.evidence = append(p.evidence, `the Procfile's web line starts it ("`+line+`"), with PORT=`+procfilePort)
}
