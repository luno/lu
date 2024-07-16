package lu

import (
	"context"
	"github.com/luno/jettison/errors"
	"github.com/luno/jettison/j"
)

var errProcessPanicked = errors.New("process panicked during run", j.C("ERR_1d22d6c1ff24b023"))

func shouldExit(ctx context.Context, err error) bool {
	return ctx.Err() != nil || errors.IsAny(err, nil, errProcessPanicked, ErrBreakContextLoop)
}

func cleanPanic() func(err *error) {
	return func(err *error) {
		if rec := recover(); rec != nil {
			*err = errors.Wrap(errProcessPanicked, "", j.KV("panic_value", rec))
		}
	}
}
