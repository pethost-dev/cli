package root

import (
	"runtime/debug"
	"strings"

	"github.com/pethost-dev/cli/internal/app"
)

// buildOf says which build this binary is: a release's stamped version, else the release that
// `go install` built it at, else "dev". A checkout's own build has a version too, made up from
// its commit ("v0.0.0-2026…-abc", "…+dirty"): that is no release.
func buildOf(stamped string) app.Build {
	if stamped != "" {
		return app.Build{Version: strings.TrimPrefix(stamped, "v")}
	}

	info, ok := debug.ReadBuildInfo()
	if !ok || !strings.HasPrefix(info.Main.Version, "v") || strings.ContainsAny(info.Main.Version, "-+") {
		return app.Build{Version: "dev"}
	}
	return app.Build{Version: strings.TrimPrefix(info.Main.Version, "v"), GoInstall: true}
}
