// Package lu is an application framework for Luno microservices
package lu

import (
	"context"
	"runtime/pprof"
	"sync"
	"time"

	"github.com/luno/jettison/errors"
	"github.com/luno/jettison/j"
	"github.com/luno/jettison/log"
	"golang.org/x/sync/errgroup"
	"k8s.io/utils/clock"
)

var errProcessStillRunning = errors.New("process still running after shutdown", j.C("ERR_fa232f807b75bab6"))

// App will manage the lifecycle of the service. Emitting events for each stage of the application.
type App struct {
	// StartupTimeout is the deadline for running the start-up hooks and starting all the Processes
	// Defaults to 15 seconds.
	StartupTimeout time.Duration

	// ShutdownTimeout is the deadline for stopping all the app Processes and
	// running the shutdown hooks.
	// Defaults to 15 seconds.
	ShutdownTimeout time.Duration

	// OnEvent will be called for every lifecycle event in the app. See EventType for details.
	OnEvent OnEvent

	// UseProcessFile will write a file at /tmp/lu.pid whilst the app is still running.
	// The file will be removed after a graceful shutdown.
	UseProcessFile bool

	// OnShutdownErr is called after failing to shut down cleanly.
	// You can use this hook to change the error or do last minute reporting.
	// This hook is only called when using Run not when using Shutdown
	OnShutdownErr func(ctx context.Context, err error) error

	// RecoverAll determines if all running processes will be recovered by default rather than
	// on a one by one basis.
	RecoverAll bool

	startupHooks  []hook
	shutdownHooks []hook

	processes      []Process
	processRunning []chan struct{}
	ctx            context.Context
	eg             *errgroup.Group
	cancel         context.CancelFunc
}

func (a *App) setDefaults() {
	if a.StartupTimeout == 0 {
		a.StartupTimeout = 15 * time.Second
	}
	if a.ShutdownTimeout == 0 {
		a.ShutdownTimeout = 15 * time.Second
	}
	if a.OnEvent == nil {
		a.OnEvent = func(context.Context, Event) {}
	}
	a.RecoverAll = false
}

// OnStartUp will call f before the app starts working
func (a *App) OnStartUp(f ProcessFunc, opts ...HookOption) {
	h := hook{F: f, createOrder: len(a.startupHooks)}
	applyHookOptions(&h, opts)
	a.startupHooks = append(a.startupHooks, h)
	sortHooks(a.startupHooks)
}

// OnShutdown will call f just before the application terminates
// Use this to close database connections or release resources
func (a *App) OnShutdown(f ProcessFunc, opts ...HookOption) {
	h := hook{F: f, createOrder: len(a.shutdownHooks)}
	applyHookOptions(&h, opts)
	a.shutdownHooks = append(a.shutdownHooks, h)
	sortHooks(a.shutdownHooks)
}

// AddProcess adds a Process that is started in parallel after start up.
// If any Process finish with an error, then the application will be stopped.
func (a *App) AddProcess(processes ...Process) {
	a.processes = append(a.processes, processes...)
}

// GetProcesses returns all the configured processes for the App
func (a *App) GetProcesses() []Process {
	ret := make([]Process, len(a.processes))
	copy(ret, a.processes)
	return ret
}

func (a *App) startup(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, a.StartupTimeout)
	defer cancel()
	// Revert the labels after running all the hooks
	defer pprof.SetGoroutineLabels(ctx)

	for idx, h := range a.startupHooks {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		a.OnEvent(ctx, Event{Type: PreHookStart, Name: h.Name})
		hookCtx := ctx
		if h.Name != "" {
			hookCtx = log.ContextWith(hookCtx, j.MKV{"hook_idx": idx, "hook_name": h.Name})
			hookCtx = pprof.WithLabels(hookCtx, pprof.Labels("lu_hook", h.Name))
			pprof.SetGoroutineLabels(hookCtx)
		}

		if err := h.F(hookCtx); err != nil {
			return errors.Wrap(err, "start hook")
		}
		a.OnEvent(ctx, Event{Type: PostHookStart, Name: h.Name})
	}
	return ctx.Err()
}

