// Package main is the main package for the Reliquary server
// It contains the main function and the entry point for the server
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"

	"reliquary/internal/config"
	"reliquary/internal/container"
	"reliquary/internal/logger"
	"reliquary/internal/runtime"
	"reliquary/internal/types/interfaces"
)

func main() {
	if handled, err := runServerCommand(os.Args[1:], os.Stdin, os.Stdout); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	// Set Gin mode
	if os.Getenv("GIN_MODE") == "release" {
		gin.SetMode(gin.ReleaseMode)
	} else {
		gin.SetMode(gin.DebugMode)
	}
	// Mute Gin's per-route registration spam (one line per route × ~150
	// routes) — replaced by a single summary printed after router build.
	runtime.SilenceGinRouteSpam()
	// Print the env banner before container build so operators see what
	// config landed even when DB / storage init fails.
	runtime.LogStartupEnv(context.Background())
	runtime.MarkServerStarted()

	// Build dependency injection container
	c := container.BuildContainer(runtime.GetContainer())

	// Run application
	err := c.Invoke(func(
		cfg *config.Config,
		router *gin.Engine,
		resourceCleaner interfaces.ResourceCleaner,
	) error {
		// Create HTTP server
		server := &http.Server{
			Handler: router,
		}

		addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
		listener, err := listenWithRetry(addr, 10, 300*time.Millisecond)
		if err != nil {
			return fmt.Errorf("failed to start server: %v", err)
		}

		runtime.LogGinRouteCount(context.Background())
		logger.Infof(context.Background(), "Server is running at %s", addr)
		return serveUntilShutdown(server, listener, cfg.Server.ShutdownTimeout, resourceCleaner)
	})
	if err != nil {
		logger.Fatalf(context.Background(), "Failed to run application: %v", err)
	}
}
