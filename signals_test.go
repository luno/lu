package lu

import (
	"context"
	"syscall"
	"testing"
	"time"

	"github.com/luno/jettison/errors"
	"github.com/luno/jettison/jtest"
	"github.com/stretchr/testify/assert"
)

func TestAppContext_QuitOnlyEndsTheAppContext(t *testing.T) {
	ac := NewAppContext(context.Background())
	t.Cleanup(ac.Stop)

	ac.signals <- syscall.SIGQUIT

	assert.Eventually(t, func() bool {
		return errors.Is(ac.AppContext.Err(), context.Canceled)
	}, time.Second, time.Millisecond)

	assert.Never(t, func() bool {
		return errors.Is(ac.TerminationContext.Err(), context.Canceled)
	}, time.Second, time.Millisecond)
}

func TestAppContext_IntEndsBothContexts(t *testing.T) {
	ac := NewAppContext(context.Background())
	t.Cleanup(ac.Stop)

	ac.signals <- syscall.SIGINT

	assert.Eventually(t, func() bool {
		return errors.Is(ac.AppContext.Err(), context.Canceled)
	}, time.Second, time.Millisecond)

	assert.Eventually(t, func() bool {
		return errors.Is(ac.TerminationContext.Err(), context.Canceled)
	}, time.Second, time.Millisecond)
}

func TestAppContext_QuitThenTerminate(t *testing.T) {
	// This is the sequence of signals we will receive in kubernetes (when using the stop script)
	ac := NewAppContext(context.Background())
	t.Cleanup(ac.Stop)

	ac.signals <- syscall.SIGQUIT

	assert.Eventually(t, func() bool {
		return errors.Is(ac.AppContext.Err(), context.Canceled)
	}, time.Second, time.Millisecond)

	jtest.AssertNil(t, ac.TerminationContext.Err())

	ac.signals <- syscall.SIGTERM

	assert.Eventually(t, func() bool {
		return errors.Is(ac.TerminationContext.Err(), context.Canceled)
	}, time.Second, time.Millisecond)
}

func TestAppContext_TerminateEndsEverything(t *testing.T) {
	ac := NewAppContext(context.Background())
	t.Cleanup(ac.Stop)

	ac.signals <- syscall.SIGTERM

	assert.Eventually(t, func() bool {
		return errors.Is(ac.AppContext.Err(), context.Canceled)
	}, time.Second, time.Millisecond)

	assert.Eventually(t, func() bool {
		return errors.Is(ac.TerminationContext.Err(), context.Canceled)
	}, time.Second, time.Millisecond)
}

func TestAppContext_CancelledContext(t *testing.T) {
	ac := NewAppContext(context.Background())
	t.Cleanup(ac.Stop)

	ac.appCancel()

	assert.Eventually(t, func() bool {
		return errors.Is(ac.AppContext.Err(), context.Canceled)
	}, time.Second, time.Millisecond)
}
