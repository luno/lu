//go:build slowtests

package process_test

import (
	"bitx/app/lu/process"
	"context"
	"fmt"
	"time"

	"github.com/luno/jettison/errors"
)

func ExampleWithErrorSleepFunc() {
	t0 := time.Now()
	f := func(ctx context.Context) error {
		fmt.Printf("Running for %d seconds\n", int(time.Since(t0).Seconds()))
		return errors.New("error")
	}
	p := process.Loop(f, process.WithErrorSleep(5*time.Second))

	ctx, cancel := context.WithTimeout(context.Background(), 18*time.Second)
	defer cancel()
	_ = p.Run(ctx)
	// Output: Running for 0 seconds
	// Running for 5 seconds
	// Running for 10 seconds
	// Running for 15 seconds
}
