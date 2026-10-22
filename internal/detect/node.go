package detect

import (
	"cmp"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
)

// The Node stack: a folder with a package.json. Its rows are the builders whose output is a
// site of files (sites), and then the one way a program is started: the start script, or the
// file npm itself would run.

// A site is a builder whose output is files: a project that names it and builds with its
// command is that kind of site.
type site struct {
	name string
	// The dependency that names the builder, and the command of it that a build script runs.
	needs, builds string
	// The folder the build writes, from the site's root: the folder itself, or the one the
	// command names (`vitepress build docs`).
	writes string
	// An app that routes in the browser: an address that is no file is its index.html.
	app bool
	// The builder's config, from the site's root and without its ending, and the key in it
	// that builds the site for a path below its address.
	config, base string
}

var sites = []site{
	{name: "VitePress site", needs: "vitepress", builds: "vitepress build", writes: ".vitepress/dist", config: ".vitepress/config", base: "base"},
	{name: "Docusaurus site", needs: "@docusaurus/core", builds: "docusaurus build", writes: "build", config: "docusaurus.config", base: "baseUrl"},
	{name: "Astro site", needs: "astro", builds: "astro build", writes: "dist", config: "astro.config", base: "base"},
	{name: "Eleventy site", needs: "@11ty/eleventy", builds: "eleventy", writes: "_site"},
	{name: "Create React App", needs: "react-scripts", builds: "react-scripts build", writes: "build", app: true},
	{name: "Vite app", needs: "vite", builds: "vite build", writes: "dist", app: true, config: "vite.config", base: "base"},
}

// servers are the dependencies with which a builder of sites makes a program's pages instead:
// what it writes is then served by that program, and is no site by itself.
var servers = []string{
	"@sveltejs/kit", "@remix-run/dev", "@react-router/dev", "@tanstack/react-start", "@solidjs/start", "vike", "nuxt",
	"@astrojs/node", "@astrojs/vercel", "@astrojs/netlify", "@astrojs/cloudflare",
}

// programs name a program by the dependency that makes it one; any other is a Node.js project.
var programs = []struct{ needs, name string }{
	{"next", "Next.js project"}, {"nuxt", "Nuxt project"}, {"@nestjs/core", "NestJS project"},
	{"@sveltejs/kit", "SvelteKit project"}, {"astro", "Astro project"},
}

// developers are the commands that serve a project while it is written. Such a server listens
// to the person's computer alone, or refuses an address it does not know: a deploy of it
// succeeds, and no visitor gets a page.
var developers = []string{
	"vite", "webpack serve", "webpack-dev-server", "ng serve", "expo", "react-scripts start", "vue-cli-service serve",
	"parcel", "gatsby develop", "next dev", "nuxt dev", "astro dev", "electron", "react-native",
}

// nodeDefault is the Node of a project that names none: the current LTS.
const nodeDefault = "24"

// How each package manager installs a project, what brings it into a Node image (npm and
// Yarn 1 are there), and where it keeps what it downloads.
var managers = map[string]struct{ brings, installs, cache string }{
	"npm":  {"", "npm install", "/root/.npm"},
	"pnpm": {"npm install -g pnpm%s", "pnpm install", "/root/.local/share/pnpm/store"},
	"yarn": {"", "yarn install", "/usr/local/share/.cache/yarn"},
	"bun":  {"npm install -g bun%s", "bun install", "/root/.bun/install/cache"},
}

var lockfiles = []struct{ file, manager string }{
	{"package-lock.json", "npm"}, {"pnpm-lock.yaml", "pnpm"}, {"yarn.lock", "yarn"}, {"bun.lock", "bun"}, {"bun.lockb", "bun"},
}

// A node is a project's package.json, and the package manager that installs it.
type node struct {
	Scripts         map[string]string `json:"scripts"`
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
	PackageManager  string            `json:"packageManager"`
	Main            string            `json:"main"`

	manager string
}

func lookNode(f *Folder) *Finding {
	n := &node{}
	if !f.JSON("package.json", n) {
		return nil
	}
	p := &plan{project: "Node.js project", manifest: "package.json"}
	if row, script := n.site(); row != nil {
		return n.asSite(f, p, row, script)
	}
	return n.asProgram(f, p)
}

func (n *node) has(name string) bool {
	_, runs := n.Dependencies[name]
	_, builds := n.DevDependencies[name]
	return runs || builds
}

