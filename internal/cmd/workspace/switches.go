package workspace

import (
	"strings"

	pethost "github.com/pethost-dev/sdk/go"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/ui"
)

// What a move uses in the destination instead of each database the project is linked to, as the
// flags name it (MoveProjectRequest.switch_databases). A database no flag names is left to the
// panel, which takes the destination's database of the same id, else a new one, with a copy of
// the data. Which database a flag may name, and which target, prefix or copy fits it, is the
// panel's to judge: its refusal says so, and nothing is checked here first.
type choices struct {
	into   []string // database=target
	empty  []string // database
	prefix []string // database=PREFIX

	usage string
}

func (c *choices) flags(cmd *cobra.Command) {
	cmd.Flags().StringArrayVar(&c.into, "into", nil, "use another `database=target` there: the id of a database of that workspace")
	cmd.Flags().StringArrayVar(&c.empty, "empty", nil, "copy no data of this `database`: the project starts with an empty one there")
	cmd.Flags().StringArrayVar(&c.prefix, "prefix", nil, "the `database=PREFIX` of connection lines you wrote into .env yourself (DATABASE for DATABASE_URL)")
	c.usage = cmd.UseLine()
}

// switches is one switch per database the flags name, in the order they first name it.
func (c *choices) switches() ([]pethost.DatabaseSwitch, error) {
	var order []string
	byID := map[string]*pethost.DatabaseSwitch{}
	of := func(id string) *pethost.DatabaseSwitch {
		if byID[id] == nil {
			byID[id] = &pethost.DatabaseSwitch{DatabaseProjectID: id}
			order = append(order, id)
		}
		return byID[id]
	}

	for _, given := range c.into {
		id, target, err := c.pair("--into", given)
		if err != nil {
			return nil, err
		}
		of(id).IntoDatabaseProjectID = target
	}
	for _, id := range c.empty {
		of(id).Empty = true
	}
	for _, given := range c.prefix {
		id, prefix, err := c.pair("--prefix", given)
		if err != nil {
			return nil, err
		}
		of(id).EnvPrefix = prefix
	}

	switches := make([]pethost.DatabaseSwitch, len(order))
	for i, id := range order {
		switches[i] = *byID[id]
	}
	return switches, nil
}

// pair is a flag's database and value, written database=value.
func (c *choices) pair(flag, given string) (string, string, error) {
	id, value, found := strings.Cut(given, "=")
	if !found || id == "" || value == "" {
		return "", "", fail.Usage(flag+" takes a database and a value: "+flag+" shop-db=…, not "+given, c.usage)
	}
	return id, value, nil
}

// sayDependency is what the move to the workspace `to` does about one database the project is
// linked to, in a line: it stays, what the project uses there instead, and with what data.
func sayDependency(dependency pethost.ProjectDependency, switches []pethost.DatabaseSwitch, to string) string {
	id := dependency.DatabaseProjectID
	given := pethost.DatabaseSwitch{DatabaseProjectID: id}
	for _, s := range switches {
		if s.DatabaseProjectID == id {
			given = s
		}
	}

	there := "the database of the same id, or a new one"
	if given.IntoDatabaseProjectID != "" {
		there = ui.Clean(given.IntoDatabaseProjectID)
	}
	data := "with nothing copied"
	switch {
	case given.Empty:
		data = "with none of its data"
	case dependency.CopyDatabase != "":
		data = "with a copy of its database " + ui.Clean(dependency.CopyDatabase)
	}
	line := ui.Clean(id) + " stays here; in " + ui.Clean(to) + " the project uses " + there + ", " + data + "."

	if dependency.EnvPrefix == "" && given.EnvPrefix == "" {
		line += " You wrote its connection lines into .env yourself: name their prefix, as --prefix " + ui.Clean(id) + "=DATABASE."
	}
	return line
}
