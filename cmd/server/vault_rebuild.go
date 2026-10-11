package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/signal"

	"reliquary/internal/container"
	"reliquary/internal/logger"
)

func runVaultRebuild(output io.Writer) error {
	logger.SetOutput(os.Stderr)
	ctx, cancel := signal.NotifyContext(context.Background(), shutdownSignals...)
	defer cancel()
	result, err := container.RebuildVault(ctx)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}
