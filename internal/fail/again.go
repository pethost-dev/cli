package fail

import (
	"errors"

	pethost "github.com/pethost-dev/sdk/go"
)

// CallAgain says whether a failure is one that the same call, made again, may get past: the
// panel or the machine did not answer, or the connection broke. A refusal the API answered
// with is final.
//
// A broken connection is told by the error's cause, which only a failure below the API has,
// and not by its code: a stream cut in the middle of a message comes from the SDK as
// INVALID_ARGUMENT, the code of a refused request.
func CallAgain(err error) bool {
	api, ok := errors.AsType[*pethost.Error](err)
	if !ok {
		return false
	}
	switch api.Code {
	case pethost.CodeUnavailable, pethost.CodeUnknown, pethost.CodeInternal, pethost.CodeDeadlineExceeded:
		return true
	}
	return api.Unwrap() != nil
}
