package lu

import (
	"context"
	"runtime/pprof"

	"github.com/luno/jettison/errors"
	"github.com/luno/jettison/j"
	"github.com/luno/jettison/log"
)

var errProcessPanicked = errors.New("process panicked during run", j.C("ERR_1d22d6c1ff24b023"))

type monitor struct {
	ctx context.Context
	app *App
	i   int
}

func processMonitor(ctx context.Context, app *App, i int) *monitor {
	return &monitor{
		ctx: ctx,
		app: app,
		i:   i,
	}
}

func (m *monitor) launch() error {
	ctx := m.ctx
	app := m.app
	var err error
	for {
		err = nil
		func() {
			defer cleanPanic()(&err)

			name := m.getProcess().Name
			ctx = labelContext(name, ctx)

			defer close(m.setProcessRunning())
			app.OnEvent(ctx, Event{Type: ProcessStart, Name: name})
			defer app.OnEvent(ctx, Event{Type: ProcessEnd, Name: name})
			err = m.getProcess().Run(ctx)
		}()
		if m.shouldExit(err) {
			break
		}
	}
	if err != nil {
		// NoReturnErr: Record why a process exited abnormally
		log.Error(ctx, err, j.KV("process_name", m.app.processes[m.i].Name))
	}
	return ctx.Err()
}

func (m *monitor) shouldExit(err error) bool {
	select {
	case <-m.ctx.Done():
		return true
	default:
		if errors.IsAny(err, nil, errProcessPanicked, ErrBreakContextLoop) {
			return true
		}
	}
	return false
}

func labelContext(processName string, ctx context.Context) context.Context {
	if processName != "" {
		ctx = log.ContextWith(ctx, j.KV("process", processName))
		ctx = pprof.WithLabels(ctx, pprof.Labels("lu_process", processName))
	}
	pprof.SetGoroutineLabels(ctx)
	return ctx
}

func (m *monitor) getProcess() *Process {
	return &m.app.processes[m.i]
}

func (m *monitor) setProcessRunning() chan struct{} {
	doneCh := make(chan struct{})
	m.app.processRunning[m.i] = doneCh
	return doneCh
}

func cleanPanic() func(err *error) {
	return func(err *error) {
		if recv := recover(); recv != nil {
			*err = errors.Wrap(errProcessPanicked, "", j.KV("panic_value", recv))
		}
	}
}
