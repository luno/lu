package test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/luno/lu"
)

// Only for testing purposes - do not import into main code builds

func AnyOrder(events ...Event) EventConstraint {
	left := make(map[lu.Event]int)
	for _, ev := range events {
		left[lu.Event(ev)]++
	}
	return ConstraintFunc(func(t *testing.T, e lu.Event) bool {
		l, ok := left[e]
		require.True(t, ok, "unexpected event %+v", e)
		assert.Greater(t, l, 0, "already got %+v", e)
		left[e]--
		for _, v := range left {
			if v > 0 {
				return true
			}
		}
		return false
	})
}

func AssertEvents(t *testing.T, events chan lu.Event, constraints ...EventConstraint) {
	var cIdx int
	count := len(events)
	for ev := range events {
		t.Log("checking event", ev)
		require.Less(t, cIdx, len(constraints), "additional unexpected event")
		more := constraints[cIdx].CheckMore(t, ev)
		if !more {
			cIdx++
		}
		count--
		if count == 0 {
			break
		}
	}
	assert.Equal(t, len(constraints), cIdx, "expected more events")
}
