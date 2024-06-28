package process

import (
	"context"
	"testing"
	"time"

	"github.com/luno/jettison/errors"
	"github.com/luno/jettison/j"
	"github.com/luno/jettison/jtest"
	"github.com/robfig/cron/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	clocktesting "k8s.io/utils/clock/testing"
)

type run struct {
	runID    string
	lastRun  time.Time
	retError error
}

var noRun run

type expectRun struct {
	t          *testing.T
	expRun     run
	alreadyRan bool
}

type memCursor map[string]string

func (m memCursor) Get(_ context.Context, name string) (string, error) {
	return m[name], nil
}

func (m memCursor) Set(_ context.Context, name string, value string) error {
	m[name] = value
	return nil
}

//goland:noinspection GoExportedFuncWithUnexportedType
func ExpectRun(t *testing.T, run run) *expectRun {
	return &expectRun{
		t:      t,
		expRun: run,
	}
}

func (r *expectRun) Run(_ context.Context, _, _ time.Time, runID string) error {
	if r.alreadyRan {
		r.t.Fatal("duplicate run")
	}
	assert.Equal(r.t, r.expRun.runID, runID)
	r.alreadyRan = true
	return r.expRun.retError
}

func (r *expectRun) AssertUsed() {
	assert.Equal(r.t, r.alreadyRan, r.expRun != noRun)
}

func TestSchedule(t *testing.T) {
	const (
		ts20220121Midnight = "1642723200"
		ts20220122Midnight = "1642809600"
		ts20220123Midnight = "1642896000"
		ts20220123Exact    = "1642944241"
	)
	const cursorName = "test_schedule"

	testCases := []struct {
		name string

		startTime   time.Time
		startCursor string

		when cron.Schedule

		setClockTo time.Time

		expRun    run
		expErr    error
		expCursor string
	}{
		{
			name:      "run next with new cursor",
			startTime: must(time.Parse(time.RFC3339, "2022-01-22T13:24:01Z")),

			when: Every(24 * time.Hour),

			setClockTo: must(time.Parse(time.RFC3339, "2022-01-23T00:00:00Z")),

			expRun:    run{runID: cursorName + "_" + ts20220123Midnight},
			expCursor: ts20220123Midnight,
		},
		{
			name:      "poll next with new cursor",
			startTime: must(time.Parse(time.RFC3339, "2022-01-22T13:24:01Z")),

			when: Poll(24 * time.Hour),

			setClockTo: must(time.Parse(time.RFC3339, "2022-01-23T13:24:01Z")),

			expRun:    run{runID: cursorName + "_" + ts20220123Exact},
			expCursor: ts20220123Exact,
		},
		{
			name:        "run before the previous, runs immediately",
			startTime:   must(time.Parse(time.RFC3339, "2022-01-22T13:24:01Z")),
			startCursor: ts20220121Midnight,

			when: Every(24 * time.Hour),

			expRun:    run{runID: cursorName + "_" + ts20220122Midnight},
			expCursor: ts20220122Midnight,
		},
		{
			name:        "poll before the previous, runs immediately",
			startTime:   must(time.Parse(time.RFC3339, "2022-01-22T13:24:01Z")),
			startCursor: ts20220121Midnight,

			when: Poll(24 * time.Hour),

			expRun:    run{runID: cursorName + "_" + ts20220122Midnight},
			expCursor: ts20220122Midnight,
		},
		{
			name:        "run in the future blocks until cancelled",
			startTime:   must(time.Parse(time.RFC3339, "2022-01-22T13:24:01Z")),
			startCursor: ts20220122Midnight,

			when: Every(24 * time.Hour),

			expRun:    noRun,
			expErr:    context.DeadlineExceeded,
			expCursor: ts20220122Midnight,
		},
		{
			name:        "poll in the future blocks until cancelled",
			startTime:   must(time.Parse(time.RFC3339, "2022-01-22T13:24:01Z")),
			startCursor: ts20220122Midnight,

			when: Poll(24 * time.Hour),

			expRun:    noRun,
			expErr:    context.DeadlineExceeded,
			expCursor: ts20220122Midnight,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			t.Cleanup(cancel)

			cc := make(memCursor)
			cl := clocktesting.NewFakeClock(tc.startTime)

			err := cc.Set(ctx, cursorName, tc.startCursor)
			jtest.RequireNil(t, err)

			runs := ExpectRun(t, tc.expRun)
			defer runs.AssertUsed()

			if !tc.setClockTo.IsZero() {
				go func() {
					for !cl.HasWaiters() {
						time.Sleep(time.Millisecond)
					}
					cl.SetTime(tc.setClockTo)
				}()
			}

			r := scheduleRunner{
				cursor: cc,
				o:      options{name: cursorName, clock: cl},
				when:   tc.when,
				f:      runs.Run,
			}
			jtest.Require(t, tc.expErr, r.doNext(ctx))

			v, err := cc.Get(ctx, cursorName)
			jtest.RequireNil(t, err)
			assert.Equal(t, tc.expCursor, v)
		})
	}
}

