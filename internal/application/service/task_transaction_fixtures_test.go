package service

import (
	"context"
	"gorm.io/gorm"
	taskqueue "reliquary/internal/queue"
)

// These fixtures inspect payloads while PostgreSQL integration tests exercise
// the real queue separately. The production transaction adapter has no fallback.

func (q failingReparseTaskEnqueuer) EnqueueTx(_ context.Context, _ *gorm.DB, task *taskqueue.Task, opts ...taskqueue.Option) (*taskqueue.TaskInfo, error) {
	return q.Enqueue(task, opts...)
}

func (q kbDeleteTaskEnqueuer) EnqueueTx(_ context.Context, _ *gorm.DB, task *taskqueue.Task, opts ...taskqueue.Option) (*taskqueue.TaskInfo, error) {
	return q.Enqueue(task, opts...)
}

func (q *intakeTask) EnqueueTx(_ context.Context, _ *gorm.DB, task *taskqueue.Task, opts ...taskqueue.Option) (*taskqueue.TaskInfo, error) {
	return q.Enqueue(task, opts...)
}

func (q *metadataUpdateTaskEnqueuer) EnqueueTx(_ context.Context, _ *gorm.DB, task *taskqueue.Task, opts ...taskqueue.Option) (*taskqueue.TaskInfo, error) {
	return q.Enqueue(task, opts...)
}

func (q *nameEmbeddingTriggerQueue) EnqueueTx(_ context.Context, _ *gorm.DB, task *taskqueue.Task, opts ...taskqueue.Option) (*taskqueue.TaskInfo, error) {
	return q.Enqueue(task, opts...)
}

func (q parentChildTaskEnqueuer) EnqueueTx(_ context.Context, _ *gorm.DB, task *taskqueue.Task, opts ...taskqueue.Option) (*taskqueue.TaskInfo, error) {
	return q.Enqueue(task, opts...)
}

func (q *nameRebuildQueue) EnqueueTx(_ context.Context, _ *gorm.DB, task *taskqueue.Task, opts ...taskqueue.Option) (*taskqueue.TaskInfo, error) {
	return q.Enqueue(task, opts...)
}

func (q *createKnowledgeTaskEnqueuerStub) EnqueueTx(_ context.Context, _ *gorm.DB, task *taskqueue.Task, opts ...taskqueue.Option) (*taskqueue.TaskInfo, error) {
	return q.Enqueue(task, opts...)
}

func (q *retainedProcessingQueue) EnqueueTx(_ context.Context, _ *gorm.DB, task *taskqueue.Task, opts ...taskqueue.Option) (*taskqueue.TaskInfo, error) {
	return q.Enqueue(task, opts...)
}
