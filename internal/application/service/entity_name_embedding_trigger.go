package service

import "context"

// EntityNameEmbeddingRebuildTrigger connects settings to the durable embedding queue.
type EntityNameEmbeddingRebuildTrigger interface {
	MarkAndEnqueue(context.Context, uint64, string) error
	Enqueue(context.Context, uint64, string) error
}

func (s *knowledgeBaseService) SetNameEmbeddingRebuilder(rebuilder EntityNameEmbeddingRebuildTrigger) {
	s.nameEmbeddingRebuilder = rebuilder
}
func (s *modelService) SetNameEmbeddingRebuilder(rebuilder EntityNameEmbeddingRebuildTrigger) {
	s.nameEmbeddingRebuilder = rebuilder
}
