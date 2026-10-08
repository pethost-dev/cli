package app

import "sync"

// Together runs calls that do not depend on each other at once, and returns when all have
// ended: the first error in the order given, or nil. The panel and a machine may be far apart,
// so a screen made of two answers asks for both at the same time.
func Together(calls ...func() error) error {
	errs := make([]error, len(calls))
	var group sync.WaitGroup
	for i, call := range calls {
		group.Go(func() { errs[i] = call() })
	}
	group.Wait()

	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
