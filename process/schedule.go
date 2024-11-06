package process

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/luno/jettison/errors"
	"github.com/luno/jettison/j"
	"github.com/luno/jettison/log"
	"github.com/robfig/cron/v3"

	"github.com/luno/lu"
)

func defaultScheduleOptions() options {
	return options{
		errorSleep: ErrorSleepFor(10 * time.Minute),
	}
}

// previousAware if a Schedule object implements the previousAware, it will use this method to determine
// when the last expected run was. This can be used to determine if there were missed intervals between
// the actual last run and the expected last run.
type previousAware interface {
	Previous(now time.Time) time.Time
}

// Schedule must return a time in the same time.Location as given to it in Next
type Schedule cron.Schedule

type cronWithPrevious struct {
	cron.Schedule
}

const maxLookBack = 1000 * 24 * time.Hour

func (c cronWithPrevious) Previous(now time.Time) time.Time {
	lookBack := 10 * time.Minute
	next := c.Next(now)
	prev := next
	for prev.Equal(next) {
		if lookBack > maxLookBack {
			return now
		}
		t := next.Add(-lookBack)
		lookBack = lookBack * 2
		prev = c.Next(t)
	}
	t := prev
	for !t.Equal(next) {
		prev, t = t, c.Next(prev)
	}
	return prev
}

func ParseCron(cronStr string) (Schedule, error) {
	s, err := cron.ParseStandard(cronStr)
	if err != nil {
		return nil, err
	}
	return cronWithPrevious{Schedule: s}, nil
}

type waitSchedule struct {
	// Wait is the (minimum) duration between successful firings of this Schedule
	Wait time.Duration
}

func (r waitSchedule) Next(t time.Time) time.Time {
	return t.Add(r.Wait)
}

// Poll returns a schedule which runs on a given minimum delay (wait) between successful runs.
func Poll(wait time.Duration) Schedule {
	return waitSchedule{Wait: wait}
}

type EveryOption func(s *intervalSchedule)

func WithOffset(offset time.Duration) EveryOption {
	return func(s *intervalSchedule) {
		s.Offset = offset
	}
}

func WithDescription(desc string) EveryOption {
	return func(s *intervalSchedule) {
		s.Description = desc
	}
}

// Every returns a schedule which returns a time equally spaced with a period.
// e.g. if period is time.Hour and Offset is 5*time.Minute then this schedule will return
// 12:05, 13:05, 14:05, etc...
// The time is truncated to the period based on unix time (see time.Truncate for details)
func Every(period time.Duration, opts ...EveryOption) Schedule {
	return newIntervalSchedule(period, opts...)
}

func newIntervalSchedule(period time.Duration, opts ...EveryOption) intervalSchedule {
	s := intervalSchedule{Period: period}
	for _, o := range opts {
		o(&s)
	}
	return s
}

type intervalSchedule struct {
	// Description is a meaningful explanation of the particular IntervalSchedule
	Description string
	// Period is the duration between firings of this Interval
	Period time.Duration
	// Offset is the lag within the period before the first (and subsequent) firing of the Interval
	Offset time.Duration
}

func (r intervalSchedule) Next(t time.Time) time.Time {
	next := t.Truncate(r.Period).Add(r.Offset)
	if !next.After(t) {
		next = next.Add(r.Period)
	}
	return next
}

// Previous this method returns the expected last run time. It uses this to compare with the
// actual last run time and ensure that the process only runs once for all the intervals in between the
// last run time and "now".
func (r intervalSchedule) Previous(now time.Time) time.Time {
	prev := now.Truncate(r.Period).Add(r.Offset)
	if prev.After(now) {
		prev = prev.Add(-1 * r.Period)
	}

	return prev
}

// FixedInterval is deprecated.
// Deprecated: Use Every.
var FixedInterval = Every

// TimeOfDay returns a Schedule that will trigger at the same time every day
// hour is based on the 24-hour clock.
func TimeOfDay(hour, minute int) Schedule {
	return timeOfDaySchedule{Hour: hour, Minute: minute}
}

type timeOfDaySchedule struct {
	Hour, Minute int
}

func (s timeOfDaySchedule) Next(t time.Time) time.Time {
	ti := time.Date(
		t.Year(), t.Month(), t.Day(),
		s.Hour, s.Minute, 0, 0,
		t.Location(),
	)
	if ti.After(t) {
		return ti
	}
	return time.Date(
		t.Year(), t.Month(), t.Day()+1,
		s.Hour, s.Minute, 0, 0,
		t.Location(),
	)
}

// ToTimezone can be used when a schedule is to be run in a particular timezone.
// When using this with zones that observe daylight savings, it's important to be aware of the caveats around
// the boundaries of daylight savings - unit tests demonstrate times being skipped in some cases.
func ToTimezone(s cron.Schedule, tz *time.Location) cron.Schedule {
	return tzSchedule{s: s, tz: tz}
}

type tzSchedule struct {
	s  Schedule
	tz *time.Location
}

func (s tzSchedule) Next(t time.Time) time.Time {
	nxt := s.s.Next(t.In(s.tz))
	return nxt.In(t.Location())
}

