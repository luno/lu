package process

import (
	"context"
	"testing"

	"github.com/luno/jettison/errors"
	"github.com/luno/jettison/jtest"
	"github.com/luno/reflex"
	"github.com/luno/reflex/rpatterns"
)

type stream struct{}

func (s *stream) Recv() (*reflex.Event, error) {
	return &reflex.Event{}, nil
}

type headStream struct{}

func (s *headStream) Recv() (*reflex.Event, error) {
	return nil, reflex.ErrHeadReached
}

type consumer struct {
	cancel context.CancelFunc
}

func (c *consumer) Name() string { return "test" }

func (c *consumer) Consume(ctx context.Context, event *reflex.Event) error {
	return errors.New("foo")
}

func (c *consumer) Stop() error {
	c.cancel()
	return nil
}

// Test_ReflexConsumer_afterLoop tests that afterLoop is called at the end of a
// context loop iteration.
func Test_ReflexConsumer_afterLoop(t *testing.T) {
	awaitFunc := func(role string) func(ctx context.Context) (context.Context, context.CancelFunc, error) {
		return func(ctx context.Context) (context.Context, context.CancelFunc, error) {
			return ctx, func() {}, ctx.Err()
		}
	}
	makeStream := func(ctx context.Context, after string, opts ...reflex.StreamOption) (reflex.StreamClient, error) {
		return new(stream), nil
	}
	cstore := rpatterns.MemCursorStore()
	c := new(consumer)
	spec := reflex.NewSpec(makeStream, cstore, c)
	process := ReflexConsumer(awaitFunc, spec, WithErrorSleep(0))
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel // When afterLoop is called, the context will be cancelled and the context loop will end.
	err := process.Run(ctx)
	jtest.Require(t, context.Canceled, err)
}

// Test_ReflexConsumer_breakLoop tests that the process run exits with a stream returns an ErrBreakContextLoop error
// when the stream Recv method returns reflex.ErrHeadReached i.e. a stream configured with the WithStreamToHead option.
func Test_ReflexConsumer_breakLoop(t *testing.T) {
	awaitFunc := func(role string) func(ctx context.Context) (context.Context, context.CancelFunc, error) {
		return func(ctx context.Context) (context.Context, context.CancelFunc, error) {
			return ctx, func() {}, ctx.Err()
		}
	}
	makeStream := func(ctx context.Context, after string, opts ...reflex.StreamOption) (reflex.StreamClient, error) {
		return new(headStream), nil
	}
	cstore := rpatterns.MemCursorStore()
	c := new(consumer)
	spec := reflex.NewSpec(makeStream, cstore, c)
	process := ReflexConsumer(awaitFunc, spec, WithErrorSleep(0), WithBreakableLoop())
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel // When afterLoop is called, the context will be cancelled and the context loop will end.
	err := process.Run(ctx)
	jtest.RequireNil(t, err)
}

func Test_makeBreakableProcessFunc(t *testing.T) {
	ctx := context.Background()
	processingErr := errors.New("Some Error")
	testcases := []struct {
		name string
		run  RunFunc
		err  error
	}{
		{
			name: "None: Nil",
			run:  func(_ context.Context, _ reflex.Spec) error { return nil },
		},
		{
			name: "None: Expected: Stopped",
			run:  func(_ context.Context, _ reflex.Spec) error { return reflex.ErrStopped },
		},
		{
			name: "Break Loop Error: ToHeadStream: Head Reached",
			run:  func(_ context.Context, _ reflex.Spec) error { return reflex.ErrHeadReached },
			err:  ErrBreakContextLoop,
		},
		{
			name: "Error: Processing Error",
			run:  func(_ context.Context, _ reflex.Spec) error { return processingErr },
			err:  processingErr,
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			var s reflex.Spec
			p := makeBreakableProcessFunc(s, tc.run)
			err := p(ctx)
			jtest.Require(t, tc.err, err)
		})
	}
}