func (a *App) cleanup(ctx context.Context) error {
	var errs []error
	for idx, h := range a.shutdownHooks {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		a.OnEvent(ctx, Event{Type: PreHookStop, Name: h.Name})
		hookCtx := log.ContextWith(ctx, j.MKV{"hook_idx": idx, "hook_name": h.Name})
		err := h.F(hookCtx)
		if err != nil {
			// NoReturnErr: Collect errors
			errs = append(errs, errors.Wrap(err, "stop hook", j.KV("hook_name", h.Name)))
		}
		a.OnEvent(ctx, Event{Type: PostHookStop, Name: h.Name})
	}
	// TODO(adam): Return all the errors
	if len(errs) > 0 {
		for i := 1; i < len(errs); i++ {
			log.Error(ctx, errs[i])
		}
		return errs[0]
	}
	return nil
}

// Run will start the App, running the startup Hooks, then the Processes.
// It will wait for any signals before shutting down first the Processes then the shutdown Hooks.
// This behaviour can be customised by using Launch, WaitForShutdown, and Shutdown.
func (a *App) Run() int {
	ac := NewAppContext(context.Background())
	defer ac.Stop()

	ctx := ac.AppContext

	if err := a.Launch(ctx); err != nil {
		// NoReturnErr: Log
		log.Error(ctx, errors.Wrap(err, "app launch"))
		return 1
	}
	<-a.WaitForShutdown()
	var exit int
	err := a.Shutdown()
	if err != nil {
		// NoReturnErr: Log
		err = handleShutdownErr(a, ac, err)
		log.Error(ctx, errors.Wrap(err, "app shutdown"))
		exit = 1
	}

	// TODO(adam): Move pid removal into Shutdown

	// This should be called in Shutdown so that clients which call that instead of
	// Run can get the right behaviour
	if a.UseProcessFile {
		removePIDFile(ctx)
	}

	// Wait for termination in case we've only been told to quit
	<-ac.TerminationContext.Done()

	log.Info(ctx, "App terminated", j.MKV{"exit_code": exit})

	return exit
}

// Launch will run all the startup hooks and launch all the processes.
// If any hook returns an error, we will return early, processes will not be started.
// ctx will be used for startup and also the main application context.
// If the hooks take longer than StartupTimeout then launch will return a deadline exceeded error.
func (a *App) Launch(ctx context.Context) error {
	a.setDefaults()

	if a.UseProcessFile {
		if err := createPIDFile(); err != nil {
			return err
		}
	}

	a.OnEvent(ctx, Event{Type: AppStartup})

	if err := a.startup(ctx); err != nil {
		return err
	}

	// Create the app context now
	appCtx, appCancel := context.WithCancel(ctx)
	eg, appCtx := errgroup.WithContext(appCtx)

	a.ctx = appCtx
	a.cancel = appCancel
	a.eg = eg

	a.processRunning = make([]chan struct{}, len(a.processes))
	for i := range a.processes {
		p := &a.processes[i]
		p.app = a

		doneCh := make(chan struct{})
		a.processRunning[i] = doneCh
		if p.Run == nil {
			close(doneCh)
			continue
		}

		ctx = labelContext(p.app.ctx, p.Name)
		a.OnEvent(ctx, Event{Type: ProcessStart, Name: p.Name})
		if a.RecoverAll || p.Recover {
			eg.Go(a.recover(ctx, p, doneCh))
		} else {
			eg.Go(a.dontRecover(ctx, p, doneCh))
		}

	}

	a.OnEvent(ctx, Event{Type: AppRunning})
	return ctx.Err()
}

func (a *App) launch(ctx context.Context, p *Process) func() error {
	return func() error {
		defer a.OnEvent(ctx, Event{Type: ProcessEnd, Name: p.Name})
		// NOTE: Any error returned by any of the processes will cause the entire App to terminate unless this
		// has been called from inside the recover function
		return p.Run(ctx)
	}
}

func (a *App) dontRecover(ctx context.Context, p *Process, doneCh chan struct{}) func() error {
	return func() error {
		defer close(doneCh)
		return a.launch(ctx, p)()
	}
}

func (a *App) recover(ctx context.Context, p *Process, doneCh chan struct{}) func() error {
	return func() error {
		defer close(doneCh)
		var err error
		for {
			err = nil
			func() {
				defer cleanPanic()(&err)
				err = a.launch(ctx, p)()
			}()
			if shouldExit(ctx, err) {
				break
			}
		}
		if err != nil {
			// NoReturnErr: Record why a process exited abnormally
			log.Error(ctx, err)
		}
		return ctx.Err()
	}
}

