package detect

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
)

// The Python stack: a folder with a requirements.txt or a pyproject.toml. Its rows are the
// frameworks a project names among its dependencies, each with the files its starter keeps the
// app in and its fixed start; a project of none of them is a program, started by the file
// that says it is one. Django has more to it than a row holds (django, below).

// A pythonRow is one kind of Python project: what it installs, where its app is, how it
// starts.
type pythonRow struct {
	// The package that makes a project this kind; empty = any project.
	needs, name string
	// The line that makes a file the app's (its group is the app's name in the file), and the
	// files a starter keeps it in. A project that keeps it in one other file at its root is
	// read as well.
	app   *regexp.Regexp
	files []string
	// The server that is added where the project names none.
	server string
	// The start, with {file}, {module} and {object} for where the app was found.
	start string
	port  int
	env   []variable
}

var pythonRows = []pythonRow{
	{
		needs: "fastapi", name: "FastAPI app",
		app:    regexp.MustCompile(`(?m)^(\w+)\s*(?::[^=\n]+)?=\s*FastAPI\(`),
		files:  []string{"main.py", "app.py", "app/main.py", "src/main.py", "api/main.py"},
		server: "uvicorn", start: "uvicorn {module}:{object} --host 0.0.0.0 --port 8000", port: 8000,
	},
	{
		needs: "flask", name: "Flask app",
		app:    regexp.MustCompile(`(?m)^(\w+)\s*=\s*Flask\(`),
		files:  []string{"app.py", "wsgi.py", "main.py", "application.py", "run.py"},
		server: "gunicorn", start: "gunicorn --bind 0.0.0.0:8000 {module}:{object}", port: 8000,
	},
	{
		needs: "streamlit", name: "Streamlit app",
		app:   regexp.MustCompile(`(?m)^\s*(?:import|from) streamlit`),
		files: []string{"streamlit_app.py", "app.py", "main.py"},
		start: "streamlit run {file}", port: 8501,
	},
	{
		needs: "gradio", name: "Gradio app",
		app:   regexp.MustCompile(`(?m)^\s*(?:import|from) gradio`),
		files: []string{"app.py", "main.py"},
		start: "python {file}", port: 7860,
		// Gradio listens to the container alone unless it is told otherwise.
		env: []variable{{"GRADIO_SERVER_NAME", "0.0.0.0"}},
	},
	{
		name:  "Python program",
		app:   regexp.MustCompile(`(?m)^if __name__ == ["']__main__["']`),
		files: []string{"main.py", "bot.py", "app.py", "run.py"},
		start: "python {file}",
	},
}

// pythonDefault is the Python of a project that names none.
const pythonDefault = "3.14"

// A python is a Python project's folder, with what it installs.
type python struct {
	f *Folder
	// What the project installs, a line of its manifest each, in lower case:
	// "fastapi[standard]>=0.115".
	requires []string
	// How one more package is installed beside the project's own: "pip install ".
	adds string
}

func lookPython(f *Folder) *Finding {
	manifest := f.First("requirements.txt", "pyproject.toml", "Pipfile")
	if manifest == "" {
		return nil
	}
	py := &python{f: f}
	p := &plan{project: "Python program", manifest: manifest}
	// Python writes its output at once: what the program says is in pethost logs as it says it.
	p.env = []variable{{"PYTHONUNBUFFERED", "1"}}
	for _, made := range []string{".venv", "venv"} {
		if f.HasDir(made) {
			p.ignore = append(p.ignore, made)
		}
	}

	var why string
	p.image, why = pythonImage(f)
	p.evidence = append(p.evidence, why)
	if stop := py.install(p); stop != nil {
		return stop
	}

	declared := webLine(f)
	if py.needs("django") && f.Has("manage.py") {
		return py.django(p, declared)
	}
	for _, row := range pythonRows {
		if row.needs != "" && !py.needs(row.needs) {
			continue
		}
		p.project = row.name
		if declared != "" {
			p.startByProcfile(declared)
			return p.finding(f)
		}
		return py.byRow(p, row)
	}
	return nil
}

// byRow starts a project as its row says, with the file its app was found in.
func (py *python) byRow(p *plan, row pythonRow) *Finding {
	file, object := py.file(row.app, row.files)
	if file == "" {
		return p.leave(fmt.Sprintf("No file where a %s keeps what it starts with has it (%s), and no other file at the "+
			"folder's root alone: which file starts the project is its Dockerfile's to say.", row.name, strings.Join(row.files, ", ")))
	}

	if row.server != "" {
		py.add(p, row.server)
	}
	module := strings.ReplaceAll(strings.TrimSuffix(file, ".py"), "/", ".")
	start := strings.NewReplacer("{file}", file, "{module}", module, "{object}", object).Replace(row.start)
	p.start, p.port = strings.Fields(start), row.port
	p.env = append(p.env, row.env...)
	p.evidence = append(p.evidence, file+" is what it starts with")
	return p.finding(py.f)
}

// file is the first of the files that has the line a pattern looks for, or the one other .py
// file at the folder's root that has it, with what the pattern's group caught there. Empty =
// none has it, or several of the others do.
func (py *python) file(pattern *regexp.Regexp, files []string) (file, caught string) {
	var others []string
	for _, name := range py.f.Names(".") {
		if strings.HasSuffix(name, ".py") && !slices.Contains(files, name) {
			others = append(others, name)
		}
	}

	found := func(names []string) (in []string) {
		for _, name := range names {
			if pattern.MatchString(py.f.Text(name)) {
				in = append(in, name)
			}
		}
		return in
	}
	in := found(files)
	if len(in) == 0 {
		if in = found(others); len(in) != 1 {
			return "", ""
		}
	}

	if groups := pattern.FindStringSubmatch(py.f.Text(in[0])); len(groups) > 1 {
		caught = groups[1]
	}
	return in[0], caught
}

