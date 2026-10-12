package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"time"

	"reliquary/internal/logger"
	"reliquary/internal/types/interfaces"
)

func expectedServerClose(err error, stopping bool) bool {
	return errors.Is(err, http.ErrServerClosed) || stopping &&
		(errors.Is(err, net.ErrClosed) || err != nil && strings.Contains(err.Error(), "use of closed network connection"))
}

func serveUntilShutdown(server *http.Server, listener net.Listener, timeout time.Duration, cleaner interfaces.ResourceCleaner) error {
	ctx, done := context.WithCancel(context.Background())
	defer done()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, shutdownSignals...)
	defer signal.Stop(signals)
	var stopping atomic.Bool
	serverClosed := make(chan struct{})
	go func() {
		select {
		case sig := <-signals:
			logger.Infof(context.Background(), "Received signal: %v, starting server shutdown...", sig)
		case <-serverClosed:
			logger.Info(context.Background(), "HTTP server closed, starting resource cleanup...")
		case <-ctx.Done():
			return
		}
		stopping.Store(true)
		// Release the port immediately, then wait for draining and resource cleanup.
		listener.Close()
		if timeout == 0 {
			timeout = 30 * time.Second
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		go func() {
			select {
			case sig := <-signals:
				logger.Warnf(context.Background(), "Received second signal: %v, forcing shutdown...", sig)
				server.Close()
			case <-ctx.Done():
			}
		}()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Errorf(context.Background(), "Server forced to shutdown: %v", err)
			server.Close()
		}
		logger.Info(context.Background(), "Cleaning up resources...")
		if errs := cleaner.Cleanup(shutdownCtx); len(errs) > 0 {
			logger.Errorf(context.Background(), "Errors occurred during resource cleanup: %v", errs)
		}
		logger.Info(context.Background(), "Server has exited")
		done()
	}()
	if err := server.Serve(listener); err != nil && !expectedServerClose(err, stopping.Load()) {
		return fmt.Errorf("server error: %w", err)
	}
	close(serverClosed)
	<-ctx.Done()
	return nil
}
