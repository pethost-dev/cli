// Package detect says what project a folder holds and writes the Dockerfile that builds and
// starts it.
//
// It is a table: a folder that looks like this is built and started like that. A stack is a
// file of this package, and its rows are the frameworks it knows. A row is found by what a
// project of its framework declares (a dependency in the manifest, a file of the framework's
// fixed layout) and fills a plan (plan.go), from which the Dockerfile is written
// (dockerfile.go). What a row takes from the folder is what the framework's own starter
// declares: the package manager by its lockfile, the version by a version file, the build and
// the start by the manifest's scripts. Nothing is run and nobody is asked: what Look answers
// depends on the folder's files alone.
//
// A project that no row fits is left: the answer says why, and the person writes the file, or
// their coding agent does with that said to it. A row does not look for what would make its
// own file fail (an old lockfile, a package that needs a library the image lacks, an app in a
// file of another name): the deploy command proves a written file by its first deploy, and
// takes back one that the machine cannot build or start.
//
// What that proof cannot see is a deploy that succeeds while the site is wrong. The few
// checks here are of that kind alone, and each is where its row is:
//   - a built site that is made for a path below its address (the builder's base);
//   - a Django project whose settings allow no host;
//   - a .env that places the database on the person's computer (guards.go);
//   - a start script that runs a development server;
//   - PHP pages that would be sent as text, or that talk to a database nobody runs;
//   - a Laravel project without its key.
//
// Two more hold for every stack (guards.go): a folder that two stacks both know is left, since
// nothing says whether the two run together, and so is one whose own Dockerfile is where the
// machine does not read it.
package detect

import (
	"io/fs"
	"slices"
	"strings"
)

// A Finding is what a folder was found to be.
type Finding struct {
	// What the project is, as a person calls it: "VitePress site", "Django project". Empty =
	// no stack knows the folder.
	Project string
	// Why, each a fact with the file that declares it: `package.json builds it with
	// "vitepress build docs"`.
	Evidence []string

	// The files that make the folder run; none where the folder is left.
	Files []File
	// What no Dockerfile can give the project, each a sentence to the person.
	Notes []string

	// Why no file can make the folder run until it changes. It says what to change.
	Cannot string
	// Why no file was written for a folder that can run: no row fits it. The person writes
	// it, or their coding agent does, with this said to it.
	Leaves string

	// The file that makes the folder this project, and the other stacks' manifests it builds
	// with: a Laravel project's package.json.
	manifest string
	uses     []string
	// What a tool made in the folder on the person's computer and the image makes again.
	ignore []string
}

// A File is one file to write at the folder's root.
type File struct {
	Name    string
	Content string
}

// stacks are the kinds of project a folder is looked at for: each looks by itself, and their
// order means nothing.
var stacks = []func(*Folder) *Finding{lookNode, lookPython, lookPHP, lookGo}

// Look says what the folder is.
func Look(files fs.FS) Finding {
	folder := &Folder{files: files}

	var found []*Finding
	for _, look := range stacks {
		if finding := look(folder); finding != nil {
			found = append(found, finding)
		}
	}
	found = withoutParts(found)

	finding := Finding{}
	switch len(found) {
	case 0:
		// A folder of pages is a site of files only where it is nothing else.
		if plain := lookPlain(folder); plain != nil {
			finding = *plain
		}
	case 1:
		finding = *found[0]
	default:
		finding = several(found)
	}

	if dockerfiles := ownDockerfiles(folder); len(dockerfiles) > 0 {
		return keptElsewhere(finding, dockerfiles)
	}
	return leftOut(folder, finding)
}

// withoutParts leaves out a finding whose manifest another one builds with: a Laravel
// project's package.json is how its pages' assets are built, not a second project.
func withoutParts(found []*Finding) []*Finding {
	var whole []*Finding
	for _, finding := range found {
		part := slices.ContainsFunc(found, func(other *Finding) bool {
			return other != finding && slices.Contains(other.uses, finding.manifest)
		})
		if !part {
			whole = append(whole, finding)
		}
	}
	return whole
}

// leftOut keeps out of the image what a tool made on the person's computer: a .dockerignore
// written with the Dockerfile. A folder that has a .dockerignore keeps its own.
func leftOut(folder *Folder, finding Finding) Finding {
	if len(finding.Files) == 0 || len(finding.ignore) == 0 || folder.Has(".dockerignore") {
		return finding
	}
	finding.Files = append(finding.Files, File{Name: ".dockerignore", Content: strings.Join(finding.ignore, "\n") + "\n"})
	return finding
}
