package process

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/utils/clock"
)

type options struct {
	name string
	// Any override role to await on instead of just waiting on the name of the service
	role string
	// Returns the time to sleep if no error occurs. Default 0.
	sleep SleepFunc
	// Config for the time to sleep if an error occurs. Defaults to a constant 10s.
	errorSleep ErrorSleepFunc
	maxErrors  uint
	clock      clock.Clock
	// Callback function that's called after a loop iteration but before the next iteration.
	// It's for internal use only, and shouldn't be exposed outside this package.
	// Default is a no-op.
	afterLoop func()

	// Counts the errors for a specific process, the default increments the error counter metric in metrics.go with the process name as a label.
	errCounter prometheus.Counter

	// EXPERIMENTAL: Added for the purposes of production testing isolated cases with the new breakable behaviour
	// Flag to determine if we allow loops to break when an ErrBreakContextLoop is returned from the process function.
	isBreakableLoop bool

	// Determines if the process should be monitored by the app when running to handle abnormal terminations.
	monitor bool
}

// SleepFunc returns how long to sleep between loops when there was no error.
type SleepFunc func() time.Duration

// SleepFor returns a SleepFunc that returns a fixed sleep duration.
func SleepFor(dur time.Duration) SleepFunc {
	return func() time.Duration {
		return dur
	}
}

// ErrorSleepFunc returns how long to sleep when we encounter an error
// `errCount` is how many times we've had an error, always > 0
// `err` is the latest error
//
// The function should not call time.Sleep itself, instead it
// should return the amount of time that will be used with lu.Wait
type ErrorSleepFunc func(errCount uint, err error) time.Duration

// ErrorSleepFor will return the same amount of time for every error
func ErrorSleepFor(dur time.Duration) ErrorSleepFunc {
	return func(uint, error) time.Duration {
		return dur
	}
}

// MakeErrorSleepFunc specifies behaviour for how long to sleep when a function errors repeatedly.
// When error count is between 1 and r (1 <= c <= r) we will retry immediately.
// Then when error count is more than r, we will sleep for d.
// The backoff array is used as multipliers on d to determine the amount of sleep.
func MakeErrorSleepFunc(r uint, d time.Duration, backoff []uint) ErrorSleepFunc {
	return func(errCount uint, err error) time.Duration {
		if errCount <= r {
			return 0
		}
		if len(backoff) == 0 {
			return d
		}
		backoffIdx := int(errCount) - 1
		if r > 0 {
			backoffIdx -= int(r)
		}
		if backoffIdx >= len(backoff) {
			backoffIdx = len(backoff) - 1
		}
		return d * time.Duration(backoff[backoffIdx])
	}
}

var DefaultBackoff = []uint{1, 2, 5, 10, 20, 50, 100}

type Option func(*options)

// resolveOptions applies the supplied LoopOptions to the defaults
func resolveOptions(defaults options, opts []Option) options {
	res := defaults
	for _, opt := range opts {
		opt(&res)
	}
	if res.sleep == nil {
		res.sleep = SleepFor(0)
	}
	if res.clock == nil {
		res.clock = clock.RealClock{}
	}
	if res.errorSleep == nil {
		res.errorSleep = ErrorSleepFor(10 * time.Second)
	}
	if res.afterLoop == nil {
		res.afterLoop = func() {}
	}
	if res.errCounter == nil {
		res.errCounter = processErrors.With(label(res.name))
	}

	return res
}

func WithName(name string) Option {
	return func(o *options) {
		o.name = name
	}
}

// WithRole allows you to specify a custom role to await on when coordinating services which may be picked up by
// supporting lu Process builder like ReflexConsumer.
func WithRole(role string) Option {
	return func(o *options) {
		o.role = role
	}
}

// WithSleep is a shortcut for WithSleepFunc + SleepFor.
// The process will sleep  for `d` on every successful loop.
func WithSleep(d time.Duration) Option {
	return func(o *options) {
		o.sleep = SleepFor(d)
	}
}

// WithSleepFunc sets the handler for determining how long ot sleep between loops when there was no error.
func WithSleepFunc(f SleepFunc) Option {
	return func(o *options) {
		o.sleep = f
	}
}

// WithErrorSleep is a shortcut for WithErrorSleepFunc + ErrorSleepFor
// The process will sleep for `d` on every error.
func WithErrorSleep(d time.Duration) Option {
	return WithErrorSleepFunc(ErrorSleepFor(d))
}

// WithErrorSleepFunc sets the handler for determining how long to sleep for
// after an error. You can use ErrorSleepFor to sleep for a fixed amount of time:
//
// p := Loop(f, WithErrorSleepFunc(ErrorSleepFor(time.Minute)))
//
// or you can use MakeErrorSleepFunc to get some more complex behaviour
//
// p := Loop(f, WithErrorSleepFunc(MakeErrorSleepFunc(5, time.Minute, []uint{1,2,5,10})))
func WithErrorSleepFunc(f ErrorSleepFunc) Option {
	return func(o *options) {
		o.errorSleep = f
	}
}

// WithClock overwrites the clock field with the value provided.
// Mainly used during testing.
func WithClock(clock clock.Clock) Option {
	return func(o *options) {
		o.clock = clock
	}
}

// WithMaxErrors sets the number errors that will cause us to give up
// on the currently running process.
// A value of 0 (the default) means we will never give up.
// A value of 1 means we give up after the first error, 2 the second and
// so on.
func WithMaxErrors(v uint) Option {
	return func(o *options) {
		o.maxErrors = v
	}
}

// WithBreakableLoop sets a flag that determines if when an ErrBreakContextLoop is returned
// from a process function if that context loop itself can be allowed to terminate as well.
// EXPERIMENTAL: Added for the purposes of production testing isolated cases with the new breakable behaviour
func WithBreakableLoop() Option {
	return func(o *options) {
		o.isBreakableLoop = true
	}
}

// WithMonitor sets a flag that determines if the process should be monitored by the app to
// handle abnormal terminations
func WithMonitor() Option {
	return func(o *options) {
		o.monitor = true
	}
}