func TestNextExecution(t *testing.T) {
	testCases := []struct {
		name string

		now  time.Time
		last time.Time
		spec cron.Schedule

		expNext time.Time
	}{
		{
			name:    "never ran, returns next one",
			now:     must(time.Parse(time.RFC3339, "2022-01-22T13:24:01Z")),
			spec:    Every(time.Hour),
			expNext: must(time.Parse(time.RFC3339, "2022-01-22T14:00:00Z")),
		},
		{
			name:    "missed previous one, returns previous",
			now:     must(time.Parse(time.RFC3339, "2022-01-22T13:24:01Z")),
			last:    must(time.Parse(time.RFC3339, "2022-01-22T12:00:00Z")),
			spec:    Every(time.Hour),
			expNext: must(time.Parse(time.RFC3339, "2022-01-22T13:00:00Z")),
		},
		{
			name:    "last equal to previous returns next",
			now:     must(time.Parse(time.RFC3339, "2022-01-22T13:24:01Z")),
			last:    must(time.Parse(time.RFC3339, "2022-01-22T13:00:00Z")),
			spec:    Every(time.Hour),
			expNext: must(time.Parse(time.RFC3339, "2022-01-22T14:00:00Z")),
		},
		{
			name:    "last in the future still returns next",
			now:     must(time.Parse(time.RFC3339, "2022-01-22T13:24:01Z")),
			last:    must(time.Parse(time.RFC3339, "2022-01-22T13:44:00Z")),
			spec:    Every(time.Hour),
			expNext: must(time.Parse(time.RFC3339, "2022-01-22T14:00:00Z")),
		},
		{
			name:    "offset handled",
			now:     must(time.Parse(time.RFC3339, "2022-01-22T15:04:53Z")),
			last:    must(time.Parse(time.RFC3339, "2022-01-22T14:10:00Z")),
			spec:    Every(time.Hour, WithOffset(10*time.Minute)),
			expNext: must(time.Parse(time.RFC3339, "2022-01-22T15:10:00Z")),
		},
		{
			name:    "mixed timezones handles next run",
			now:     must(time.Parse(time.RFC3339, "2022-01-22T15:04:53+07:00")),
			last:    must(time.Parse(time.RFC3339, "2022-01-22T07:10:00Z")),
			spec:    Every(time.Hour, WithOffset(10*time.Minute)),
			expNext: must(time.Parse(time.RFC3339, "2022-01-22T15:10:00+07:00")),
		},
		{
			name:    "mixed timezones handles previous run in now timezone",
			now:     must(time.Parse(time.RFC3339, "2022-01-22T15:04:53+07:00")),
			last:    must(time.Parse(time.RFC3339, "2022-01-22T06:10:00Z")),
			spec:    Every(time.Hour, WithOffset(10*time.Minute)),
			expNext: must(time.Parse(time.RFC3339, "2022-01-22T14:10:00+07:00")),
		},
		{
			name:    "handle cron schedules",
			now:     must(time.Parse(time.RFC3339, "2022-01-21T15:04:53Z")),
			last:    must(time.Parse(time.RFC3339, "2022-01-21T14:00:00Z")),
			spec:    must(cron.ParseStandard("0 7,10,14 * * 1-5")),
			expNext: must(time.Parse(time.RFC3339, "2022-01-24T07:00:00Z")), // 21st was a Friday, so should skip to Monday 24th
		},
		{
			name:    "tod handles current run",
			now:     must(time.Parse(time.RFC3339, "2022-01-21T15:00:00Z")),
			last:    must(time.Parse(time.RFC3339, "2022-01-21T15:00:00Z")),
			spec:    TimeOfDay(15, 0),
			expNext: must(time.Parse(time.RFC3339, "2022-01-22T15:00:00Z")),
		},
		{
			name:    "fixed interval with cursor far in the past",
			now:     must(time.Parse(time.RFC3339, "2022-01-21T12:15:00Z")),
			last:    must(time.Parse(time.RFC3339, "2022-01-21T10:00:00Z")),
			spec:    FixedInterval(time.Hour),
			expNext: must(time.Parse(time.RFC3339, "2022-01-21T12:00:00Z")),
		},
		{
			name:    "fixed interval with cursor in the past but now the same as expected run time",
			now:     must(time.Parse(time.RFC3339, "2022-01-21T12:00:00Z")),
			last:    must(time.Parse(time.RFC3339, "2022-01-21T10:00:00Z")),
			spec:    FixedInterval(time.Hour),
			expNext: must(time.Parse(time.RFC3339, "2022-01-21T12:00:00Z")),
		},
		{
			name:    "fixed interval with cursor updated",
			now:     must(time.Parse(time.RFC3339, "2022-01-21T12:00:00Z")),
			last:    must(time.Parse(time.RFC3339, "2022-01-21T12:00:00Z")),
			spec:    FixedInterval(time.Hour),
			expNext: must(time.Parse(time.RFC3339, "2022-01-21T13:00:00Z")),
		},
		{
			name:    "fixed interval with offset",
			now:     must(time.Parse(time.RFC3339, "2022-01-21T12:20:00Z")),
			last:    must(time.Parse(time.RFC3339, "2022-01-21T08:00:00Z")),
			spec:    FixedInterval(time.Hour, WithOffset(time.Minute)),
			expNext: must(time.Parse(time.RFC3339, "2022-01-21T12:01:00Z")),
		},
		{
			name:    "fixed interval with historic cursor and offset run time and now value",
			now:     must(time.Parse(time.RFC3339, "2022-01-21T12:15:00Z")),
			last:    must(time.Parse(time.RFC3339, "2022-01-21T08:00:00Z")),
			spec:    FixedInterval(time.Hour, WithOffset(20*time.Minute)),
			expNext: must(time.Parse(time.RFC3339, "2022-01-21T11:20:00Z")),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			next := nextExecution(tc.now, tc.last, tc.spec, "")
			assert.Equal(t, tc.expNext, next)
		})
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func TestNextExecutionMany(t *testing.T) {
	timezoneAmericaNewYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		require.Fail(t, "could not load location")
	}
	testCases := []struct {
		name     string
		schedule cron.Schedule
		start    time.Time
		end      time.Time
		expRuns  []time.Time
	}{
		{
			name:     "timezones",
			schedule: ToTimezone(TimeOfDay(0, 30), timezoneAmericaNewYork),
			start:    time.Date(2022, 3, 10, 0, 0, 0, 0, time.UTC),
			end:      time.Date(2022, 3, 15, 0, 0, 0, 0, time.UTC),
			expRuns: []time.Time{
				time.Date(2022, 3, 10, 5, 30, 0, 0, time.UTC),
				time.Date(2022, 3, 11, 5, 30, 0, 0, time.UTC),
				time.Date(2022, 3, 12, 5, 30, 0, 0, time.UTC),
				time.Date(2022, 3, 13, 5, 30, 0, 0, time.UTC),
				time.Date(2022, 3, 14, 4, 30, 0, 0, time.UTC),
			},
		},
		{
			name:     "timezones over DST switchover boundary (2AM 13 March, 2022)",
			schedule: ToTimezone(TimeOfDay(2, 30), timezoneAmericaNewYork),
			start:    time.Date(2022, 3, 10, 0, 0, 0, 0, time.UTC),
			end:      time.Date(2022, 3, 15, 0, 0, 0, 0, time.UTC),
			expRuns: []time.Time{
				time.Date(2022, 3, 10, 7, 30, 0, 0, time.UTC),
				time.Date(2022, 3, 11, 7, 30, 0, 0, time.UTC),
				time.Date(2022, 3, 12, 7, 30, 0, 0, time.UTC),
				time.Date(2022, 3, 13, 6, 30, 0, 0, time.UTC),
				time.Date(2022, 3, 14, 6, 30, 0, 0, time.UTC),
			},
		},
		{
			name: "timezones with cron",
			schedule: ToTimezone(
				must(cron.ParseStandard("0 12,14 * * 1-5")),
				timezoneAmericaNewYork,
			),
			start: time.Date(2022, 3, 10, 0, 0, 0, 0, time.UTC),
			end:   time.Date(2022, 3, 15, 0, 0, 0, 0, time.UTC),
			expRuns: []time.Time{
				time.Date(2022, 3, 10, 17, 0, 0, 0, time.UTC),
				time.Date(2022, 3, 10, 19, 0, 0, 0, time.UTC),
				time.Date(2022, 3, 11, 17, 0, 0, 0, time.UTC),
				time.Date(2022, 3, 11, 19, 0, 0, 0, time.UTC),
				time.Date(2022, 3, 14, 16, 0, 0, 0, time.UTC),
				time.Date(2022, 3, 14, 18, 0, 0, 0, time.UTC),
			},
		},
		{
			name: "timezones with cron over DST switchover boundary (2AM 13 March, 2022)",
			schedule: ToTimezone(
				must(cron.ParseStandard("30 2 * * *")),
				timezoneAmericaNewYork,
			),
			start: time.Date(2022, 3, 10, 0, 0, 0, 0, time.UTC),
			end:   time.Date(2022, 3, 15, 0, 0, 0, 0, time.UTC),
			expRuns: []time.Time{
				time.Date(2022, 3, 10, 7, 30, 0, 0, time.UTC),
				time.Date(2022, 3, 11, 7, 30, 0, 0, time.UTC),
				time.Date(2022, 3, 12, 7, 30, 0, 0, time.UTC),
				// 2AM becomes 3AM on 13th at 2AM, so 2AM never happens on this day and the next run only happens the following day
				time.Date(2022, 3, 14, 6, 30, 0, 0, time.UTC),
			},
		},
		{
			name: "timezones with cron running every afternoon over switchover into DST",
			schedule: ToTimezone(
				must(cron.ParseStandard("0 15 * * *")),
				timezoneAmericaNewYork,
			),
			start: time.Date(2022, 3, 10, 0, 0, 0, 0, time.UTC),
			end:   time.Date(2022, 3, 15, 0, 0, 0, 0, time.UTC),
			expRuns: []time.Time{
				time.Date(2022, 3, 10, 20, 0, 0, 0, time.UTC),
				time.Date(2022, 3, 11, 20, 0, 0, 0, time.UTC),
				time.Date(2022, 3, 12, 20, 0, 0, 0, time.UTC),
				time.Date(2022, 3, 13, 19, 0, 0, 0, time.UTC),
				time.Date(2022, 3, 14, 19, 0, 0, 0, time.UTC),
			},
		},
		{
			name: "timezones with short interval cron running every 15mins over switchover into DST",
			schedule: ToTimezone(
				must(cron.ParseStandard("45 * * * *")),
				timezoneAmericaNewYork,
			),
			start: time.Date(2022, 3, 13, 0, 0, 0, 0, time.UTC),
			end:   time.Date(2022, 3, 14, 0, 0, 0, 0, time.UTC),
			expRuns: []time.Time{
				time.Date(2022, time.March, 13, 0, 45, 0, 0, time.UTC),
				time.Date(2022, time.March, 13, 1, 45, 0, 0, time.UTC),
				time.Date(2022, time.March, 13, 2, 45, 0, 0, time.UTC),
				time.Date(2022, time.March, 13, 3, 45, 0, 0, time.UTC),
				time.Date(2022, time.March, 13, 4, 45, 0, 0, time.UTC),
				time.Date(2022, time.March, 13, 5, 45, 0, 0, time.UTC),
				time.Date(2022, time.March, 13, 6, 45, 0, 0, time.UTC),
				time.Date(2022, time.March, 13, 7, 45, 0, 0, time.UTC),
				time.Date(2022, time.March, 13, 8, 45, 0, 0, time.UTC),
				time.Date(2022, time.March, 13, 9, 45, 0, 0, time.UTC),
				time.Date(2022, time.March, 13, 10, 45, 0, 0, time.UTC),
				time.Date(2022, time.March, 13, 11, 45, 0, 0, time.UTC),
				time.Date(2022, time.March, 13, 12, 45, 0, 0, time.UTC),
				time.Date(2022, time.March, 13, 13, 45, 0, 0, time.UTC),
				time.Date(2022, time.March, 13, 14, 45, 0, 0, time.UTC),
				time.Date(2022, time.March, 13, 15, 45, 0, 0, time.UTC),
				time.Date(2022, time.March, 13, 16, 45, 0, 0, time.UTC),
				time.Date(2022, time.March, 13, 17, 45, 0, 0, time.UTC),
				time.Date(2022, time.March, 13, 18, 45, 0, 0, time.UTC),
				time.Date(2022, time.March, 13, 19, 45, 0, 0, time.UTC),
				time.Date(2022, time.March, 13, 20, 45, 0, 0, time.UTC),
				time.Date(2022, time.March, 13, 21, 45, 0, 0, time.UTC),
				time.Date(2022, time.March, 13, 22, 45, 0, 0, time.UTC),
				time.Date(2022, time.March, 13, 23, 45, 0, 0, time.UTC),
			},
		},
		{
			name:     "new years day in foreign timezone, produces expected UTC execution time",
			schedule: ToTimezone(TimeOfDay(0, 0), timezoneAmericaNewYork),
			start:    time.Date(2021, 12, 31, 0, 0, 0, 0, time.UTC),
			end:      time.Date(2022, 1, 2, 0, 0, 0, 0, time.UTC),
			expRuns: []time.Time{
				time.Date(2021, 12, 31, 5, 0, 0, 0, time.UTC),
				time.Date(2022, 1, 1, 5, 0, 0, 0, time.UTC),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ti := tc.start
			var runs []time.Time
			for {
				ti = tc.schedule.Next(ti)
				if !ti.Before(tc.end) {
					break
				}
				runs = append(runs, ti)
			}
			assert.Equal(t, tc.expRuns, runs)
		})
	}
}

