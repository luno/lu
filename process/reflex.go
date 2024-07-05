package process

import (
	"cmp"
	"context"
	"time"

	"github.com/luno/jettison/errors"
	"github.com/luno/reflex"
	"github.com/luno/reflex/rpatterns"
	"k8s.io/utils/clock"

	"github.com/luno/lu"
)

var defaultReflexOptions = options{
	sleep:      SleepFor(100 * time.Millisecond),
	errorSleep: ErrorSleepFor(time.Minute),
	clock:      clock.RealClock{},
}

type RunFunc func(in context.Context, s reflex.Spec) error

// ReflexConsumer is the most standard function for generating a lu.Process that wraps a
// reflex consumer/stream loop. Unless you need the Particular temporary behaviour of a
// ReflexLiveConsumer or the multiplexing capability from a ManyReflexConsumer this should
// be your default choice to wait for a role, on a given consumer Spec, with any options
// that need to be defined.
func ReflexConsumer(awaitFunc AwaitRoleFunc, s reflex.Spec, ol ...Option) lu.Process {
	return makeReflexProcess(awaitFunc, s, resolveOptions(defaultReflexOptions, ol))
}

// ManyReflexConsumers allows you to take a number of (probably related) specs and ensure that
// they all run on the same service instance against a given role (and all with the same set of options).
// Unlike the other ReflexConsumer generating functions it returns a slice of lu.Process with a
// cardinality directly related the size of the supplied specs parameter.
func ManyReflexConsumers(awaitFunc AwaitRoleFunc, specs []reflex.Spec, ol ...Option) []lu.Process {
	opts := resolveOptions(defaultReflexOptions, ol)
	ret := make([]lu.Process, 0, len(specs))
	for _, s := range specs {
		ret = append(ret, makeReflexProcess(awaitFunc, s, opts))
	}
	return ret
}

// ReflexLiveConsumer will run a consumer on every instance of the service
// The stream will start from the latest event and the position is not restored on service restart.
func ReflexLiveConsumer(stream reflex.StreamFunc, consumer reflex.Consumer) lu.Process {
	s := rpatterns.NewBootstrapSpec(
		stream,
		rpatterns.MemCursorStore(),
		consumer,
		reflex.WithStreamFromHead(),
	)
	opts := resolveOptions(defaultReflexOptions, []Option{WithName(s.Name())})
	return makeContextProcess(noOpContextFunc, makeProcessFunc(s, reflex.Run), s, opts)
}

// These two lu.Process generating functions handle the standard case with makeReflexProcess
// of generating breakable Reflex Consumer processes and makeContextProcess where we can provide
// an alternative lu ProcessFunc at the core of the process loop. In particular makeContextProcess
// allows us to handle the special case of a ReflexLiveConsumer but since its code is still wrapped
// by makeReflexProcess it is still the same core code that is run for all reflex Consumer processes.
// NOTE: This separation also exposed the internals to allow for simpler and better test coverage.

// makeReflexProcess creates a looping lu.Process that will correctly handle breaking out of the loop
// configured with a reflex.WithStreamToHead() option which will return an error to show that the stream
// head has been reached and thus to consumer/stream can terminate. At its core it wraps the
// makeContextProcess but defines that the code can only execute if it can obtain a role and also
// ensures that the loop is always potentially breakable.
func makeReflexProcess(awaitFunc AwaitRoleFunc, s reflex.Spec, opts options) lu.Process {
	rl := cmp.Or(opts.role, s.Name())
	return makeContextProcess(awaitFunc(rl), makeBreakableProcessFunc(s, reflex.Run), s, opts)
}

// makeContextProcess is the core lu.Process generating function, it allows you to supply a
// ContextFunc that may or may require you to obtain a role (not in the case of a ReflexLiveConsumer)
// and an lu.ProcessFunc which allows you to supply a breakable or non-breakable instance (again
// none breakable in the case of a ReflexLiveConsumer)
func makeContextProcess(contextFunc ContextFunc, processFunc lu.ProcessFunc, s reflex.Spec, opts options) lu.Process {
	opts.afterLoop = func() { _ = s.Stop() }
	p := wrapContextLoop(contextFunc, processFunc, opts)
	return lu.Process{Name: s.Name(), Run: p}
}

// These two process functions handle the cases where we may wish to break out
// of a process loop (makeBreakableProcessFunc) or we can't break as for example
// we are only starting running from the cursor head.
// NOTE: This separation also exposed the internals to allow for simpler and better test coverage.

// makeBreakableProcessFunc wraps makeProcessFunc to handle the special case of
// translating a reflex head reached error into an lu.Process ErrBreakContextLoop
// error so that for consumers configured with the reflex option reflex.WithStreamToHead()
// they can correctly terminate when the cursor head has been reached.
func makeBreakableProcessFunc(s reflex.Spec, run RunFunc) lu.ProcessFunc {
	pf := makeProcessFunc(s, run)
	return func(ctx context.Context) error {
		err := pf(ctx)
		if reflex.IsHeadReachedErr(err) {
			return errors.Wrap(lu.ErrBreakContextLoop, err.Error())
		}
		return err
	}
}

// makeProcessFunc executes the given run function for the given spec and handles
// any expected reflex errors such as contexts being cancelled. However, it should not
// be used as the basis for process loops that may need to terminate early such as those
// configured with the reflex option reflex.WithStreamToHead() as unlike makeBreakableProcessFunc
// they will not return the correct error to let the stream/loop terminate.
func makeProcessFunc(s reflex.Spec, run RunFunc) lu.ProcessFunc {
	return func(ctx context.Context) error {
		err := run(ctx, s)
		if reflex.IsExpected(err) {
			return nil
		}
		return err
	}
}