type (
	// ContextFunc should create a child context of ctx and return a cancellation function
	// the cancel function will be called after the process has been executed
	// TODO(adam): Offer a CancelCauseFunc option for cancelling the context
	ContextFunc   = func(ctx context.Context) (context.Context, context.CancelFunc, error)
	AwaitRoleFunc = func(role string) ContextFunc
	ScheduledFunc func(ctx context.Context, lastRunTime, runTime time.Time, runID string) error
)

type Cursor interface {
	Get(ctx context.Context, name string) (string, error)
	Set(ctx context.Context, name string, value string) error
}

// Scheduled will create a lu.Process which executes according to a Schedule
func Scheduled(awaitFunc AwaitRoleFunc, curs Cursor,
	name string, when Schedule, f ScheduledFunc,
	ol ...Option,
) lu.Process {
	opts := resolveOptions(defaultScheduleOptions(), append(ol, WithName(name)))

	if opts.role == "" {
		opts.role = opts.name
	}

	runner := scheduleRunner{cursor: curs, o: opts, when: when, f: f}
	process := func(ctx context.Context) time.Duration { return processOnce(ctx, awaitFunc, opts, &runner) }
	wait := func(ctx context.Context, sleep time.Duration) error { return lu.Wait(ctx, opts.clock, sleep) }
	loop := func(ctx context.Context) error { return processLoop(ctx, process, wait) }

	return lu.Process{
		Name: opts.name,
		Run:  loop,
	}
}

type (
	processFunc func(context.Context) time.Duration
	waitFunc    func(context.Context, time.Duration) error
)

// processLoop may panic if processOnce or wait is nil.
func processLoop(ctx context.Context, process processFunc, wait waitFunc) error {
	for ctx.Err() == nil {
		sleep := process(ctx)
		if err := wait(ctx, sleep); err != nil {
			return err
		}
	}
	return context.Cause(ctx)
}

// processOnce may panic if awaitRole is nil or if when calling it returns a nil role.ContextFunc, and
// it may also panic if opts.sleep or opts.errSleep are nil as well; which can be avoided by
// calling resolveOptions on the opts parameter before passing it into this function; it my also panic if
// runner.f is nil as well.
func processOnce(ctx context.Context, awaitRole AwaitRoleFunc, opts options, runner *scheduleRunner) time.Duration {
	err := runWithContext(ctx, awaitRole(opts.role), runner.doNext)
	sleep := opts.sleep()
	if err != nil && !errors.Is(err, context.Canceled) {
		// NoReturnErr: Log critical errors and continue loop
		runner.ErrCount++
		sleep = opts.errorSleep(runner.ErrCount, err)
		opts.errCounter.Inc()
		log.Error(ctx, err)
	} else {
		runner.ErrCount = 0
	}
	return sleep
}

type scheduleRunner struct {
	cursor Cursor
	o      options
	when   Schedule
	f      ScheduledFunc

	ErrCount uint
}

// doNext executes the next iteration of the schedule.
// We use a cursor to keep track of the last completed run.
// If we miss running multiple runs of the cron then we will only attempt to run the latest one.
func (r scheduleRunner) doNext(ctx context.Context) error {
	lastDone, err := getLastRun(ctx, r.cursor, r.o.name)
	if err != nil {
		return err
	}

	next := nextExecution(r.o.clock.Now(), lastDone, r.when, r.o.name)

	ctx = log.ContextWith(ctx, j.MKV{
		"schedule_last": lastDone,
		"schedule_next": next,
	})

	if r.o.maxErrors > 0 && r.ErrCount >= r.o.maxErrors {
		return setRunDone(ctx, next, r.cursor, r.o.name)
	}

	if err := lu.WaitUntil(ctx, r.o.clock, next); err != nil {
		return err
	}

	runID := fmt.Sprintf("%s_%d", r.o.name, next.Unix())

	ctx = log.ContextWith(ctx, j.MKV{"schedule_run_id": runID})

	if err := r.f(ctx, lastDone, next, runID); err != nil {
		return err
	}

	return setRunDone(ctx, next, r.cursor, r.o.name)
}

func nextExecution(now, last time.Time, s Schedule, name string) time.Time {
	fromNow := s.Next(now)
	if last.IsZero() {
		return fromNow
	}

	// If the expected last run does not match the actual last run, we will
	// favour the expected last run if the schedule implements the right interface.
	prev, ok := s.(previousAware)
	if ok {
		expectedLastRun := prev.Previous(now)
		if !last.Equal(expectedLastRun) {
			return expectedLastRun
		}
	}

	fromLast := s.Next(last)
	if fromLast.Before(fromNow) {
		scheduleCursorLag.WithLabelValues(name).Set(fromNow.Sub(fromLast).Seconds())
		return fromLast.In(now.Location())
	}
	return fromNow
}

// getLastRun returns the last successful run timestamp.
// Returns a zero time if no run is found.
func getLastRun(ctx context.Context, curs Cursor, name string) (time.Time, error) {
	val, err := curs.Get(ctx, name)
	if err != nil {
		return time.Time{}, err
	}

	if val == "" {
		// Return zero time if no cursor.
		return time.Time{}, nil
	}

	unixSec, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return time.Time{}, err
	}

	return time.Unix(unixSec, 0), nil
}

func setRunDone(ctx context.Context, t time.Time, curs Cursor, name string) error {
	unixSec := strconv.FormatInt(t.Unix(), 10)
	return curs.Set(ctx, name, unixSec)
}