func TestRetries(t *testing.T) {
	errRun := errors.New("run error")

	testCases := []struct {
		name      string
		maxErrors uint
		errCount  uint

		expWait   bool
		expErr    error
		expCursor string
	}{
		{
			name:      "error on initial run",
			maxErrors: 0,
			errCount:  0,
			expWait:   true,
			expErr:    errRun,
		},
		{
			name:      "error is retried",
			maxErrors: 0,
			errCount:  1,
			expWait:   true,
			expErr:    errRun,
		},
		{
			name:      "error is not retried if max errors is set",
			maxErrors: 1,
			errCount:  1,
			expCursor: "10020",
		},
		{
			name:      "error on retry",
			maxErrors: 5,
			errCount:  4,
			expWait:   true,
			expErr:    errRun,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			clock := clocktesting.NewFakeClock(time.Unix(10_000, 0))
			cursor := make(memCursor)
			o := options{
				name:       "test_retry",
				errorSleep: ErrorSleepFor(0),
				maxErrors:  tc.maxErrors,
				clock:      clock,
			}

			r := scheduleRunner{
				cursor: cursor,
				o:      o,
				when:   Every(time.Minute),
				f: func(_ context.Context, _, _ time.Time, _ string) error {
					return errRun
				},
				ErrCount: tc.errCount,
			}

			if tc.expWait {
				go step(clock, time.Minute)
			}

			jtest.Assert(t, tc.expErr, r.doNext(context.Background()))

			v, err := cursor.Get(context.Background(), o.name)
			jtest.RequireNil(t, err)
			assert.Equal(t, tc.expCursor, v)
		})
	}
}

