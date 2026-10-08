package fail

import "errors"

// Innermost is the error at the bottom of a chain: "connection refused", "permission denied",
// without the addresses and the layers that wrap it.
func Innermost(err error) error {
	for inner := errors.Unwrap(err); inner != nil; inner = errors.Unwrap(err) {
		err = inner
	}
	return err
}
