package ui

// The colours and marks of a stream: escape sequences on a colour terminal, the bare text
// elsewhere, and no mark at all off a terminal.

func (s *Stream) paint(code, text string) string {
	if !s.Color || text == "" {
		return text
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}

func (s *Stream) Bold(text string) string   { return s.paint("1", text) }
func (s *Stream) Dim(text string) string    { return s.paint("2", text) }
func (s *Stream) Red(text string) string    { return s.paint("31", text) }
func (s *Stream) Green(text string) string  { return s.paint("32", text) }
func (s *Stream) Yellow(text string) string { return s.paint("33", text) }
func (s *Stream) Cyan(text string) string   { return s.paint("36", text) }

// Check is "✓ " before a line that says a step ended well; nothing off a terminal.
func (s *Stream) Check() string { return s.mark(s.Green("✓")) }

// Cross is "✗ " before a line that says what failed or is wrong; nothing off a terminal.
func (s *Stream) Cross() string { return s.mark(s.Red("✗")) }

// Tilde is "~ " before a line that says what is slow; nothing off a terminal.
func (s *Stream) Tilde() string { return s.mark(s.Yellow("~")) }

func (s *Stream) mark(symbol string) string {
	if !s.Terminal {
		return ""
	}
	return symbol + " "
}