func step(clock *clocktesting.FakeClock, d time.Duration) {
	for !clock.HasWaiters() {
		time.Sleep(time.Millisecond)
	}
	clock.Step(d)
}

type testContext struct {
	errCalled int
	err       []error
	t         *testing.T
}

func (*testContext) Deadline() (deadline time.Time, ok bool) {
	return
}

func (*testContext) Done() <-chan struct{} {
	return nil
}

func (tc *testContext) Err() error {
	l := len(tc.err)
	if l <= tc.errCalled {
		require.Fail(tc.t, "Insufficient context errors")
		return nil
	}
	err := tc.err[tc.errCalled]
	tc.errCalled++
	return err
}

func (*testContext) Value(_ any) any {
	return nil
}

func (*testContext) String() string {
	return "test error Context"
}

func Test_processLoop(t *testing.T) {
	process := func(context.Context) time.Duration { return time.Minute }

	tests := []struct {
		name       string
		ctx        context.Context
		wait       waitFunc
		waitCalled int
		err        error
	}{
		{
			name: "bad context",
			ctx:  &testContext{err: []error{errors.New("ctx.Err()1!", j.C("err_1")), errors.New("ctx.Err()2!", j.C("err_2"))}},
			err:  errors.New("ctx.Err()2!", j.C("err_2")),
		},
		{
			name: "wait function errors",
			ctx:  &testContext{err: []error{nil}},
			wait: func(_ context.Context, _ time.Duration) error {
				return errors.New("Wait Error!", j.C("err_1"))
			},
			waitCalled: 1,
			err:        errors.New("Wait Error!", j.C("err_1")),
		},
		{
			name:       "Loop is cancelled after one process",
			ctx:        &testContext{err: []error{nil, context.Canceled, errors.New("Context Was Cancelled", j.C("err_1"))}},
			waitCalled: 1,
			err:        errors.New("Context Was Cancelled", j.C("err_1")),
		},
		{
			name:       "Loop is cancelled after two process runs",
			ctx:        &testContext{err: []error{nil, nil, context.Canceled, errors.New("Context Was Cancelled", j.C("err_1"))}},
			waitCalled: 2,
			err:        errors.New("Context Was Cancelled", j.C("err_1")),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			waitCalled := 0
			if tt.wait == nil {
				tt.wait = func(_ context.Context, _ time.Duration) error {
					return nil
				}
			}
			wait := func(ctx context.Context, s time.Duration) error {
				waitCalled++
				return tt.wait(ctx, s)
			}
			err := processLoop(tt.ctx, process, wait)
			require.Equal(t, tt.waitCalled, waitCalled)
			if tt.err == nil {
				jtest.RequireNil(t, err)
			} else {
				jtest.Require(t, tt.err, err)
			}
		})
	}
}

