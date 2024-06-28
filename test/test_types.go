package test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/luno/lu"
)

// Only for testing purposes - do not import into main code builds

type EventLog chan lu.Event

func (l EventLog) Append(_ context.Context, e lu.Event) {
	l <- e
}

type EventConstraint interface {
	CheckMore(t *testing.T, e lu.Event) bool
}

type Event lu.Event

func (e Event) CheckMore(t *testing.T, got lu.Event) bool {
	assert.Equal(t, lu.Event(e), got)
	return false
}

type ConstraintFunc func(t *testing.T, e lu.Event) bool

func (f ConstraintFunc) CheckMore(t *testing.T, got lu.Event) bool {
	return f(t, got)
}