// WaitForShutdown returns a channel that waits for the application to be cancelled.
// Note the application has not finished terminating when this channel is closed.
// Shutdown should be called after waiting on the channel from this function.
func (a *App) WaitForShutdown() <-chan struct{} {
	return a.ctx.Done()
}

// Shutdown will synchronously stop all the resources running in the app.
func (a *App) Shutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), a.ShutdownTimeout)
	defer cancel()

	a.OnEvent(ctx, Event{Type: AppTerminating})
	defer a.OnEvent(ctx, Event{Type: AppTerminated})

	defer func() {
		err := a.cleanup(ctx)
		if err != nil {
			// NoReturnErr: Log
			log.Error(ctx, errors.Wrap(err, ""))
		}
	}()

	shutErrs := make(chan error)
	var shutCount int
	// Shutdown processes which need shutting down explicitly first
	for i := range a.processes {
		p := &a.processes[i]
		if p.Shutdown != nil {
			shutCount++
			go func() {
				if err := p.Shutdown(ctx); err != nil {
					// NoReturnErr: Send error to collector
					shutErrs <- errors.Wrap(err, "", j.KV("process", p.Name))
				}
				shutErrs <- nil
			}()
		}
	}

	var errs []error
	for i := 0; i < shutCount; i++ {
		shutErr, err := WaitFor(ctx, shutErrs)
		if err != nil {
			return err
		}
		if shutErr != nil {
			// NoReturnErr: Collect for later
			errs = append(errs, shutErr)
		}
	}

	// Cancel the context for all the other processes
	a.cancel()

	groupErr, err := WaitFor(ctx, ErrGroupWait(a.eg))
	if err != nil {
		return err
	}
	if groupErr != nil && !errors.Is(groupErr, context.Canceled) {
		// NoReturnErr: Store them up
		errs = append(errs, groupErr)
	}

	if len(errs) > 0 {
		for i := 1; i < len(errs); i++ {
			log.Error(ctx, errs[i])
		}
		return errs[0]
	}

	return nil
}

func (a *App) RunningProcesses() []string {
	var ret []string
	for idx, p := range a.processes {
		select {
		case <-a.processRunning[idx]:
		default:
			ret = append(ret, p.Name)
		}
	}
	return ret
}

// Wait is a cancellable wait, it will return either when
// d has passed or ctx is cancelled.
// It will return an error if cancelled early.
func Wait(ctx context.Context, cl clock.Clock, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	ti := cl.NewTimer(d)
	defer ti.Stop()
	_, err := WaitFor(ctx, ti.C())
	return err
}

func WaitUntil(ctx context.Context, cl clock.Clock, t time.Time) error {
	return Wait(ctx, cl, t.Sub(cl.Now()))
}

func ErrGroupWait(eg *errgroup.Group) <-chan error {
	ch := make(chan error)
	go func() {
		ch <- eg.Wait()
	}()
	return ch
}

func WaitFor[T any](ctx context.Context, ch <-chan T) (T, error) {
	select {
	case v := <-ch:
		return v, nil
	case <-ctx.Done():
		var v T
		return v, ctx.Err()
	}
}

// SyncGroupWait wait for the wait group (websocket connections) to finalize
func SyncGroupWait(wg *sync.WaitGroup) <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		defer close(ch)
		wg.Wait()
	}()
	return ch
}

func handleShutdownErr(a *App, ac AppContext, err error) error {
	if !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	running := a.RunningProcesses()
	if len(running) == 0 {
		return err
	}
	errs := make([]error, 0, len(running))
	for _, p := range running {
		err := errors.Wrap(errProcessStillRunning, "", j.KV("process", p))
		errs = append(errs, err)
	}
	err = errors.Join(errs...)
	if ac.TerminationContext.Err() != nil {
		return err
	}
	if a.OnShutdownErr != nil {
		return a.OnShutdownErr(ac.TerminationContext, err)
	}
	return err
}

func labelContext(ctx context.Context, processName string) context.Context {
	if processName != "" {
		ctx = log.ContextWith(ctx, j.KV("process", processName))
		ctx = pprof.WithLabels(ctx, pprof.Labels("lu_process", processName))
	}
	pprof.SetGoroutineLabels(ctx)
	return ctx
}
