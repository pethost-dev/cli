package root

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/pethost-dev/cli/internal/cmd/account"
	"github.com/pethost-dev/cli/internal/fail"
)

// A command line that is wrong ends with one line that says what is wrong and the command's
// usage line, not with the whole help, and with the exit code of "a choice is needed".

// wrongFlag is the answer to a flag the command does not have or cannot read, in the CLI's
// words: what the flag library found is read from its error's fields, never from its text.
func wrongFlag(cmd *cobra.Command, err error) error {
	var message string
	if unknown, ok := errors.AsType[*pflag.NotExistError](err); ok {
		message = fmt.Sprintf("%s has no flag %s.", cmd.CommandPath(), flagAsTyped(unknown.GetSpecifiedName(), unknown.GetSpecifiedShortnames()))
	} else if bare, ok := errors.AsType[*pflag.ValueRequiredError](err); ok {
		message = fmt.Sprintf("%s needs a value after it.", flagAsTyped(bare.GetSpecifiedName(), bare.GetSpecifiedShortnames()))
	} else if invalid, ok := errors.AsType[*pflag.InvalidValueError](err); ok {
		message = fmt.Sprintf("--%s takes %s, and %q was given.", invalid.GetFlag().Name, valueWords(invalid.GetFlag()), invalid.GetValue())
	} else {
		return wrongUsage(cmd, err)
	}
	return fail.Usage(message, usageOf(cmd))
}

// flagAsTyped is a flag as the command line had it: "--since", or "-x", the one of a group of
// short flags that was at fault.
func flagAsTyped(name, shorthands string) string {
	if shorthands != "" {
		return "-" + shorthands[:1]
	}
	return "--" + name
}

// valueWords say what a flag's value is, by its type.
func valueWords(flag *pflag.Flag) string {
	switch kind := flag.Value.Type(); kind {
	case "int", "int32", "int64", "uint", "uint32", "uint64":
		return "a number"
	case "bool":
		return "true or false"
	default:
		return "a " + kind
	}
}

func wrongUsage(cmd *cobra.Command, err error) error {
	if e, ok := err.(*fail.Error); ok {
		return e
	}
	return fail.Usage(fmt.Sprintf("%s: %s", cmd.CommandPath(), err), usageOf(cmd))
}

// usageOf is a command's usage line, and where its flags and examples are shown.
func usageOf(cmd *cobra.Command) string {
	return cmd.UseLine() + "     See: " + strings.Replace(cmd.CommandPath(), "pethost", "pethost help", 1)
}

// strictArguments makes every command's refusal of its arguments a wrong usage.
func strictArguments(cmd *cobra.Command) {
	if check := cmd.Args; check != nil {
		cmd.Args = func(cmd *cobra.Command, args []string) error {
			err := check(cmd, args)
			switch {
			case err == nil:
				return nil
			case takesNone(cmd, check, args):
				// The library calls a word after such a command an unknown command, which it is not.
				return fail.Usage(fmt.Sprintf("%s takes no argument, and %q was given.", cmd.CommandPath(), args[0]), usageOf(cmd))
			case cmd.HasSubCommands() && len(args) > 0 && check(cmd, args[:1]) != nil:
				// A word after a noun that is neither a command of it nor an argument it takes.
				return noSuchCommand(cmd, args)
			}
			if _, own := err.(*fail.Error); own {
				return err
			}
			return fail.Usage(fmt.Sprintf("%s takes %s, and %s given.", cmd.CommandPath(), taken(cmd, check, len(args)), given(len(args))), usageOf(cmd))
		}
	}
	for _, sub := range cmd.Commands() {
		strictArguments(sub)
	}
}

// takesNone says whether the command refused its arguments because it takes none at all, by
// asking its own check: it has no commands under it, takes nothing, and refuses one word.
func takesNone(cmd *cobra.Command, check cobra.PositionalArgs, args []string) bool {
	if cmd.HasSubCommands() || len(args) == 0 {
		return false
	}
	one := check(cmd, args[:1])
	_, own := one.(*fail.Error)
	return one != nil && !own && check(cmd, nil) == nil
}

// taken says how many arguments a command takes: "2 arguments", "at most 1 argument", "1 or 2
// arguments". The command's own check is asked with every number of words up to a few past
// what was given, so the rule is written once, where the command declares it.
func taken(cmd *cobra.Command, check cobra.PositionalArgs, givenCount int) string {
	const past = 4
	least, most := -1, -1
	for n := 0; n <= givenCount+past; n++ {
		if check(cmd, make([]string, n)) == nil {
			if least < 0 {
				least = n
			}
			most = n
		}
	}

	switch {
	case least < 0:
		return "other arguments"
	case most == givenCount+past:
		return "at least " + arguments(least)
	case least == 0:
		return "at most " + arguments(most)
	case least == most:
		return arguments(most)
	case most == least+1:
		return fmt.Sprintf("%d or %s", least, arguments(most))
	}
	return fmt.Sprintf("%d to %s", least, arguments(most))
}

func arguments(n int) string {
	if n == 1 {
		return "1 argument"
	}
	return fmt.Sprintf("%d arguments", n)
}

// given says how many arguments the command line had: "none was", "1 was", "3 were".
func given(n int) string {
	switch n {
	case 0:
		return "none was"
	case 1:
		return "1 was"
	}
	return fmt.Sprintf("%d were", n)
}

// noSuchCommand is the root's check of its arguments, and the answer to a word after a noun
// that is no command of it: the bare command takes none, so a word after it is a command that
// does not exist, answered with the ones it may have meant.
func noSuchCommand(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}

	message := fmt.Sprintf("%s has no command %q.", cmd.CommandPath(), args[0])
	if !cmd.HasParent() && args[0] == "version" {
		// The version is a flag of the program, as it is of most.
		return fail.Choice(message).WithLead("The version:", "pethost --version")
	}
	suggestions := account.Near(cmd, args[0])
	if len(suggestions) == 0 {
		if cmd.HasParent() {
			return fail.Choice(message).WithLead("Its commands:", strings.Replace(cmd.CommandPath(), "pethost", "pethost help", 1))
		}
		return fail.Choice(message).WithLead("Every command:", "pethost help")
	}
	for i, suggestion := range suggestions {
		suggestions[i] = cmd.CommandPath() + " " + suggestion
	}
	e := fail.Choice(message, suggestions...)
	e.Lead = "Did you mean:"
	return e
}
