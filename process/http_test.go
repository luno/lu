package process

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/luno/jettison/jtest"

	"github.com/luno/lu"
)

func TestProcess(t *testing.T) {
	testCases := []struct {
		name    string
		process lu.Process
	}{
		{
			name:    "http server",
			process: HTTP("test", &http.Server{Addr: "localhost:8080"}),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var a lu.App
			a.AddProcess(tc.process)

			err := a.Launch(context.Background())
			jtest.AssertNil(t, err)

			time.Sleep(100 * time.Millisecond)

			err = a.Shutdown()
			jtest.RequireNil(t, err)
		})
	}
}
