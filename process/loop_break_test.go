package process_test

import (
	"context"
	"testing"
	"time"

	"github.com/luno/jettison/errors"
	"github.com/luno/jettison/jtest"
	"github.com/luno/jettison/log"

	"github.com/luno/lu"
	"github.com/luno/lu/process"
	"github.com/luno/lu/test"
)

func TestLifecycle(t *testing.T) {
	ev := make(test.EventLog, 100)
	a := &lu.App{OnEvent: ev.Append}

	a.OnStartUp(func(ctx context.Context) error {
		log.Info(ctx, "starting up")
		return nil
	}, lu.WithHookName("basic start hook"))

	a.OnShutdown(func(ctx context.Context) error {
		log.Info(ctx, "stopping")
		return nil
	}, lu.WithHookName("basic stop hook"))

	a.AddProcess(
		process.ContextLoop(noOpContextFunc(), noOpProcessFunc(), process.WithName("noop")),
		process.ContextLoop(noOpContextFunc(), errProcessFunc(), process.WithName("error")),
		process.ContextLoop(noOpContextFunc(), breakProcessFunc(), process.WithName("continue loop")),
		process.ContextLoop(noOpContextFunc(), breakProcessFunc(), process.WithName("break loop"), process.WithBreakableLoop()),
	)

	err := a.Launch(context.Background())
	jtest.AssertNil(t, err)

	time.Sleep(250 * time.Millisecond)

	test.AssertEvents(t, ev,
		test.Event{Type: lu.AppStartup},
		test.Event{Type: lu.PreHookStart, Name: "basic start hook"},
		test.Event{Type: lu.PostHookStart, Name: "basic start hook"},
		test.Event{Type: lu.AppRunning},
		test.AnyOrder(
			test.Event{Type: lu.ProcessStart, Name: "noop"},
			test.Event{Type: lu.ProcessStart, Name: "error"},
			test.Event{Type: lu.ProcessStart, Name: "continue loop"},
			test.Event{Type: lu.ProcessStart, Name: "break loop"},
			test.Event{Type: lu.ProcessEnd, Name: "break loop"},
		),
	)

	err = a.Shutdown()
	jtest.AssertNil(t, err)

	close(ev)
	test.AssertEvents(t, ev,
		test.Event{Type: lu.AppTerminating},
		test.AnyOrder(
			test.Event{Type: lu.ProcessEnd, Name: "noop"},
			test.Event{Type: lu.ProcessEnd, Name: "error"},
			test.Event{Type: lu.ProcessEnd, Name: "continue loop"},
		),
		test.Event{Type: lu.PreHookStop, Name: "basic stop hook"},
		test.Event{Type: lu.PostHookStop, Name: "basic stop hook"},
		test.Event{Type: lu.AppTerminated},
	)
}

func breakProcessFunc() func(context.Context) error {
	return func(_ context.Context) error { return process.ErrBreakContextLoop }
}

func errProcessFunc() func(context.Context) error {
	return func(_ context.Context) error {
		return errors.New("processing fail")
	}
}

func noOpProcessFunc() func(context.Context) error {
	return func(_ context.Context) error {
		return nil
	}
}

func noOpContextFunc() func(context.Context) (context.Context, context.CancelFunc, error) {
	return func(ctx context.Context) (context.Context, context.CancelFunc, error) {
		return ctx, func() {}, nil
	}
}