// install reads what the project installs and fills the plan with how: uv where its lockfile
// is there, pip for a requirements.txt. A project that another tool installs is left.
func (py *python) install(p *plan) *Finding {
	var project struct {
		Project struct {
			Dependencies []string `toml:"dependencies"`
		} `toml:"project"`
	}
	py.f.TOML("pyproject.toml", &project)
	for _, line := range append(project.Project.Dependencies, py.f.Lines("requirements.txt")...) {
		py.requires = append(py.requires, strings.ToLower(line))
	}

	switch {
	case py.f.Has("uv.lock"):
		p.setup = append(p.setup,
			step{run: "pip install uv", why: "uv, which installs the project into .venv: that environment's programs are on PATH."},
			step{line: "ENV PATH=" + workdir + "/.venv/bin:$PATH"},
		)
		p.steps = append(p.steps, step{run: "uv sync --locked"})
		py.adds = "uv pip install "
		p.evidence = append(p.evidence, "uv installs it (uv.lock)")
	case py.f.Has("requirements.txt"):
		p.steps = append(p.steps, step{run: "pip install -r requirements.txt", cache: "/root/.cache/pip"})
		py.adds = "pip install "
		p.evidence = append(p.evidence, "pip installs it (requirements.txt)")
	default:
		return p.leave("Its dependencies are in " + p.manifest + " with neither a uv.lock nor a requirements.txt beside it: " +
			"another tool installs them (Poetry, Pipenv, pip with the project's own build), and pethost has no row for it.")
	}
	return nil
}

var packageName = regexp.MustCompile(`^[a-z0-9._-]+`)

// needs says whether the project installs a package.
func (py *python) needs(name string) bool {
	return slices.ContainsFunc(py.requires, func(line string) bool { return packageName.FindString(line) == name })
}

// add installs a server beside the project's own packages, where the project names none.
// FastAPI's extras bring uvicorn with them.
func (py *python) add(p *plan, server string) {
	withExtras := slices.ContainsFunc(py.requires, func(line string) bool { return strings.HasPrefix(line, "fastapi[") })
	if py.needs(server) || server == "uvicorn" && withExtras {
		return
	}
	p.steps = append(p.steps, step{run: py.adds + server, why: "A server for visitors: the project names none."})
	p.evidence = append(p.evidence, server+" serves it (added: the project names no server)")
}

var pythonRelease = regexp.MustCompile(`3\.\d+`)

// pythonImage is the image of the Python a version file names, or of the default.
func pythonImage(f *Folder) (image, why string) {
	for _, file := range []string{".python-version", "runtime.txt"} {
		if lines := f.Lines(file); len(lines) > 0 && pythonRelease.MatchString(lines[0]) {
			release := pythonRelease.FindString(lines[0])
			return "python:" + release, "Python " + release + " (" + file + ")"
		}
	}
	return "python:" + pythonDefault, "Python " + pythonDefault + " (no .python-version names another)"
}

// noHosts is the line of a settings module that allows no host: what django-admin
// startproject writes, and what Django answers 400 with at every address once it is deployed.
var noHosts = regexp.MustCompile(`(?m)^ALLOWED_HOSTS\s*=\s*\[\s*\]`)

// django starts a Django project: its WSGI module under gunicorn, or its Procfile's line,
// after the migrations, with its static files gathered where WhiteNoise serves them.
func (py *python) django(p *plan, declared string) *Finding {
	p.project = "Django project"
	p.evidence = append(p.evidence, "manage.py is Django's")

	var modules []string
	py.f.Walk(2, func(name string) {
		if path.Base(name) == "wsgi.py" {
			modules = append(modules, name)
		}
	})
	if len(modules) == 1 {
		settings := path.Join(path.Dir(modules[0]), "settings.py")
		if noHosts.MatchString(py.f.Text(settings)) {
			return p.cannot(settings + " allows no host (ALLOWED_HOSTS = []), and Django answers 400 at every address its settings " +
				"do not allow. Name the site's address there, or \"*\", and deploy again.")
		}
		if strings.Contains(py.f.Text(settings), "sqlite3") {
			p.notes = append(p.notes, "What the site saves is in a SQLite file inside the container, and a new deploy starts "+
				"with a new one: a VOLUME line in the Dockerfile keeps the directory "+settings+" places the file in.")
		}
	}

	if py.needs("whitenoise") {
		p.steps = append(p.steps, step{run: "python manage.py collectstatic --noinput", why: "The static files, where WhiteNoise serves them from."})
	}
	p.first = []string{"python manage.py migrate --noinput"}
	// gunicorn takes the machine's proxy at its word that the site is HTTPS, without which
	// Django refuses every form.
	p.env = append(p.env, variable{"FORWARDED_ALLOW_IPS", "*"})

	switch {
	case declared != "":
		p.startByProcfile(declared)
	case len(modules) != 1:
		return p.leave(fmt.Sprintf("The project has %d wsgi.py files where django-admin startproject writes one: "+
			"which module a server runs is its Dockerfile's to say.", len(modules)))
	default:
		py.add(p, "gunicorn")
		module := strings.ReplaceAll(strings.TrimSuffix(modules[0], ".py"), "/", ".")
		p.start, p.port = []string{"gunicorn", "--bind", "0.0.0.0:8000", module + ":application"}, 8000
		p.evidence = append(p.evidence, modules[0]+" is its WSGI module")
	}
	return p.finding(py.f)
}
