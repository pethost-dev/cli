// Package jsonout writes what --json prints: the API's own JSON, which the SDK's types marshal
// to by themselves, one object a line and without HTML escaping, so that a URL's "&" stays "&".
//
// A string of it may be a machine's text. JSON escapes the control characters below U+0020 by
// itself; the rest of what a terminal obeys (the C1 controls, and the controls that reorder
// letters) is written here as its \u escape, which reads back as the same string.
package jsonout

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"unicode/utf8"

	"github.com/pethost-dev/cli/internal/ui"
)

// Write writes value as one line of JSON. A command's answer is one call of it; a stream's
// answers are one call each.
func Write(w io.Writer, value any) error {
	return write(w, value, "")
}

// WriteIndented writes value as JSON over several lines, for a person to read at a terminal.
func WriteIndented(w io.Writer, value any) error {
	return write(w, value, "  ")
}

func write(w io.Writer, value any, indent string) error {
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", indent)
	if err := encoder.Encode(value); err != nil {
		return err
	}
	_, err := w.Write(escaped(encoded.Bytes()))
	return err
}

// escaped is JSON with every character a terminal would obey written as its \u escape. Outside
// a string JSON has none but the line breaks of its own layout, which ui.Obeyed leaves be.
func escaped(encoded []byte) []byte {
	if !bytes.ContainsFunc(encoded, ui.Obeyed) {
		return encoded
	}

	var out bytes.Buffer
	for len(encoded) > 0 {
		r, size := utf8.DecodeRune(encoded)
		if ui.Obeyed(r) {
			fmt.Fprintf(&out, `\u%04x`, r)
		} else {
			out.Write(encoded[:size])
		}
		encoded = encoded[size:]
	}
	return out.Bytes()
}
