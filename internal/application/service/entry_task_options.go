package service

import (
	"reliquary/internal/config"
	taskqueue "reliquary/internal/queue"
	"reliquary/internal/types"
)

func documentProcessTaskOptions(cfg *config.Config, extra ...taskqueue.Option) []taskqueue.Option {
	opts := []taskqueue.Option{
		taskqueue.Queue(types.QueueDefault),
		taskqueue.Timeout(config.DocumentProcessTimeout(cfg)),
		taskqueue.MaxRetry(3),
	}
	opts = append(opts, extra...)
	return opts
}
