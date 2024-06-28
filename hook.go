package lu

import (
	"context"
	"fmt"
	"sort"
)

type hook struct {
	Name        string
	createOrder int
	Priority    HookPriority
	// F is called either at the start or at the end of the application lifecycle
	// ctx will be cancelled if the function takes too long
	F func(ctx context.Context) error
}

func sortHooks(h []hook) {
	sort.Slice(h, func(i, j int) bool {
		hi, hj := h[i], h[j]
		if hi.Priority != hj.Priority {
			return hi.Priority < hj.Priority
		}
		return hi.createOrder < hj.createOrder
	})
}

type HookOption func(*hook)

func applyHookOptions(h *hook, opts []HookOption) {
	for _, o := range opts {
		o(h)
	}
}

// WithHookName is used for logging so each Hook can be identified
func WithHookName(s string) HookOption {
	return func(options *hook) {
		options.Name = s
	}
}

type HookPriority int

const (
	HookPriorityFirst   HookPriority = -100
	HookPriorityDefault HookPriority = 0
	HookPriorityLast    HookPriority = 100
)

// WithHookPriority controls the order in which hooks are run, the lower the value of p
// the earlier it will be run (compared to other hooks)
// The default priority is 0, negative priorities will be run before positive ones
func WithHookPriority(p HookPriority) HookOption {
	if p < HookPriorityFirst || p > HookPriorityLast {
		panic(fmt.Sprintln("invalid hook priority", p))
	}
	return func(options *hook) {
		options.Priority = p
	}
}
