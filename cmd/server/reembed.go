package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/signal"

	"reliquary/internal/container"
	"reliquary/internal/types"
)

func parseReembedArgs(args []string) (action, runID string, err error) {
	if len(args) == 0 {
		return "start", "", nil
	}
	if len(args) == 1 && args[0] == "--preserve-entity-timestamps" {
		return "maintenance_start", "", nil
	}
	if len(args) == 2 && args[1] != "" {
		if args[0] == "--resume" {
			return "resume", args[1], nil
		}
		if args[0] == "--retry-failed" {
			return "retry_failed", args[1], nil
		}
	}
	return "", "", errors.New("usage: server reembed [--preserve-entity-timestamps|--resume RUN_ID|--retry-failed RUN_ID]")
}

func runReembed(args []string, output io.Writer) error {
	action, runID, err := parseReembedArgs(args)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), shutdownSignals...)
	defer cancel()
	encoder := json.NewEncoder(output)
	var outputErr error
	status, runErr := container.Reembed(ctx, action, runID, func(run *types.EmbeddingRebuildRun) {
		if outputErr == nil {
			outputErr = encoder.Encode(map[string]any{"event": "progress", "data": run})
		}
	})
	if status != nil && outputErr == nil {
		outputErr = encoder.Encode(map[string]any{"event": "status", "data": status})
	}
	if runErr == nil && status != nil && status.LatestRun != nil && status.LatestRun.FailedCount > 0 {
		runErr = fmt.Errorf("embedding rebuild has %d failed items; retry with --retry-failed %s", status.LatestRun.FailedCount, status.LatestRun.ID)
	}
	return errors.Join(runErr, outputErr)
}
