//go:build !windows

package ui

import "os"

// enableEscapes is for Windows, whose old console needs it; every other terminal reads escape
// sequences as it is.
func enableEscapes(*os.File) bool { return true }
