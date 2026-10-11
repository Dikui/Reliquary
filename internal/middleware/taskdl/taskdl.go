// Package taskdl defines the callback run after an exhausted task and its
// dead-letter row have committed together in PostgreSQL.
package taskdl

import (
	"context"
	"reliquary/internal/queue"
)

// OnDeadLetter updates the article status and processing span after archival.
// The queue supplies a live background context and contains callback panics.
// Archive persistence itself is handled by the worker transaction.
type OnDeadLetter func(context.Context, *queue.Task, error)
