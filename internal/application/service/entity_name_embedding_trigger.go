package service

import (
	"context"
	"gorm.io/gorm"
)

// EntityNameEmbeddingRebuildTrigger connects settings to the durable embedding queue.
type EntityNameEmbeddingRebuildTrigger interface {
	MarkAndEnqueue(context.Context, uint64, string) error
	Enqueue(context.Context, uint64, string) error
	BindTransaction(context.Context, *gorm.DB) (EntityNameEmbeddingRebuildTrigger, error)
}

func (s *libraryService) SetNameEmbeddingRebuilder(rebuilder EntityNameEmbeddingRebuildTrigger) {
	s.nameEmbeddingRebuilder = rebuilder
}
func (s *modelService) SetNameEmbeddingRebuilder(rebuilder EntityNameEmbeddingRebuildTrigger) {
	s.nameEmbeddingRebuilder = rebuilder
}