func Test_processOnce(t *testing.T) {
	stdSleep := time.Minute * 10
	errSleep := time.Minute * 5
	tests := []struct {
		name      string
		awaitRole AwaitRoleFunc
		f         ScheduledFunc
		errCount  uint
		sleep     time.Duration
	}{
		{
			name: "awaitRole returns context.Cancelled error",
			awaitRole: func(_ string) ContextFunc {
				return func(ctx context.Context) (context.Context, context.CancelFunc, error) {
					return ctx, nil, context.Canceled
				}
			},
			sleep: stdSleep,
		},
		{
			name: "awaitRole returns non context.Cancelled error",
			awaitRole: func(_ string) ContextFunc {
				return func(ctx context.Context) (context.Context, context.CancelFunc, error) {
					return ctx, nil, errors.New("Bang1!")
				}
			},
			sleep:    errSleep,
			errCount: 1,
		},
		{
			name: "runner.doNext returns context.Cancelled error",
			f: func(_ context.Context, _, _ time.Time, _ string) error {
				return context.Canceled
			},
			sleep: stdSleep,
		},
		{
			name: "runner.doNext returns non context.Cancelled error",
			f: func(_ context.Context, _, _ time.Time, _ string) error {
				return errors.New("Bang1!")
			},
			sleep:    errSleep,
			errCount: 1,
		},
		{
			name:  "no errors returned",
			sleep: stdSleep,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.awaitRole == nil {
				tt.awaitRole = func(role string) ContextFunc {
					return func(ctx context.Context) (context.Context, context.CancelFunc, error) {
						return ctx, func() {}, nil
					}
				}
			}
			if tt.f == nil {
				tt.f = func(_ context.Context, _, _ time.Time, _ string) error {
					return nil
				}
			}
			r := scheduleRunner{
				cursor: make(memCursor),
				o:      options{name: "test_processFunc", clock: clocktesting.NewFakeClock(time.Unix(10_000, 0))},
				when:   Poll(0),
				f:      tt.f,
			}
			var errCount uint
			opts := options{
				sleep: SleepFor(stdSleep),
				errorSleep: func(ec uint, err error) time.Duration {
					errCount = ec
					return errSleep
				},
			}
			opts = resolveOptions(opts, nil)

			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			t.Cleanup(cancel)
			sleep := processOnce(ctx, tt.awaitRole, opts, &r)
			require.Equal(t, tt.sleep, sleep)
			require.Equal(t, tt.errCount, errCount)
			_ = processOnce(ctx, tt.awaitRole, opts, &r)
			// If there was no error, we still expect errCount=0.
			// If there was an error, we expect another so errCount=2.
			require.Equal(t, tt.errCount*2, errCount)
		})
	}
}

