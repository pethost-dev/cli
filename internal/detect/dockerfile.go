package detect

import (
	"cmp"
	"encoding/json"
	"fmt"
	"path"
	"strings"
)

// A Dockerfile is written from a plan in one of three shapes. A project that runs a program:
// its image, what the image lacks, the project's files, its steps, how it starts. A site that
// is files once built: the same up to its build, then a second stage that serves what the
// build wrote (serve.go). A folder that is the site as it stands: that second stage alone.
// The file is the person's to read and to change, and pethost never writes it again.

// workdir is where the project's files are in the image.
const workdir = "/app"

// dockerfileOf writes a plan's Dockerfile.
func dockerfileOf(p *plan) File {
	d := &dockerfile{}
	if p.serves == "" && p.byShell() {
		// docker build warns of a CMD in the shell's form. This one is so on purpose, and the
		// line that says so is read only as the file's first.
		d.line("# check=skip=JSONArgsRecommended")
	}
	d.comment("Written by pethost deploy for this %s: %s.", p.project, strings.Join(p.evidence, ", "))
	d.comment("It is yours now: change it as you like. pethost does not write it again.")

	if p.image == "" {
		d.serve(".", p.app)
		return d.file()
	}

	d.blank()
	if p.serves != "" {
		d.line("FROM %s AS build", p.image)
	} else {
		d.line("FROM %s", p.image)
	}
	d.line("WORKDIR %s", workdir)
	d.steps(p.setup)

	// Everything is copied before anything is installed: an install reads more than the
	// manifest (patches, a schema, a script of its own).
	d.blank()
	if p.owner == "" {
		d.line("COPY . .")
	} else {
		d.comment("The image's web server runs as %s, and the site may write into its own folder.", p.owner)
		d.line("COPY --chown=%s:%s . .", p.owner, p.owner)
	}
	d.steps(p.steps)

	if p.serves != "" {
		d.serve(path.Join(workdir, p.serves), p.app)
	} else {
		d.run(p)
	}
	return d.file()
}

// steps writes steps, each under its reason.
func (d *dockerfile) steps(steps []step) {
	for _, step := range steps {
		d.blank()
		if step.why != "" {
			d.comment("%s", step.why)
		}
		switch {
		case step.line != "":
			d.line("%s", step.line)
		case step.cache == "":
			d.line("RUN %s", step.run)
		default:
			prefix := "RUN "
			for _, directory := range strings.Fields(step.cache) {
				d.line("%s--mount=type=cache,target=%s \\", prefix, directory)
				prefix = "    "
			}
			d.line("    %s", step.run)
		}
	}
}

// run writes how a project that runs a program is started.
func (d *dockerfile) run(p *plan) {
	d.blank()
	for _, variable := range p.env {
		d.line("ENV %s=%s", variable.name, variable.value)
	}
	if p.volume != "" {
		d.comment("The machine keeps this directory across deploys.")
		d.line("VOLUME %s", p.volume)
	}
	if p.port != 0 {
		d.line("EXPOSE %d", p.port)
	}

	if !p.byShell() {
		d.line("CMD %s", execForm(p.start))
		return
	}
	last := cmp.Or(p.shell, strings.Join(p.start, " "))
	d.comment("A shell runs this, and its last command takes the shell's place (exec), so that it gets the signal that stops the container.")
	d.line("CMD %s", strings.Join(append(p.first, inPlaceOfShell(last)), " && \\\n    "))
}

// byShell says whether a shell runs the plan's start: it has something to do first, or the
// start is a shell's own line.
func (p *plan) byShell() bool {
	return p.shell != "" || len(p.first) > 0
}

// inPlaceOfShell is a shell's line with "exec" before its command. A line with a shell's own
// operators is several commands, and one that begins by setting a variable is the shell's to
// read: neither is touched.
func inPlaceOfShell(line string) string {
	first, _, _ := strings.Cut(line, " ")
	if strings.ContainsAny(line, "&|;<>") || strings.Contains(first, "=") || first == "exec" {
		return line
	}
	return "exec " + line
}

// A dockerfile is a Dockerfile's text as it is written.
type dockerfile struct {
	text strings.Builder
}

// commentWidth is where a comment's line ends.
const commentWidth = 88

// comment writes a comment, wrapped at commentWidth.
func (d *dockerfile) comment(format string, args ...any) {
	line := "#"
	for _, word := range strings.Fields(fmt.Sprintf(format, args...)) {
		if len(line)+1+len(word) > commentWidth && line != "#" {
			d.text.WriteString(line + "\n")
			line = "#"
		}
		line += " " + word
	}
	d.text.WriteString(line + "\n")
}

func (d *dockerfile) blank() {
	d.text.WriteString("\n")
}

// line writes one instruction.
func (d *dockerfile) line(format string, args ...any) {
	fmt.Fprintf(&d.text, format+"\n", args...)
}

// execForm is an instruction's exec form, a JSON array: the program runs itself, with no shell
// around it, and gets the signal that stops the container.
func execForm(args []string) string {
	form, _ := json.Marshal(args)
	return strings.ReplaceAll(string(form), `","`, `", "`)
}

func (d *dockerfile) file() File {
	return File{Name: "Dockerfile", Content: d.text.String()}
}
