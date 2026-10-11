package container

import (
	"reliquary/internal/application/service"
	"reliquary/internal/config"
	"reliquary/internal/types/interfaces"
)

func registerTopicIntegration(topics *service.TopicService, models interfaces.ModelService, logs *service.OperationLogService, vault *service.VaultService, cfg *config.Config, tasks interfaces.TaskEnqueuer) error {
	topics.SetModelService(models)
	topics.SetOperationLogs(logs)
	topics.SetHeaderRefresher(vault)
	topics.SetBackfillBatchSize(cfg.TopicBackfillBatchSize)
	topics.SetTaskEnqueuer(tasks)
	return nil
}
