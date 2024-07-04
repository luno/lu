package process

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"k8s.io/utils/clock"
	clocktesting "k8s.io/utils/clock/testing"
)

func Test_ResolveOptions(t *testing.T) {
	cc := []struct {
		name     string
		defaults options
		opts     []Option
		want     options
	}{
		{
			name: "no options",
			want: options{
				clock:      clock.RealClock{},
				sleep:      SleepFor(0),
				errorSleep: ErrorSleepFor(10 * time.Second),
				errCounter: processErrors.With(label("")),
			},
		},
		{
			name: "name",
			opts: []Option{WithName("test-name")},
			want: options{
				name:       "test-name",
				clock:      clock.RealClock{},
				sleep:      SleepFor(0),
				errorSleep: ErrorSleepFor(10 * time.Second),
				errCounter: processErrors.With(label("test-name")),
			},
		},
		{
			name: "sleep",
			opts: []Option{WithSleep(time.Hour)},
			want: options{
				clock:      clock.RealClock{},
				sleep:      SleepFor(time.Hour),
				errorSleep: ErrorSleepFor(10 * time.Second),
				errCounter: processErrors.With(label("")),
			},
		},
		{
			name: "error sleep",
			opts: []Option{WithErrorSleepFunc(ErrorSleepFor(3 * time.Hour))},
			want: options{
				clock:      clock.RealClock{},
				sleep:      SleepFor(0),
				errorSleep: ErrorSleepFor(3 * time.Hour),
				errCounter: processErrors.With(label("")),
			},
		},
		{
			name: "clock is defaulted to valid value",
			opts: []Option{WithClock(nil)},
			want: options{
				clock:      clock.RealClock{},
				sleep:      SleepFor(0),
				errorSleep: ErrorSleepFor(10 * time.Second),
				errCounter: processErrors.With(label("")),
			},
		},
		{
			name:     "default is used",
			defaults: options{errorSleep: ErrorSleepFor(time.Minute)},
			want: options{
				clock:      clock.RealClock{},
				sleep:      SleepFor(0),
				errorSleep: ErrorSleepFor(time.Minute),
				errCounter: processErrors.With(label("")),
			},
		},
		{
			name: "overriding default with bad value falls back to safe",
			defaults: options{
				clock:      &clocktesting.FakeClock{},
				sleep:      SleepFor(0),
				errorSleep: ErrorSleepFor(time.Minute),
				errCounter: processErrors.With(label("")),
			},
			opts: []Option{
				WithClock(nil),
				WithErrorSleepFunc(nil),
			},
			want: options{
				clock:      clock.RealClock{},
				sleep:      SleepFor(0),
				errorSleep: ErrorSleepFor(10 * time.Second),
				errCounter: processErrors.With(label("")),
			},
		},
		{
			name: "negative sleep values",
			opts: []Option{WithSleep(-time.Nanosecond), WithErrorSleepFunc(ErrorSleepFor(-time.Nanosecond))},
			want: options{
				clock:      clock.RealClock{},
				sleep:      SleepFor(-time.Nanosecond),
				errorSleep: ErrorSleepFor(-time.Nanosecond),
				errCounter: processErrors.With(label("")),
			},
		},
		{
			name: "sleep func",
			opts: []Option{WithSleepFunc(func() time.Duration { return time.Hour })},
			want: options{
				clock:      clock.RealClock{},
				sleep:      SleepFor(time.Hour),
				errorSleep: ErrorSleepFor(10 * time.Second),
				errCounter: processErrors.With(label("")),
			},
		},
	}

	for _, c := range cc {
		t.Run(c.name, func(t *testing.T) {
			o := resolveOptions(c.defaults, c.opts)
			if c.want.sleep == nil {
				assert.Nil(t, o.sleep)
			} else {
				assert.Equal(t, c.want.sleep(), o.sleep())
				c.want.sleep = nil
				o.sleep = nil
			}
			if c.want.errorSleep == nil {
				assert.Nil(t, o.errorSleep)
			} else {
				assert.Equal(t, c.want.errorSleep(1, nil), o.errorSleep(1, nil))
				c.want.errorSleep = nil
				o.errorSleep = nil
			}
			o.afterLoop = nil
			assert.Equal(t, c.want, o)
		})
	}
}
