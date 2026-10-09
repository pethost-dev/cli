package detect

import (
	"encoding/json"
	"slices"
	"strings"
)

// The PHP stack. Its rows are Laravel and Symfony, by what composer.json requires, each as its
// own starter lays it out and on FrankenPHP; and a folder of PHP pages, on Apache.

// phpDefault is the PHP every row runs on: the newest that both servers' images have.
const phpDefault = "8.4"

// phpBundled are the extensions PHP's image has already: asking its installer for one fails.
var phpBundled = []string{
	"ctype", "curl", "dom", "fileinfo", "filter", "hash", "iconv", "json", "libxml", "mbstring", "openssl", "pcre", "pdo",
	"pdo_sqlite", "phar", "posix", "session", "simplexml", "sodium", "sqlite3", "tokenizer", "xml", "xmlreader", "xmlwriter", "zlib",
}

// frankenConfig is the server of a framework's site: FrankenPHP runs every page through
// public/index.php, on port 80. The machine's proxy ends HTTPS before it, which PHP is told.
const frankenConfig = `COPY <<'EOF' /etc/frankenphp/Caddyfile
:80 {
	root public/
	encode zstd br gzip
	php_server {
		env HTTPS on
	}
}
EOF`

// apacheConfig is the server of a folder of pages: Apache serves the folder itself, with what
// its .htaccess files say, and sends neither the Dockerfile nor a name that begins with a dot.
const apacheConfig = `COPY <<'EOF' /etc/apache2/sites-available/000-default.conf
<VirtualHost *:80>
	DocumentRoot /app
	<Directory /app>
		AllowOverride All
		Require all granted
	</Directory>
	RedirectMatch 404 "(^|/)\.(?!well-known/)"
	RedirectMatch 404 "^/Dockerfile$"
	SetEnv HTTPS on
</VirtualHost>
EOF`

// productionSettings are PHP's own settings for a public site: errors go to the log, not into
// the page.
var productionSettings = step{run: `cp "$PHP_INI_DIR/php.ini-production" "$PHP_INI_DIR/php.ini"`, why: "PHP's settings for a public site."}

var frankenStart = []string{"frankenphp", "run", "--config", "/etc/frankenphp/Caddyfile"}

func lookPHP(f *Folder) *Finding {
	if !f.Has("composer.json") {
		return phpPages(f)
	}
	required := composerRequires(f)
	p := &plan{project: "PHP project", manifest: "composer.json", evidence: []string{"composer.json is in the folder"}}
	if f.Has("package.json") {
		p.uses = []string{"package.json"}
	}

	_, isLaravel := required["laravel/framework"]
	_, isSymfony := required["symfony/framework-bundle"]
	switch {
	case isLaravel:
		return laravel(f, p, required)
	case isSymfony:
		return symfony(f, p, required)
	}
	return p.leave("composer.json requires neither Laravel nor Symfony, the PHP frameworks pethost has a row for.")
}

// composerRequires is what composer.json requires. An empty list is written there as "[]",
// which is no object: it is read apart from the rest.
func composerRequires(f *Folder) map[string]string {
	var manifest struct {
		Require json.RawMessage `json:"require"`
	}
	f.JSON("composer.json", &manifest)
	var required map[string]string
	json.Unmarshal(manifest.Require, &required)
	return required
}

// onFrankenPHP builds the plan on FrankenPHP: Composer, the extensions composer.json asks
// for, the server's config, and Composer's install of the project.
func onFrankenPHP(p *plan, required map[string]string) {
	var extensions []string
	for name := range required {
		if extension, is := strings.CutPrefix(name, "ext-"); is && !slices.Contains(phpBundled, extension) && extension != "zip" {
			extensions = append(extensions, extension)
		}
	}
	slices.Sort(extensions)

	p.image = "dunglas/frankenphp:1-php" + phpDefault
	p.setup = append(p.setup,
		step{
			run: strings.Join(append([]string{"install-php-extensions", "@composer", "zip"}, extensions...), " "),
			why: "Composer, the zip extension it unpacks packages with, and the extensions composer.json asks for.",
		},
		step{line: "ENV COMPOSER_ALLOW_SUPERUSER=1"},
		step{line: frankenConfig, why: "FrankenPHP serves public/ on port 80, and tells PHP that the site is HTTPS: the machine's proxy ends it."},
		productionSettings,
	)
	p.steps = append(p.steps, step{run: "composer install"})
	p.start, p.port = frankenStart, 80
}

// laravelDatabase is where a Laravel project's SQLite file is kept: storage/app is the one
// directory the framework gives the site's own files, and the machine keeps it.
const laravelDatabase = workdir + "/storage/app/database.sqlite"

