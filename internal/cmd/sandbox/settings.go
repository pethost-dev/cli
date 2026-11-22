package sandbox

import (
	"slices"
	"strings"
	"time"

	"github.com/pethost-dev/sdk/go/sandboxes"
	"github.com/spf13/pflag"
)

// The flags of a sandbox's settings, which `new` and `set` take. The API takes its settings in
// groups, and a group sent replaces the group it meets whole: a flag of a group sends that group,
// as the base it is given with the flag's field or list put in. For `set` the base is the
// sandbox's group as it is, so that a flag changes its own field alone; for `new` there is none,
// and the fields no flag gives are the API's defaults.

type settingsFlags struct {
	flags *pflag.FlagSet

	size                               string
	pauseAfter, stopAfter, deleteAfter time.Duration
	wakeWhenStopped                    bool
	allow, deny                        []string
	env                                []string
}

// addSettingsFlags adds the flags of the settings to a command's flags.
func addSettingsFlags(flags *pflag.FlagSet) *settingsFlags {
	f := &settingsFlags{flags: flags}
	flags.StringVar(&f.size, "size", "", "its vCPUs, memory and disk: small, default, large or xlarge")
	flags.DurationVar(&f.pauseAfter, "pause-after", 0, "pause it after this long idle, as 5m; 0 = never (default 5m)")
	flags.DurationVar(&f.stopAfter, "stop-after", 0, "stop it after this long idle, as 30m; 0 = never (default 30m)")
	flags.DurationVar(&f.deleteAfter, "delete-after", 0, "delete it after this long idle, as 24h; 0 = never (the default)")
	flags.BoolVar(&f.wakeWhenStopped, "wake-when-stopped", false, "a call, an SSH login or a request to a port starts it when stopped, as when paused")
	flags.StringArrayVar(&f.allow, "allow", nil, `a host it may connect to: "*", pypi.org, *.github.com, github.com:22, an IP or a CIDR (again for more)`)
	flags.StringArrayVar(&f.deny, "deny", nil, "a host it may never connect to, which wins over --allow (again for more)")
	flags.StringArrayVar(&f.env, "env", nil, "a variable, NAME=value, or NAME=secret:SECRET for a workspace secret (again for more)")
	return f
}

// changed says whether any of the flags was given.
func (f *settingsFlags) changed(names ...string) bool {
	return slices.ContainsFunc(names, f.flags.Changed)
}

// apply puts the flags given into settings, over base: the sandbox's settings in force for
// `set`, nil for `new`. A group no flag names is left out of settings. The size is only set when
// given.
func (f *settingsFlags) apply(settings, base *sandboxes.SandboxSettings) error {
	if base == nil {
		base = &sandboxes.SandboxSettings{}
	}
	if f.changed("size") {
		size, err := fromWord("size", f.size, sizes...)
		if err != nil {
			return err
		}
		settings.Size = size
	}

	lifecycle, err := f.lifecycle(base.Lifecycle)
	if err != nil {
		return err
	}
	settings.Lifecycle = lifecycle
	settings.Egress = f.egress(base.Egress)
	environment, err := f.environment()
	if err != nil {
		return err
	}
	settings.Environment = environment
	return nil
}

// lifecycle is the lifecycle group with the timers given put over base; nil when none was.
func (f *settingsFlags) lifecycle(base *sandboxes.Lifecycle) (*sandboxes.Lifecycle, error) {
	if !f.changed("pause-after", "stop-after", "delete-after", "wake-when-stopped") {
		return nil, nil
	}
	lifecycle := &sandboxes.Lifecycle{}
	if base != nil {
		*lifecycle = *base
	}

	for _, timer := range []struct {
		flag  string
		value time.Duration
		set   func(uint32)
	}{
		{"pause-after", f.pauseAfter, func(s uint32) { lifecycle.PauseAfterIdleSeconds = new(s) }},
		{"stop-after", f.stopAfter, func(s uint32) { lifecycle.StopAfterIdleSeconds = new(s) }},
		{"delete-after", f.deleteAfter, func(s uint32) { lifecycle.DeleteAfterIdleSeconds = s }},
	} {
		if !f.changed(timer.flag) {
			continue
		}
		value, err := seconds(timer.flag, timer.value)
		if err != nil {
			return nil, err
		}
		timer.set(value)
	}
	if f.changed("wake-when-stopped") {
		lifecycle.WakeWhenStopped = f.wakeWhenStopped
	}
	return lifecycle, nil
}

// egress is the egress group with the lists given in place of base's; nil when neither was.
// Without a base its allow is "*", the API's own default, so that --deny alone keeps every other
// host open, as a person who names a few hosts to block means.
func (f *settingsFlags) egress(base *sandboxes.Egress) *sandboxes.Egress {
	if !f.changed("allow", "deny") {
		return nil
	}
	egress := &sandboxes.Egress{Allow: []string{"*"}}
	if base != nil {
		egress = &sandboxes.Egress{Allow: slices.Clone(base.Allow), Deny: slices.Clone(base.Deny)}
	}
	if f.changed("allow") {
		egress.Allow = f.allow
	}
	if f.changed("deny") {
		egress.Deny = f.deny
	}
	return egress
}

// environment is the environment group the --env flags make, whole; nil when none was given.
func (f *settingsFlags) environment() (*sandboxes.Environment, error) {
	if !f.changed("env") {
		return nil, nil
	}
	environment := &sandboxes.Environment{}
	for _, typed := range f.env {
		name, value, ok := strings.Cut(typed, "=")
		if !ok {
			return nil, wrongValue("--env takes NAME=value or NAME=secret:SECRET, and %q was given.", typed)
		}
		variable := sandboxes.EnvVar{Name: name, Source: sandboxes.EnvVarValue(value)}
		if secret, isSecret := strings.CutPrefix(value, "secret:"); isSecret {
			variable.Source = sandboxes.EnvVarSecret(secret)
		}
		environment.Variables = append(environment.Variables, variable)
	}
	return environment, nil
}
