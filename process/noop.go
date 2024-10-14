package process

import (
	"context"

	"github.com/luno/lu"
)

// NoOp is a Process which doesn't do anything but runs until the app is terminated.
func NoOp() lu.Process {
	return lu.Process{
		Name: "noop",
		Run: func(ctx context.Context) error {
			<-ctx.Done()
			return context.Cause(ctx)
		},
	}
}
