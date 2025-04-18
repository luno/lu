package lu_test

import (
	"context"
	"io"
	"os"
	"testing"
	"time"

	"github.com/luno/jettison/errors"
	"github.com/luno/jettison/jtest"
	"github.com/luno/jettison/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/utils/clock"

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
		lu.Process{
			Name: "one",
			Run: func(ctx context.Context) error {
				log.Info(ctx, "one")
				<-ctx.Done()
				return context.Cause(ctx)
			},
		},
		lu.Process{
			Name: "two",
			Run: func(ctx context.Context) error {
				log.Info(ctx, "two")
				<-ctx.Done()
				return context.Cause(ctx)
			},
		},
		lu.Process{
			Name: "three",
			Run: func(ctx context.Context) error {
				log.Info(ctx, "three")
				<-ctx.Done()
				return context.Cause(ctx)
			},
		},
		process.ContextLoop(
			func(ctx context.Context) (context.Context, context.CancelFunc, error) { return ctx, func() {}, nil },
			func(ctx context.Context) error { return process.ErrBreakContextLoop },
			process.WithName("break loop"),
			process.WithBreakableLoop()),
	)

	err := a.Launch(context.Background())
	jtest.AssertNil(t, err)

	time.Sleep(250 * time.Millisecond)

	err = a.Shutdown()
	jtest.AssertNil(t, err)

	close(ev)
	test.AssertEvents(t, ev,
		test.Event{Type: lu.AppStartup},
		test.Event{Type: lu.PreHookStart, Name: "basic start hook"},
		test.Event{Type: lu.PostHookStart, Name: "basic start hook"},
		test.AnyOrder(
			test.Event{Type: lu.ProcessStart, Name: "one"},
			test.Event{Type: lu.ProcessStart, Name: "two"},
			test.Event{Type: lu.ProcessStart, Name: "three"},
			test.Event{Type: lu.ProcessStart, Name: "break loop"},
		),
		test.AnyOrder(
			test.Event{Type: lu.AppRunning},
			test.Event{Type: lu.ProcessEnd, Name: "break loop"},
		),
		test.Event{Type: lu.AppTerminating},
		test.AnyOrder(
			test.Event{Type: lu.ProcessEnd, Name: "one"},
			test.Event{Type: lu.ProcessEnd, Name: "two"},
			test.Event{Type: lu.ProcessEnd, Name: "three"},
		),
		test.Event{Type: lu.PreHookStop, Name: "basic stop hook"},
		test.Event{Type: lu.PostHookStop, Name: "basic stop hook"},
		test.Event{Type: lu.AppTerminated},
	)
}

