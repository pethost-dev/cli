package ui

import (
	"fmt"
	"strings"
)

// A Table is rows of cells: on a terminal in columns as wide as their widest cell, off one
// separated by tabs, which a program splits. Between the rows it may hold lines of their own,
// such as a problem under its project.
type Table struct {
	stream *Stream
	lines  []tableLine
}

type tableLine struct {
	cells []string // A row.
	text  string   // Or a line as it is.
}

// Table starts a table on the stream; nothing is written until Flush.
func (s *Stream) Table() *Table {
	return &Table{stream: s}
}

// Row adds a row. Rows may differ in length; a row's last cell is not padded.
func (t *Table) Row(cells ...string) {
	t.lines = append(t.lines, tableLine{cells: cells})
}

// Line adds a line that is not a row, written as it is between the rows around it.
func (t *Table) Line(text string) {
	t.lines = append(t.lines, tableLine{text: text})
}

// Flush writes the table.
func (t *Table) Flush() {
	widths := t.widths()
	for _, line := range t.lines {
		switch {
		case line.cells == nil:
			fmt.Fprintln(t.stream, line.text)
		case t.stream.Terminal:
			fmt.Fprintln(t.stream, aligned(line.cells, widths))
		default:
			fmt.Fprintln(t.stream, strings.Join(line.cells, "\t"))
		}
	}
	t.lines = nil
}

func (t *Table) widths() []int {
	var widths []int
	for _, line := range t.lines {
		for i, cell := range line.cells {
			if i == len(widths) {
				widths = append(widths, 0)
			}
			widths[i] = max(widths[i], Width(cell))
		}
	}
	return widths
}

// columnGap is the space between two columns on a terminal.
const columnGap = 3

func aligned(cells []string, widths []int) string {
	var out strings.Builder
	for i, cell := range cells {
		out.WriteString(cell)
		if i < len(cells)-1 {
			out.WriteString(strings.Repeat(" ", widths[i]-Width(cell)+columnGap))
		}
	}
	return strings.TrimRight(out.String(), " ")
}