// site is the row of the site the project builds, and the script that builds it. A project
// whose start script is another program's is that program, which serves what was built.
func (n *node) site() (*site, string) {
	if slices.ContainsFunc(servers, n.has) {
		return nil, ""
	}
	start := n.Scripts["start"]
	for i := range sites {
		row := &sites[i]
		tool, _, _ := strings.Cut(row.builds, " ")
		script := n.building(row.builds)
		if n.has(row.needs) && script != "" && (start == "" || strings.Contains(start, tool)) {
			return row, script
		}
	}
	return nil, ""
}

// building is the script that builds with a command: "build", or the first by name of the
// others that say so in their name (docs:build).
func (n *node) building(command string) string {
	names := []string{"build"}
	for name := range n.Scripts {
		if name != "build" && strings.Contains(name, "build") {
			names = append(names, name)
		}
	}
	slices.Sort(names[1:])
	for _, name := range names {
		if strings.Contains(n.Scripts[name], command) {
			return name
		}
	}
	return ""
}

func (n *node) asSite(f *Folder, p *plan, row *site, script string) *Finding {
	line := n.Scripts[script]
	root := argument(line, row.builds)
	p.project, p.app = row.name, row.app
	p.serves = path.Join(root, row.writes)
	p.evidence = []string{fmt.Sprintf("package.json builds it with %q, which writes %s", line, p.serves)}

	if row.config != "" {
		if config, below := builtBelow(f, path.Join(root, row.config), row.base); below != "" {
			return p.cannot(fmt.Sprintf("%s builds the site for the path %s of an address (%s), and at Pethost a site is at the root of "+
				"its own address: its pages would find none of their styles and scripts. Set %s to \"/\" there and deploy again.",
				config, below, row.base, row.base))
		}
	}
	if stop := n.onNode(f, p); stop != nil {
		return stop
	}
	p.steps = append(p.steps, step{run: n.manager + " run " + script})
	return p.finding(f)
}

// argument is the word after a command in a script's line, where it is a folder: the one
// `vitepress build docs` builds. "." = the command names none.
func argument(line, command string) string {
	_, after, _ := strings.Cut(line, command)
	words := strings.Fields(after)
	if len(words) == 0 || strings.HasPrefix(words[0], "-") || strings.ContainsAny(words[0], "&|;") {
		return "."
	}
	return path.Clean(words[0])
}

// configEndings are the endings a builder's config has: it is JavaScript or TypeScript.
var configEndings = []string{".js", ".mjs", ".cjs", ".ts", ".mts", ".cts"}

// builtBelow is the path below its address that a config builds the site for, and the config
// that says so; empty = none, or "/". A site built for /repo/ (GitHub Pages) deploys well and
// then loads none of its own files at the root of an address.
func builtBelow(f *Folder, config, key string) (file, below string) {
	quote := "[\"'`]"
	value := regexp.MustCompile(`\b` + key + `\s*:\s*` + quote + "([^\"'`]*)")
	for _, ending := range configEndings {
		found := value.FindStringSubmatch(f.Text(config + ending))
		if found != nil && found[1] != "" && found[1] != "/" && found[1] != "./" {
			return config + ending, found[1]
		}
	}
	return "", ""
}

func (n *node) asProgram(f *Folder, p *plan) *Finding {
	start, build, entry := n.Scripts["start"], n.Scripts["build"], n.entry(f)
	if start == "" && build == "" && entry == "" {
		// A package.json of tools beside another language's project: nothing of it runs.
		return nil
	}
	for _, program := range programs {
		if n.has(program.needs) {
			p.project = program.name
			break
		}
	}

	if stop := n.onNode(f, p); stop != nil {
		return stop
	}
	if build != "" {
		p.steps = append(p.steps, step{run: n.manager + " run build"})
		p.evidence = append(p.evidence, fmt.Sprintf("package.json builds it with %q", build))
	}
	switch {
	case developing(start):
		return p.leave(fmt.Sprintf("package.json's start script runs a server for development (%q), which is none for visitors: "+
			"it listens to this computer alone, or answers no address it does not know. How the project is served once it is "+
			"built is its Dockerfile's to say.", start))
	case start != "":
		p.start = []string{n.manager, "run", "start"}
		p.evidence = append(p.evidence, fmt.Sprintf("package.json starts it with %q", start))
	case entry != "":
		p.start = []string{"node", entry}
		p.evidence = append(p.evidence, "node runs "+entry+" (package.json has no start script)")
	default:
		return p.leave("package.json builds the project and names nothing that starts it (no start script), " +
			"and what it builds is no site of a kind pethost has a row for.")
	}
	p.env = append(p.env, variable{"NODE_ENV", "production"})
	return p.finding(f)
}

