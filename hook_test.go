package lu

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSortHooks(t *testing.T) {
	testCases := []struct {
		name      string
		hooks     []hook
		expSorted []hook
	}{
		{
			name: "by order",
			hooks: []hook{
				{Name: "c", createOrder: 3},
				{Name: "a", createOrder: 1},
				{Name: "b", createOrder: 2},
			},
			expSorted: []hook{
				{Name: "a", createOrder: 1},
				{Name: "b", createOrder: 2},
				{Name: "c", createOrder: 3},
			},
		},
		{
			name: "by priority",
			hooks: []hook{
				{Priority: 2},
				{Priority: 3},
				{Priority: 1},
			},
			expSorted: []hook{
				{Priority: 1},
				{Priority: 2},
				{Priority: 3},
			},
		},
		{
			name: "by both",
			hooks: []hook{
				{createOrder: 1, Priority: HookPriorityDefault},
				{createOrder: 2, Priority: HookPriorityDefault},
				{createOrder: 3, Priority: HookPriorityLast},
				{createOrder: 4, Priority: HookPriorityDefault},
				{createOrder: 5, Priority: HookPriorityFirst},
			},
			expSorted: []hook{
				{createOrder: 5, Priority: HookPriorityFirst},
				{createOrder: 1, Priority: HookPriorityDefault},
				{createOrder: 2, Priority: HookPriorityDefault},
				{createOrder: 4, Priority: HookPriorityDefault},
				{createOrder: 3, Priority: HookPriorityLast},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sortedHooks := make([]hook, len(tc.hooks))
			copy(sortedHooks, tc.hooks)
			sortHooks(sortedHooks)
			assert.Equal(t, tc.expSorted, sortedHooks)
		})
	}
}

func TestOptions(t *testing.T) {
	testCases := []struct {
		name    string
		options []HookOption
		expHook hook
	}{
		{
			name:    "defaults",
			expHook: hook{Priority: HookPriorityDefault},
		},
		{
			name:    "name",
			options: []HookOption{WithHookName("test name")},
			expHook: hook{Name: "test name", Priority: HookPriorityDefault},
		},
		{
			name:    "priority",
			options: []HookOption{WithHookPriority(23)},
			expHook: hook{Priority: 23},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var h hook
			applyHookOptions(&h, tc.options)
			assert.Equal(t, tc.expHook, h)
		})
	}
}

func TestPriorityPanic(t *testing.T) {
	assert.Panics(t, func() {
		WithHookPriority(-101)
	})
	assert.Panics(t, func() {
		WithHookPriority(101)
	})
}
