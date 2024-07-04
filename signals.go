package lu

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/luno/jettison/j"
	"github.com/luno/jettison/log"
)

// AppContext manages two contexts for running an app. It responds to different signals by
// cancelling one or both of these contexts. This behaviour allows us to do graceful shutdown
// in kubernetes using a stop script. If the application terminates before the stop script finishes
// then we get an error event from Kubernetes, so we need to be able to shut the application down
// using one signal, then exit the stop script and let Kubernetes send another signal to do the final
// termination. See this for more details on the hook behaviour
// https://kubernetes.io/docs/concepts/containers/container-lifecycle-hooks/
//
// For SIGINT and SIGTERM, we will cancel both contexts, the application should
// finish all processes and call os.Exit
//
// For SIGQUIT, we cancel just the AppContext, the application should shut down all
// processes and wait for termination.
type AppContext struct {
	signals chan os.Signal

	// AppContext should be used for running the application.
	// When it's cancelled, the application should stop running all processes.
	AppContext context.Context
	appCancel  context.CancelFunc

	// TerminationContext should be used for the execution of application.
	// When it's cancelled the application binary should terminate.
	// AppContext will be cancelled with this context as well.
	TerminationContext context.Context
	termCancel         context.CancelFunc
}

func NewAppContext(ctx context.Context) AppContext {
	c := AppContext{
		signals: make(chan os.Signal, 1),
	}

	c.TerminationContext, c.termCancel = context.WithCancel(ctx)
	c.AppContext, c.appCancel = context.WithCancel(c.TerminationContext)

	signal.Notify(c.signals, syscall.SIGQUIT, syscall.SIGINT, syscall.SIGTERM)

	go c.monitor(ctx)

	return c
}

func (c AppContext) Stop() {
	signal.Stop(c.signals)
	close(c.signals)
}

func (c AppContext) monitor(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case s, ok := <-c.signals:
			if !ok {
				return
			}
			call, ok := s.(syscall.Signal)
			if !ok {
				log.Info(ctx, "received unknown OS signal", j.KV("signal", s))
				continue
			}
			log.Info(ctx, "received OS signal", j.KV("signal", call))
			switch call {
			case syscall.SIGQUIT:
				c.appCancel()
			case syscall.SIGINT, syscall.SIGTERM:
				c.termCancel()
			}
		}
	}
}
