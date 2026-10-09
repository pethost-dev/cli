package detect

import "strings"

// The server of a site that is files: Caddy, on port 80. It is the one program a Dockerfile
// adds that the project did not ask for, since a site of files has none of its own, and its
// builder's preview server is no server for visitors (Vite's refuses a host it does not know).
//
// Every site is looked up alike: the address, then the address with ".html", then the folder of
// that name, which Caddy answers by leading to the address with its slash, so that the page's
// own links hold.

// siteRoot is where the site's files are in the server's image.
const siteRoot = "/srv"

// serve writes the stage that serves a site: from is its files, a folder of the build stage
// or "." for the folder as it stands. app = an address that is no file answers the site's
// index.html; else the site's own 404.html.
func (d *dockerfile) serve(from string, app bool) {
	// What a visitor is not given: names that begin with a dot, and in a folder that is the
	// site as it stands, where this file is among the pages, the Dockerfile.
	own, whose := "*/.*", "no pages"
	d.blank()
	d.comment("The site is files: Caddy serves them on port 80.")
	d.line("FROM caddy:2")
	if from == "." {
		d.line("COPY . %s", siteRoot)
		own, whose = own+" /Dockerfile", "the folder's own business, as is this Dockerfile"
	} else {
		d.line("COPY --from=build %s", copied(from, siteRoot))
	}

	d.blank()
	d.line("COPY <<'EOF' /etc/caddy/Caddyfile")
	d.line(":80 {")
	d.line("\troot * %s", siteRoot)
	d.line("\t# A browser that takes its answers compressed gets them so.")
	d.line("\tencode")
	d.line("\t# Names that begin with a dot are %s;", whose)
	d.line("\t# .well-known is the one such folder that is there for visitors.")
	d.line("\t@own {")
	d.line("\t\tpath %s", own)
	d.line("\t\tnot path /.well-known/*")
	d.line("\t}")
	d.line("\terror @own 404")
	d.line("\t# A page's address works without its \".html\", and a folder's leads to its index.html.")
	if app {
		d.line("\t# An address that is no file gets the app's index.html: the app routes in the browser.")
		d.line("\ttry_files {path} {path}.html {path}/ /index.html")
		d.line("\tfile_server")
	} else {
		d.line("\ttry_files {path} {path}.html {path}/")
		d.line("\tfile_server")
		d.line("\t# An address that is no page: the site's own 404.html, if it has one.")
		d.line("\thandle_errors 404 {")
		d.line("\t\trewrite * /404.html")
		d.line("\t\tfile_server")
		d.line("\t}")
	}
	d.line("}")
	d.line("EOF")
	d.line("EXPOSE 80")
}

// copied is the two paths of a COPY: as they are, or in the instruction's JSON form where a
// name has what would cut it in two.
func copied(from, to string) string {
	if strings.ContainsAny(from+to, " \t\"'[]") {
		return execForm([]string{from, to})
	}
	return from + " " + to
}
