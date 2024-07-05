package process_test

import (
	"context"
	"testing"
	"time"

	"github.com/luno/jettison/errors"
	"github.com/luno/jettison/j"
	"github.com/luno/jettison/jtest"
	"github.com/stretchr/testify/assert"
	"k8s.io/utils/clock"
	clock_testing "k8s.io/utils/clock/testing"

	"github.com/luno/lu"
	"github.com/luno/lu/process"
)

func ctxRetry(ctx context.Context) (context.Context, context.CancelFunc, error) {
	newCtx, cancelFunc := context.WithCancel(ctx)
	return newCtx, cancelFunc, nil
}

func alwaysSucceed() func(ctx context.Context) error {
	return func(ctx context.Context) error { return nil }
}

func failTimes(times int) func(ctx context.Context) error {
	var failCount int
	return func(ctx context.Context) error {
		if failCount >= times {
			return nil
		}
		failCount++
		return errors.New("failTimes", j.MKV{"fail_count": failCount})
	}
}

func TestRetry_success(t *testing.T) {
	ctx := context.Background()
	p := process.Retry(alwaysSucceed())
	assert.Nil(t, p.Run(ctx))
}

func TestRetry_retries(t *testing.T) {
	ctx := context.Background()
	p := process.Retry(failTimes(3), process.WithErrorSleep(0))
	assert.Nil(t, p.Run(ctx))
}

func TestContextRetry_success(t *testing.T) {
	ctx := context.Background()
	p := process.ContextRetry(ctxRetry, alwaysSucceed())
	assert.Empty(t, p.Name)
	assert.Nil(t, p.Shutdown)
	assert.Nil(t, p.Run(ctx))
}

func TestContextRetry_retries(t *testing.T) {
	ctx := context.Background()
	fakeClock := &testClock{
		FakeClock: *clock_testing.NewFakeClock(time.Now()),
	}

	errSleepTime := time.Second
	p := process.ContextRetry(ctxRetry, failTimes(3),
		process.WithName("retry-test"),
		process.WithErrorSleep(errSleepTime),
		process.WithClock(fakeClock),
		process.WithSleep(time.Hour), // doen't get used in ContextRetry
	)
	assert.Equal(t, p.Name, "retry-test")
	assert.Nil(t, p.Shutdown)
	assert.Nil(t, p.Run(ctx))
	assert.Equal(t, []time.Duration{errSleepTime, errSleepTime, errSleepTime},
		fakeClock.newTimerCalls,
		"Expecting call to call clock.NewTimer 3 times, once for each failure")
}

func TestContextRetry_cancelRoleContext(t *testing.T) {
	ch := make(chan context.CancelFunc)

	fnGetRole := func(ctx context.Context) (context.Context, context.CancelFunc, error) {
		ctx, cancel := context.WithCancel(ctx)
		ch <- cancel
		return ctx, cancel, nil
	}

	p := process.ContextRetry(
		fnGetRole,
		func(ctx context.Context) error {
			// Drop straight into sleep
			return errors.New("some error")
		},
		process.WithSleepFunc(func() time.Duration {
			return time.Second * 10
		}),
	)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	go func() {
		_ = p.Run(ctx)
	}()

	// Start one loop and send it to sleep
	cancel1 := <-ch
	// Cancel it, sending to the getCtx again
	cancel1()

	select {
	case nextCancel := <-ch:
		t.Cleanup(nextCancel)
	case <-time.After(time.Second):
		assert.Fail(t, "timeout waiting for next getCtx")
	}
}

func TestContextRetry_cancelLuContext(t *testing.T) {
	chStart := make(chan struct{})
	chDone := make(chan struct{})

	fnGetRole := func(ctx context.Context) (context.Context, context.CancelFunc, error) {
		ctx, cancel := context.WithCancel(ctx)
		return ctx, cancel, nil
	}

	p := process.ContextRetry(
		fnGetRole,
		func(ctx context.Context) error {
			close(chStart)
			// Drop straight into sleep
			return errors.New("some error")
		},
		process.WithSleepFunc(func() time.Duration {
			return time.Second * 10
		}),
	)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	go func() {
		_ = p.Run(ctx)
		close(chDone)
	}()

	select {
	case <-chStart:
	case <-time.After(time.Second):
		assert.Fail(t, "timeout wiating for process to start")
	}

	time.Sleep(time.Second * 5)

	// Lu cancelles process
	cancel()

	select {
	case <-chStart:
	case <-time.After(time.Second):
		assert.Fail(t, "timeout waiting for next getCtx")
	}
}

