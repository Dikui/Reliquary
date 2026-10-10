package container

import (
	"context"

	"reliquary/internal/application/service"
	"reliquary/internal/config"
	"reliquary/internal/types/interfaces"
)

func registerTopicIntegration(topics *service.TopicService, models interfaces.ModelService, logs *service.OperationLogService, vault *service.VaultService, cfg *config.Config, cleaner interfaces.ResourceCleaner) error {
	topics.SetModelService(models)
	topics.SetOperationLogs(logs)
	topics.SetHeaderRefresher(vault)
	topics.SetBackfillBatchSize(cfg.TopicBackfillBatchSize)
	cleaner.RegisterWithName("Topic historical classification", topics.Close)
	return topics.Start(context.Background())
}
