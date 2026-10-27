package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// Serve runs the shared HTTP listener until the process context is cancelled.
// The caller initializes dependencies, registers routes and starts workers.
func Serve(ctx context.Context, address string, handler http.Handler) error {
	if ctx == nil || handler == nil {
		return fmt.Errorf("server context and handler are required")
	}
	httpServer := &http.Server{
		Addr: address, Handler: handler, ReadHeaderTimeout: 10 * time.Second,
	}
	completed := make(chan error, 1)
	go func() { completed <- httpServer.ListenAndServe() }()
	select {
	case err := <-completed:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		if err := httpServer.Shutdown(context.Background()); err != nil {
			return fmt.Errorf("shutdown HTTP: %w", err)
		}
		if err := <-completed; !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}
