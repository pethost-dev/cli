//go:build ignore

// Command reference_gen prints the help of every command as JSON: what the CLI's reference,
// pethost.dev/docs/cli/, is drawn from (landing/src/pages/docs/cli.astro). A command's words
// are written once, in its own file, and the page says them as `pethost help` does. Run it in
// cli/ after a command's help changed:
//
//	go run internal/root/reference_gen.go > ../landing/src/docs/cli.json
//
// The CLI's workflow checks that the file is what this writes.
package main

import (
	"encoding/json"
	"log"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/pethost-dev/cli/internal/cmd/account"
	"github.com/pethost-dev/cli/internal/root"
)

type reference struct {
	// The flags every command takes.
	Flags    []flag    `json:"flags"`
	Sections []section `json:"sections"`
}

type section struct {
	Title    string    `json:"title"`
	Commands []command `json:"commands"`
}

type command struct {
	// "pethost env set"
	Path    string `json:"path"`
	Summary string `json:"summary"`
	// The long description, as the help prints it: paragraphs, and lines set apart by an indent.
	Text string `json:"text,omitempty"`
	// How it is typed. Empty for a command that only holds others.
	Usage    string    `json:"usage,omitempty"`
	Flags    []flag    `json:"flags,omitempty"`
	Examples string    `json:"examples,omitempty"`
	Commands []command `json:"commands,omitempty"`
}

type flag struct {
	Name  string `json:"name"`
	Short string `json:"short,omitempty"`
	// What its value is: "string", "uint32". Empty for a flag that takes none.
	Value string `json:"value,omitempty"`
	Text  string `json:"text"`
}

func main() {
	tree := root.Tree()
	page := reference{Flags: flagsOf(tree.PersistentFlags())}
	for _, part := range account.Sections(tree) {
		page.Sections = append(page.Sections, section{Title: part.Title, Commands: commandsOf(part.Commands)})
	}

	out := json.NewEncoder(os.Stdout)
	out.SetIndent("", "\t")
	// A usage line has <angle brackets>, which are text here.
	out.SetEscapeHTML(false)
	if err := out.Encode(page); err != nil {
		log.Fatal(err)
	}
}

func commandsOf(commands []*cobra.Command) []command {
	var described []command
	for _, cmd := range commands {
		described = append(described, commandOf(cmd))
	}
	return described
}

func commandOf(cmd *cobra.Command) command {
	described := command{
		Path:     cmd.CommandPath(),
		Summary:  cmd.Short,
		Text:     cmd.Long,
		Flags:    flagsOf(cmd.LocalFlags()),
		Examples: unindented(cmd.Example),
	}
	if cmd.Runnable() {
		described.Usage = cmd.UseLine()
	}
	for _, sub := range cmd.Commands() {
		if sub.IsAvailableCommand() {
			described.Commands = append(described.Commands, commandOf(sub))
		}
	}
	return described
}

func flagsOf(set *pflag.FlagSet) []flag {
	var flags []flag
	set.VisitAll(func(f *pflag.Flag) {
		if f.Hidden {
			return
		}
		value, text := pflag.UnquoteUsage(f)
		flags = append(flags, flag{Name: f.Name, Short: f.Shorthand, Value: value, Text: text})
	})
	return flags
}

// unindented are a command's examples without the indent the help prints them under.
func unindented(examples string) string {
	lines := strings.Split(examples, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimPrefix(line, "  ")
	}
	return strings.Join(lines, "\n")
}