// entry is the file node runs where no script starts the project: what a Nuxt build writes,
// npm's own default, or package.json's main.
func (n *node) entry(f *Folder) string {
	switch {
	case n.has("nuxt"):
		return ".output/server/index.mjs"
	case f.Has("server.js"):
		return "server.js"
	case n.Main != "" && f.Has(n.Main):
		return n.Main
	}
	return ""
}

// developing says whether a start script runs a server for development.
func developing(script string) bool {
	script = strings.TrimPrefix(script, "npx ") + " "
	return slices.ContainsFunc(developers, func(tool string) bool { return strings.HasPrefix(script, tool+" ") })
}

// install fills the plan with the project's package manager and that manager's install. The
// manager is the one package.json names, or the one whose lockfile is there; a folder with
// the lockfiles of several is left.
func (n *node) install(f *Folder, p *plan) *Finding {
	name, release, by := n.named(f)
	manager, known := managers[name]
	if !known {
		return p.leave("Which package manager installs the project is not said: " + by + ".")
	}
	n.manager = name
	p.evidence = append(p.evidence, name+" installs it ("+by+")")

	brings := manager.brings
	if name == "yarn" && (f.Has(".yarnrc.yml") || strings.Contains(f.Text("yarn.lock"), "__metadata:")) {
		// A Yarn later than 1, which Corepack brings: the one package.json names, or the newest.
		brings, manager.cache = "npm install -g --force corepack && corepack enable && corepack prepare yarn%s --activate", ""
		release = cmp.Or(release, "stable")
	}
	if release != "" {
		release = "@" + release
	}
	if brings != "" {
		p.setup = append(p.setup, step{run: fmt.Sprintf(brings, release)})
	}
	p.steps = append(p.steps, step{
		run:   manager.installs,
		cache: manager.cache,
		why:   "Every dependency: what builds the project, and may start it, is among the ones for development.",
	})
	return nil
}

// onNode builds the plan on the project's Node and installs the project there.
func (n *node) onNode(f *Folder, p *plan) *Finding {
	image, why := nodeImage(f)
	p.image = image
	p.evidence = append(p.evidence, why)
	return n.install(f, p)
}

// named is the package manager of the project, its release where package.json names one, and
// what says so. An empty name = the folder has several lockfiles, which by lists.
func (n *node) named(f *Folder) (name, release, by string) {
	if n.PackageManager != "" {
		name, release, _ = strings.Cut(n.PackageManager, "@")
		release, _, _ = strings.Cut(release, "+") // "pnpm@9.12.0+sha512.…": the release, less its checksum
		return name, release, "packageManager in package.json"
	}

	var found, files []string
	for _, lockfile := range lockfiles {
		if f.Has(lockfile.file) && !slices.Contains(found, lockfile.manager) {
			found, files = append(found, lockfile.manager), append(files, lockfile.file)
		}
	}
	switch len(found) {
	case 0:
		return "npm", "", "there is no lockfile"
	case 1:
		return found[0], "", files[0]
	}
	return "", "", "the folder has the lockfiles of several (" + strings.Join(files, ", ") + ")"
}

var firstNumber = regexp.MustCompile(`\d+`)

// nodeImage is the image of the Node a version file names, by its major release, or of the
// default.
func nodeImage(f *Folder) (image, why string) {
	for _, file := range []string{".nvmrc", ".node-version"} {
		if lines := f.Lines(file); len(lines) > 0 && firstNumber.MatchString(lines[0]) {
			major := firstNumber.FindString(lines[0])
			return "node:" + major, "Node " + major + " (" + file + ")"
		}
	}
	return "node:" + nodeDefault, "Node " + nodeDefault + " (no .nvmrc names another)"
}

// withAssets adds to another stack's plan what builds its pages' assets, where the folder's
// package.json has a build script: Node beside the image's own language, the install, the
// build. Node is copied as its image has it, Yarn 1 included, which the image keeps in /opt
// (managers counts on it).
func withAssets(f *Folder, p *plan) *Finding {
	n := &node{}
	if !f.JSON("package.json", n) || n.Scripts["build"] == "" {
		return nil
	}
	image, _ := nodeImage(f)
	from := "COPY --from=" + image
	p.setup = append(p.setup, step{
		line: from + " /usr/local /usr/local\n" + from + " /opt /opt",
		why:  "Node, which builds the pages' assets, and the Yarn 1 its image keeps in /opt.",
	})
	p.evidence = append(p.evidence, fmt.Sprintf("package.json builds the assets with %q", n.Scripts["build"]))
	if stop := n.install(f, p); stop != nil {
		return stop
	}
	p.steps = append(p.steps, step{run: n.manager + " run build"})
	return nil
}
