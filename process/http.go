package process

import (
	"context"
	"net/http"

	"github.com/luno/jettison/errors"
	"github.com/luno/jettison/j"
	"github.com/luno/jettison/log"

	"github.com/luno/lu"
)

// HTTP integrates a http.Server as an App Process
func HTTP(name string, server *http.Server) lu.Process {
	p := lu.Process{
		Name: "http " + name,
		Run: func(ctx context.Context) error {
			log.Info(ctx, "Listening for HTTP requests", j.KS("address", server.Addr))
			err := server.ListenAndServe()
			if errors.Is(err, http.ErrServerClosed) {
				// NoReturnErr: Don't need to return this error
				return nil
			}
			return err
		},
		Shutdown: server.Shutdown,
	}
	return p
}

// SecureHTTP integrates a secure http.Server as an App Process
func SecureHTTP(name string, server *http.Server, tlsCert, tlsKey string) lu.Process {
	p := lu.Process{
		Name: "https " + name,
		Run: func(ctx context.Context) error {
			log.Info(ctx, "Listening for HTTPS requests", j.KS("address", server.Addr))
			err := server.ListenAndServeTLS(tlsCert, tlsKey)
			if errors.Is(err, http.ErrServerClosed) {
				// NoReturnErr: Don't need to return this error
				return nil
			}
			return err
		},
		Shutdown: server.Shutdown,
	}
	return p
}
