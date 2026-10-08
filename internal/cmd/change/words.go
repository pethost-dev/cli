package change

import (
	"fmt"
	"strings"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/ui"
)

// word is a name as one word of a command the screen offers to run: cleaned, since most names
// come from the machine, then written as a shell takes it.
func word(name string) string {
	return app.ShellWord(ui.Clean(name))
}

// listedMost is how many names a sentence holds before it counts the rest.
const listedMost = 6

// listed is names as a sentence holds them: "A", "A and B", "A, B and C", and past a few "A, B,
// C and 4 more". The names are other people's text, so each is cleaned.
func listed(names []string) string {
	words := make([]string, 0, len(names))
	for _, name := range names {
		words = append(words, ui.Clean(name))
	}
	if len(words) > listedMost {
		words = append(words[:listedMost-1], fmt.Sprintf("%d more", len(words)-listedMost+1))
	}

	switch len(words) {
	case 0:
		return ""
	case 1:
		return words[0]
	}
	return strings.Join(words[:len(words)-1], ", ") + " and " + words[len(words)-1]
}