// laravel is a Laravel project as `laravel new` makes one: SQLite, its assets built by Vite.
func laravel(f *Folder, p *plan, required map[string]string) *Finding {
	p.project = "Laravel project"
	p.evidence = []string{"composer.json requires laravel/framework"}

	env := f.Env(".env")
	switch connection := env["DB_CONNECTION"]; {
	case env["APP_KEY"] == "":
		return p.cannot("Laravel answers no page without the APP_KEY of the folder's .env, and there is none. " +
			"Copy .env.example to .env where there is no .env, run php artisan key:generate, and deploy again.")
	case connection != "" && connection != "sqlite":
		return p.leave(".env names a database other than SQLite (DB_CONNECTION=" + connection + "), and the machine runs none " +
			"beside a Dockerfile: the project needs a compose.yaml with the site and its database.")
	}

	onFrankenPHP(p, required)
	if stop := withAssets(f, p); stop != nil {
		return stop
	}
	p.ownData, p.volume = true, workdir+"/storage/app"
	p.env = append(p.env, variable{"DB_DATABASE", laravelDatabase})
	// The folder's .env is the one of the person's computer, and the machine gives it to the
	// container, where it wins over the image's variables: the start sets production over it.
	p.first = []string{
		"export APP_ENV=production APP_DEBUG=false LOG_CHANNEL=stderr DB_DATABASE=" + laravelDatabase,
		"php artisan migrate --force",
	}
	p.leaveOut(f, "vendor", "public/hot", "bootstrap/cache/packages.php", "bootstrap/cache/services.php", "bootstrap/cache/config.php")
	p.evidence = append(p.evidence,
		"its SQLite database is kept in storage/app, with the files the site is given",
		"it runs in production mode, set over the folder's .env",
	)
	p.notes = append(p.notes, "The database starts empty on the machine and is migrated at each start: "+
		"pethost run -- php artisan db:seed --force fills it from the project's seeders.")
	return p.finding(f)
}

// symfony is a Symfony project as `symfony new` makes one, and one that keeps no data: where
// a project's database runs, and how its tables are made, is in no file of a starter.
func symfony(f *Folder, p *plan, required map[string]string) *Finding {
	p.project = "Symfony project"
	p.evidence = []string{"composer.json requires symfony/framework-bundle"}
	if entities := f.Names("src/Entity"); slices.ContainsFunc(entities, func(name string) bool { return strings.HasSuffix(name, ".php") }) {
		return p.leave("The project keeps data (src/Entity), and the machine runs no database beside a Dockerfile: " +
			"it needs a compose.yaml with the site and its database, or a Dockerfile that says where its data is kept.")
	}

	// Composer's scripts warm the cache at the build, for the mode the site then runs in.
	p.setup = append(p.setup, step{line: "ENV APP_ENV=prod"})
	onFrankenPHP(p, required)
	if f.Has("importmap.php") {
		p.steps = append(p.steps, step{run: "php bin/console asset-map:compile", why: "The pages' assets: in production AssetMapper serves only what was compiled."})
	}
	if stop := withAssets(f, p); stop != nil {
		return stop
	}
	// A Symfony project's .env is committed and says APP_ENV=dev: the start sets over it.
	p.ownData, p.first = true, []string{"export APP_ENV=prod"}
	p.leaveOut(f, "vendor", "var")
	p.evidence = append(p.evidence, "it runs in production mode (APP_ENV=prod), set over the folder's .env")
	return p.finding(f)
}

// leaveOut keeps out of the image what a tool made on the person's computer, where it is
// there.
func (p *plan) leaveOut(f *Folder, names ...string) {
	for _, name := range names {
		if f.Has(name) || f.HasDir(name) {
			p.ignore = append(p.ignore, name)
		}
	}
}

// databaseCalls are what a PHP page opens a database with.
var databaseCalls = []string{"mysqli", "new PDO", "mysql_connect", "pg_connect"}

// phpPages is a folder of PHP pages: an index.php at its root, or pages beside an
// index.html, which a server of files would send to visitors as text.
func phpPages(f *Folder) *Finding {
	isPage := func(name string) bool { return strings.HasSuffix(name, ".php") }
	first := f.First("index.php", "index.html")
	if first == "" || !slices.ContainsFunc(f.Names("."), isPage) {
		return nil
	}
	p := &plan{project: "PHP site", manifest: first, evidence: []string{first + " is at the folder's root, with PHP pages"}}

	// A page that opens a database: the deploy would succeed, and the page answer an error.
	var opens string
	f.Walk(2, func(name string) {
		if opens == "" && isPage(name) && slices.ContainsFunc(databaseCalls, func(call string) bool { return strings.Contains(f.Text(name), call) }) {
			opens = name
		}
	})
	if opens != "" {
		return p.leave("Its pages open a database (" + opens + "), and the machine runs none beside a Dockerfile: " +
			"the project needs a compose.yaml with the site and its database.")
	}

	p.image, p.owner = "php:"+phpDefault+"-apache", "www-data"
	p.setup = []step{
		{line: apacheConfig, why: "Apache serves the folder on port 80, and tells PHP that the site is HTTPS: the machine's proxy ends it."},
		{run: "a2enmod rewrite", why: "What a .htaccess rewrites addresses with."},
		productionSettings,
	}
	p.start, p.port = []string{"apache2-foreground"}, 80
	p.notes = []string{"Every file of the folder is public at its address, but for the Dockerfile and names that begin with a dot."}
	return p.finding(f)
}
