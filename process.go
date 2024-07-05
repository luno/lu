package lu

import (
	"context"

	"github.com/luno/jettison/errors"
	"github.com/luno/jettison/j"
)

// ErrBreakContextLoop acts as a translation error between the reflex domain and the lu process one. It will be
// returned as an alternative when (correctly configured) a reflex stream returns a reflex.ErrSteamToHead error.
var ErrBreakContextLoop = errors.New("the context loop has been stopped", j.C("ERR_f3833d51676ea908"))

// ProcessFunc is a core process. See Process.Run for more details
type ProcessFunc func(ctx context.Context) error

// Process will be a long-running part of the application which,
// if/when it errors, should bring the application down with it.
// It takes a context, if that context is canceled then the Process
// should return as soon as possible.
type Process struct {
	app *App // Will be set before the process is Run

	// Name is used for logging lifecycle events with the Process
	Name string
	// Run takes a context, if that context is canceled then the ProcessFunc
	// should return as soon as possible
	// If Run returns an error, the application will begin the shutdown procedure
	Run ProcessFunc
	// Shutdown will be called to terminate the Process
	// prior to cancelling the Run context.
	// This is for Processes where synchronous shutdown is necessary
	Shutdown func(ctx context.Context) error
}