func TestLastScheduled(t *testing.T) {
	tests := []struct {
		name   string
		f      ScheduledFunc
		panics bool
	}{
		{
			name: "No panic: f LastScheduledFunc not nil",
			f:    func(_ context.Context, _, _ time.Time, _ string) error { return nil },
		},
		{
			name:   "Panic: f LastScheduledFunc is nil",
			panics: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			t.Cleanup(cancel)
			awaitRole := func(role string) ContextFunc {
				return func(ctx context.Context) (context.Context, context.CancelFunc, error) {
					return ctx, func() {}, nil
				}
			}
			process := Scheduled(awaitRole, make(memCursor), "TestLastScheduled", Poll(1), tt.f)
			tf := func() { _ = process.Run(ctx) }
			if tt.panics {
				require.Panics(t, tf)
			} else {
				require.NotPanics(t, tf)
			}
		})
	}
}

func TestCronWithPrevious(t *testing.T) {
	testCases := []struct {
		name        string
		cron        string
		now         time.Time
		expPrevious time.Time
		expNext     time.Time
	}{
		{
			name:        "cron that never runs, gives up",
			cron:        "0 0 31 2 *",
			now:         time.Date(2024, 1, 1, 0, 0, 59, 0, time.UTC),
			expPrevious: time.Date(2024, 1, 1, 0, 0, 59, 0, time.UTC),
			expNext:     time.Time{},
		},
		{
			name:        "look back over a year",
			cron:        "1 1 1 1 *",
			now:         time.Date(2024, 1, 1, 0, 0, 59, 0, time.UTC),
			expPrevious: time.Date(2023, 1, 1, 1, 1, 0, 0, time.UTC),
			expNext:     time.Date(2024, 1, 1, 1, 1, 0, 0, time.UTC),
		},
		{
			name:        "daily at 9am",
			cron:        "0 9 * * *",
			now:         time.Date(2024, 10, 3, 8, 0, 0, 0, time.UTC),
			expPrevious: time.Date(2024, 10, 2, 9, 0, 0, 0, time.UTC),
			expNext:     time.Date(2024, 10, 3, 9, 0, 0, 0, time.UTC),
		},
		{
			name:        "every minute of every day",
			cron:        "* * * * *",
			now:         time.Date(2024, 10, 3, 8, 14, 45, 0, time.UTC),
			expPrevious: time.Date(2024, 10, 3, 8, 14, 0, 0, time.UTC),
			expNext:     time.Date(2024, 10, 3, 8, 15, 0, 0, time.UTC),
		},
		{
			name:        "every minute of every day, now is on schedule",
			cron:        "* * * * *",
			now:         time.Date(2024, 10, 3, 8, 14, 0, 0, time.UTC),
			expPrevious: time.Date(2024, 10, 3, 8, 14, 0, 0, time.UTC),
			expNext:     time.Date(2024, 10, 3, 8, 15, 0, 0, time.UTC),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cr, err := cron.ParseStandard(tc.cron)
			jtest.RequireNil(t, err)
			s := cronWithPrevious{Schedule: cr}

			prev := s.Previous(tc.now)
			assert.Equal(t, tc.expPrevious, prev)

			next := s.Next(tc.now)
			assert.Equal(t, tc.expNext, next)
		})
	}
}