func TestShutdownWithParentContext(t *testing.T) {
	var a lu.App
	a.AddProcess(lu.Process{
		Run: func(ctx context.Context) error {
			<-ctx.Done()
			return context.Cause(ctx)
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	err := a.Launch(ctx)
	jtest.AssertNil(t, err)

	require.Eventually(t, func() bool {
		select {
		case <-a.WaitForShutdown():
			return true
		default:
			return false
		}
	}, 2*time.Second, 100*time.Millisecond)

	err = a.Shutdown()
	jtest.Assert(t, context.DeadlineExceeded, err)
}

func TestProcessShutdown(t *testing.T) {
	testCases := []struct {
		name     string
		setupApp func(a *lu.App)

		expErr error
	}{
		{name: "empty"},
		{
			name: "cancellable",
			setupApp: func(a *lu.App) {
				a.ShutdownTimeout = 100 * time.Millisecond
				a.AddProcess(lu.Process{Shutdown: func(ctx context.Context) error {
					<-ctx.Done()
					return context.Cause(ctx)
				}})
			},
			expErr: context.DeadlineExceeded,
		},
		{
			name: "dependents",
			setupApp: func(a *lu.App) {
				ch := make(chan struct{})
				p1 := lu.Process{Shutdown: func(ctx context.Context) error { <-ch; return nil }}
				p2 := lu.Process{Shutdown: func(ctx context.Context) error { close(ch); return nil }}
				p3 := lu.Process{Shutdown: func(ctx context.Context) error { <-ch; return nil }}
				a.AddProcess(p1, p2, p3)
			},
		},
		{
			name: "blocked",
			setupApp: func(a *lu.App) {
				a.ShutdownTimeout = 100 * time.Millisecond
				ch := make(chan struct{})
				a.AddProcess(lu.Process{Shutdown: func(ctx context.Context) error { <-ch; return nil }})
			},
			expErr: context.DeadlineExceeded,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var a lu.App
			if tc.setupApp != nil {
				tc.setupApp(&a)
			}

			err := a.Launch(context.Background())
			jtest.RequireNil(t, err)

			err = a.Shutdown()
			jtest.Assert(t, tc.expErr, err)
		})
	}
}

func TestRunningProcesses(t *testing.T) {
	testCases := []struct {
		name             string
		processes        []lu.Process
		expShutdownError error
		expRunning       []string
	}{
		{name: "nil"},
		{
			name: "blocker",
			processes: []lu.Process{
				{Name: "blocker", Run: func(ctx context.Context) error {
					var c chan struct{}
					<-c
					return nil
				}},
			},
			expShutdownError: context.DeadlineExceeded,
			expRunning:       []string{"blocker"},
		},
		{
			name: "non-blocker",
			processes: []lu.Process{
				{Name: "gogo", Run: func(ctx context.Context) error {
					<-ctx.Done()
					return nil
				}},
			},
		},
		{
			name: "one blocker among others",
			processes: []lu.Process{
				{Name: "gogo", Run: func(ctx context.Context) error {
					<-ctx.Done()
					return nil
				}},
				{Name: "blocker", Run: func(ctx context.Context) error {
					var c chan struct{}
					<-c
					return nil
				}},
			},
			expShutdownError: context.DeadlineExceeded,
			expRunning:       []string{"blocker"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			a := lu.App{ShutdownTimeout: 100 * time.Millisecond}
			a.AddProcess(tc.processes...)

			jtest.RequireNil(t, a.Launch(context.Background()))
			jtest.Assert(t, tc.expShutdownError, a.Shutdown())
			require.Equal(t, tc.expRunning, a.RunningProcesses())
		})
	}
}

func TestRun(t *testing.T) {
	tests := []struct {
		name      string
		app       func(t *testing.T) *lu.App
		expExit   int
		expCtxErr error
	}{
		{
			name: "not using pid",
			app: func(t *testing.T) *lu.App {
				var a lu.App
				a.AddProcess(process.NoOp())
				return &a
			},
			expExit:   1,
			expCtxErr: context.DeadlineExceeded,
		},
		{
			name: "normal app",
			app: func(t *testing.T) *lu.App {
				a := lu.App{UseProcessFile: true}
				a.AddProcess(process.NoOp())
				return &a
			},
			expExit:   1,
			expCtxErr: context.DeadlineExceeded,
		},
		{
			name: "app fails to start",
			app: func(t *testing.T) *lu.App {
				a := lu.App{UseProcessFile: true}
				a.OnStartUp(func(ctx context.Context) error {
					return io.ErrUnexpectedEOF
				})
				a.AddProcess(process.NoOp())
				return &a
			},
			expExit: 1,
		},
		{
			name: "app fails to shutdown",
			app: func(t *testing.T) *lu.App {
				a := lu.App{UseProcessFile: true}
				a.OnShutdown(func(ctx context.Context) error {
					return io.ErrUnexpectedEOF
				})
				a.AddProcess(process.NoOp())
				return &a
			},
			expExit:   1,
			expCtxErr: context.DeadlineExceeded,
		},
		{
			name: "app shuts itself down",
			app: func(t *testing.T) *lu.App {
				var a lu.App
				a.AddProcess(lu.Process{Run: func(ctx context.Context) error {
					if err := lu.Wait(ctx, clock.RealClock{}, 10*time.Millisecond); err != nil {
						return err
					}
					return errors.New("terminate the app please")
				}})
				return &a
			},
			expExit: 1,
		},
		{
			name: "app shuts itself down and handles it",
			app: func(t *testing.T) *lu.App {
				var a lu.App

				termErr := errors.New("terminate the app please")

				a.AddProcess(lu.Process{Run: func(ctx context.Context) error {
					if err := lu.Wait(ctx, clock.RealClock{}, 10*time.Millisecond); err != nil {
						return err
					}
					return termErr
				}})
				a.OnShutdownErr = func(ctx context.Context, err error) error {
					if errors.Is(err, termErr) {
						return nil
					}
					return err
				}
				return &a
			},
			expExit: 0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			t.Cleanup(cancel)
			lu.SetBackgroundContextForTesting(t, ctx)

			exit := tc.app(t).Run()
			assert.Equal(t, tc.expExit, exit)
			_, err := os.Open("/tmp/lu.pid")
			assert.True(t, os.IsNotExist(err))

			jtest.Assert(t, tc.expCtxErr, ctx.Err())
		})
	}
}

func TestWaitFor(t *testing.T) {
	tests := []struct {
		name   string
		ctx    func(t *testing.T) context.Context
		ch     func(t *testing.T) chan int
		exp    int
		expErr error
	}{
		{
			name: "canceled with io error",
			ctx: func(t *testing.T) context.Context {
				ctx, cancel := context.WithCancelCause(context.Background())
				cancel(io.ErrUnexpectedEOF)
				return ctx
			},
			ch: func(t *testing.T) chan int {
				return nil
			},
			expErr: io.ErrUnexpectedEOF,
		},
		{
			name: "canceled",
			ctx: func(t *testing.T) context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
			ch: func(t *testing.T) chan int {
				return nil
			},
			expErr: context.Canceled,
		},
		{
			name: "time out",
			ctx: func(t *testing.T) context.Context {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
				t.Cleanup(cancel)
				return ctx
			},
			ch: func(t *testing.T) chan int {
				return nil
			},
			expErr: context.DeadlineExceeded,
		},
		{
			name: "success",
			ctx: func(t *testing.T) context.Context {
				return context.Background()
			},
			ch: func(t *testing.T) chan int {
				ch := make(chan int, 1)
				ch <- 1
				close(ch)
				return ch
			},
			exp: 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			i, err := lu.WaitFor(tc.ctx(t), tc.ch(t))
			jtest.Require(t, tc.expErr, err)
			assert.Equal(t, tc.exp, i)
		})
	}
}

func TestNoApp(t *testing.T) {
	require.Nil(t, lu.NoApp())
}
