package ui

// tints are the colours names get that stand side by side, such as the services of a log: red
// is not among them, since it marks what failed.
var tints = []string{"36", "33", "32", "35", "34"}

// Tint paints text in the n-th of a few colours, the first again after the last: give each name
// its own n, and a reader tells the names apart at a glance.
func (s *Stream) Tint(n int, text string) string {
	return s.paint(tints[n%len(tints)], text)
}
