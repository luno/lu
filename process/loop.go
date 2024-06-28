package process

import (
	"context"
	"fmt"
	"time"

	"github.com/go-stack/stack"
	"github.com/luno/jettison/errors"
	"github.com/luno/jettison/j"
	"github.com/luno/jettison/log"
	"github.com/luno/jettison/trace"

	"github.com/luno/lu"
)

// ErrBreakContextLoop acts as a translation error between the reflex domain and the lu process one. It will be
// returned as an alternative when (correctly configured) a reflex stream returns a reflex.ErrSteamToHead error.
var ErrBreakContextLoop = errors.New("the context loop has been stopped", j.C("ERR_f3833d51676ea908"))

func defaultLoopOptions() options {
	o := options{
		errorSleep: ErrorSleepFor(10 * time.Second),
		// EXPERIMENTAL: Added for the purposes of production testing isolated cases with the new breakable behaviour
		isBreakableLoop: false,
	}
	stk := trace.GetStackTrace(1, trace.StackConfig{
		RemoveLambdas:  true,
		PackagesHidden: []string{trace.PackagePath(lu.Process{})},
		TrimRuntime:    true,
		FormatStack: func(call stack.Call) string {
			return fmt.Sprintf("%n", call)
		},
	})
	if len(stk) > 0 {
		o.name = stk[0]
	}
	return o
}

func noOpContextFunc(ctx context.Context) (context.Context, context.CancelFunc, error) {
	return ctx, func() {}, nil
}

// Loop is a Process that will repeatedly call f, logging errors until the process is cancelled.
func Loop(f lu.ProcessFunc, lo ...Option) lu.Process {
	return ContextLoop(noOpContextFunc, f, lo...)
}

// Retry runs the process function until it returns no error once.
func Retry(f lu.ProcessFunc, lo ...Option) lu.Process {
	return ContextRetry(noOpContextFunc, f, lo...)
}

// ContextLoop is a Process that will fetch a context and run f with that context.
// This can be used to block execution until a context is available.
func ContextLoop(getCtx ContextFunc, f lu.ProcessFunc, lo ...Option) lu.Process {
	opts := resolveOptions(defaultLoopOptions(), lo)
	return lu.Process{
		Name: opts.name,
		Run:  wrapContextLoop(getCtx, f, opts),
		Shutdown: func(ctx context.Context) error {
			return nil
		},
	}
}

func wrapContextLoop(getCtx ContextFunc, f lu.ProcessFunc, opts options) lu.ProcessFunc {
	return func(ctx context.Context) error {
		var errCount uint
		for ctx.Err() == nil {
			err := runWithContext(ctx, getCtx, func(ctx context.Context) error {
				err := f(ctx)
				sleep := opts.sleep()
				if opts.isBreakableLoop && errors.Is(err, ErrBreakContextLoop) {
					return err
				}
				if err != nil && !errors.IsAny(err, context.Canceled) {
					// NoReturnErr: Log critical errors and continue loop
					errCount += 1
					sleep = opts.errorSleep(errCount, err)
					opts.errCounter.Inc()
					log.Error(ctx, err)
					if opts.maxErrors > 0 && errCount >= opts.maxErrors {
						return err
					}
				} else {
					errCount = 0
				}
				if err = lu.Wait(ctx, opts.clock, sleep); err != nil {
					opts.afterLoop()
					return err
				}
				opts.afterLoop()
				return nil
			})
			if errors.Is(err, ErrBreakContextLoop) {
				log.Info(ctx, "context loop terminated", log.WithError(err))
				return nil
			}
			if err != nil && !errors.Is(err, context.Canceled) {
				// NOTE: Any error returned at this point will cause the entire App to terminate
				return err
			}
		}
		return ctx.Err()
	}
}

// ContextRetry runs the process function until it returns no error once.
func ContextRetry(
	getCtx ContextFunc,
	f lu.ProcessFunc,
	callOpts ...Option,
) lu.Process {
	opts := resolveOptions(defaultLoopOptions(), callOpts)

	var p lu.Process
	p.Name = opts.name
	p.Run = func(ctx context.Context) error {
		var errCount uint
		for ctx.Err() == nil {
			err := runWithContext(ctx, getCtx, func(ctx context.Context) error {
				err := f(ctx)
				if err == nil {
					return nil
				}

				errCount += 1
				// NoReturnErr: Log critical errors and continue loop
				if !errors.Is(err, context.Canceled) {
					opts.errCounter.Inc()
					log.Error(ctx, err)
				}
				sleep := opts.errorSleep(errCount, err)
				if wErr := lu.Wait(ctx, opts.clock, sleep); wErr != nil {
					return wErr
				}

				return err
			})
			if err == nil {
				break
			}
		}
		return ctx.Err()
	}
	return p
}

func runWithContext(ctx context.Context, getCtx ContextFunc, f lu.ProcessFunc) error {
	runCtx, cancel, err := getCtx(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	return f(runCtx)
}
