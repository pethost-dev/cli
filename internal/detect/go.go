package detect

import (
	"path"
	"regexp"
	"strings"
)

// The Go stack: a folder with a go.mod. Its one row is a module with one program, the package
// main at its root or the one folder of cmd/ that has one, built to the name `go install`
// gives it and started with no arguments, or as the Procfile's web line says.

// goDefault is the Go of a module whose go line names a language level and no toolchain
// (before 1.21).
const goDefault = "1.27"

var (
	goLine      = regexp.MustCompile(`(?m)^go (1\.(\d+))`)
	modulePath  = regexp.MustCompile(`(?m)^module\s+(\S+)`)
	packageMain = regexp.MustCompile(`(?m)^package main\b`)
	// PocketBase as the program itself: go.mod requires it directly.
	pocketBase = regexp.MustCompile(`(?m)^\s*(?:require\s+)?github\.com/pocketbase/pocketbase v\S+\s*$`)
)

func lookGo(f *Folder) *Finding {
	if !f.Has("go.mod") {
		return nil
	}
	module := f.Text("go.mod")
	p := &plan{project: "Go program", manifest: "go.mod"}

	// Go's image builds with its own toolchain alone: the go line's release is the image's.
	release := goDefault
	if line := goLine.FindStringSubmatch(module); line != nil && len(line[2]) > 1 && line[2] >= "21" {
		release = line[1]
	}
	p.image = "golang:" + release
	p.evidence = append(p.evidence, "Go "+release+" (go.mod)")

	program := goProgram(f)
	if program == "" {
		return p.leave("The module has no one program: pethost builds the package main at a module's root, or the one folder " +
			"of cmd/ that has one. Which program visitors get, where there are several, is its Dockerfile's to say.")
	}
	name := goName(module, program)
	p.steps = []step{{run: "go build -o /usr/local/bin/" + name + " " + program, cache: "/go/pkg/mod /root/.cache/go-build"}}
	p.evidence = append(p.evidence, "its program is "+program)

	switch declared := webLine(f); {
	case declared != "":
		p.startByProcfile(declared)
	case pocketBase.MatchString(module):
		p.project = "PocketBase app"
		p.start, p.port = []string{name, "serve", "--http=0.0.0.0:8090"}, 8090
		p.ownData, p.volume = true, workdir+"/pb_data"
		p.evidence = append(p.evidence, "go.mod requires PocketBase, which serves with \"serve\" and keeps its data in pb_data")
	default:
		p.start = []string{name}
	}
	return p.finding(f)
}

// goProgram is the module's one program, as go build names it: "." or "./cmd/name". Empty =
// there is none, or cmd/ has several.
func goProgram(f *Folder) string {
	if isMain(f, ".") {
		return "."
	}
	var programs []string
	for _, name := range f.Names("cmd") {
		if dir, is := strings.CutSuffix(name, "/"); is && isMain(f, "cmd/"+dir) {
			programs = append(programs, "./cmd/"+dir)
		}
	}
	if len(programs) != 1 {
		return ""
	}
	return programs[0]
}

// isMain says whether a directory's Go files are a program's.
func isMain(f *Folder, dir string) bool {
	for _, name := range f.Names(dir) {
		if strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") && packageMain.MatchString(f.Text(path.Join(dir, name))) {
			return true
		}
	}
	return false
}

var majorSuffix = regexp.MustCompile(`^v\d+$`)

// goName is the name go install gives a program: its folder's, which for the module's root is
// the last part of the module's path, a major version's suffix left out. A Procfile names the
// program so.
func goName(module, program string) string {
	if program != "." {
		return path.Base(program)
	}
	found := modulePath.FindStringSubmatch(module)
	if found == nil {
		return "app"
	}
	name := found[1]
	if majorSuffix.MatchString(path.Base(name)) {
		name = path.Dir(name)
	}
	return path.Base(name)
}
