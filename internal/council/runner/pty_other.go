//go:build !windows

package runner

import "context"

// StartPTY refuses on every platform that is not Windows.
//
// A build tag rather than a runtime check, and that is the honest shape: a
// Unix binary does not carry a pseudoconsole path it could never take, so the
// refusal is a fact about the program rather than a guess made at the moment
// somebody asks for a pane. openpty plus Setctty is a real alternative and it
// is unwritten and unmeasured — §9.53 records that as owed, not as absent by
// accident.
func StartPTY(_ context.Context, _ Spec, _, _ int, _ chan<- PTYChunk) (PTYSession, error) {
	return nil, ErrPTYUnsupported
}