// testClock is a clock.Clock implementation that returns a fakeTimer and keeps
// track of each call to NewTimer.
type testClock struct {
	clock_testing.FakeClock
	newTimerCalls []time.Duration
}

func (f *testClock) NewTimer(d time.Duration) clock.Timer {
	f.newTimerCalls = append(f.newTimerCalls, d)
	return newFakeTime()
}

// fakeTimer is a clock.Timer implementation that doesn't block and never
// reports that is has triggered.
type fakeTimer struct {
	c <-chan time.Time
}

func newFakeTime() *fakeTimer {
	c := make(chan time.Time)
	close(c) // close so it doesn't block
	return &fakeTimer{c: c}
}

func (t *fakeTimer) C() <-chan time.Time {
	return t.c
}

func (t *fakeTimer) Stop() bool {
	return false
}

func (t *fakeTimer) Reset(_ time.Duration) bool {
	return false
}

func TestContextLoopMaxError(t *testing.T) {
	fail := errors.New("failure")

	testCases := []struct {
		name        string
		maxErrors   uint
		returnErr   error
		expectedErr error
	}{
		{
			name:        "run forever",
			maxErrors:   0,
			returnErr:   fail,
			expectedErr: context.Canceled,
		},
		{
			name:        "fail after one",
			maxErrors:   1,
			returnErr:   fail,
			expectedErr: fail,
		},

		{
			name:        "fail after two",
			maxErrors:   2,
			returnErr:   fail,
			expectedErr: fail,
		},

		{
			name:      "fail after break",
			maxErrors: 0,
			returnErr: lu.ErrBreakContextLoop,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var iteration int

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			p := process.ContextLoop(
				func(ctx context.Context) (context.Context, context.CancelFunc, error) { return ctx, func() {}, nil },
				func(ctx context.Context) error {
					iteration = iteration + 1
					if iteration > 10 {
						cancel()
					}

					return tc.returnErr
				},
				process.WithErrorSleep(0),
				process.WithMaxErrors(tc.maxErrors),
				process.WithBreakableLoop(),
			)

			err := p.Run(ctx)
			jtest.Require(t, tc.expectedErr, err)
		})
	}
}

func TestErrorSleepConfig(t *testing.T) {
	testCases := []struct {
		name      string
		sleepFunc process.ErrorSleepFunc
		expSleeps []time.Duration
	}{
		{
			name:      "constant",
			sleepFunc: process.MakeErrorSleepFunc(0, time.Second, nil),
			expSleeps: []time.Duration{time.Second, time.Second, time.Second, time.Second, time.Second, time.Second},
		},
		{
			name:      "quick retries then falls back to sleep",
			sleepFunc: process.MakeErrorSleepFunc(3, time.Second, nil),
			expSleeps: []time.Duration{0, 0, 0, time.Second},
		},
		{
			name:      "default backoff",
			sleepFunc: process.MakeErrorSleepFunc(0, 100*time.Millisecond, process.DefaultBackoff),
			expSleeps: []time.Duration{
				100 * time.Millisecond,
				200 * time.Millisecond,
				500 * time.Millisecond,
				time.Second,
				2 * time.Second,
				5 * time.Second,
				10 * time.Second,
				10 * time.Second,
			},
		},
		{
			name:      "backoff with retries",
			sleepFunc: process.MakeErrorSleepFunc(2, time.Second, []uint{1, 2, 3}),
			expSleeps: []time.Duration{
				0, 0,
				time.Second, 2 * time.Second, 3 * time.Second,
				3 * time.Second,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			for idx, expDur := range tc.expSleeps {
				dur := tc.sleepFunc(uint(idx+1), context.DeadlineExceeded)
				assert.Equal(t, expDur, dur)
			}
		})
	}
}

func TestSleepContextCancelled(t *testing.T) {
	ch := make(chan context.CancelFunc)

	fnGetRole := func(ctx context.Context) (context.Context, context.CancelFunc, error) {
		ctx, cancel := context.WithCancel(ctx)
		ch <- cancel
		return ctx, cancel, nil
	}

	p := process.ContextLoop(
		fnGetRole,
		func(ctx context.Context) error {
			// Drop straight into sleep
			return nil
		},
		process.WithSleepFunc(func() time.Duration {
			return time.Second * 10
		}),
	)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	go func() {
		_ = p.Run(ctx)
	}()

	// Start one loop and send it to sleep
	cancel1 := <-ch
	// Cancel it, sending to the getCtx again
	cancel1()

	select {
	case nextCancel := <-ch:
		t.Cleanup(nextCancel)
	case <-time.After(time.Second):
		assert.Fail(t, "timeout waiting for next getCtx")
	}
}
