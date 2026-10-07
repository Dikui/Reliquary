package service

import "context"

// ContextNameEmbeddingRebuildTrigger connects settings and explicit rebuilds to
// the durable name embedding queue without changing existing constructors.
type ContextNameEmbeddingRebuildTrigger interface {
	MarkAndEnqueue(context.Context, uint64, string) error
	Enqueue(context.Context, uint64, string) error
}

func (s *KnowledgeDomainService) SetNameEmbeddingRebuilder(rebuilder ContextNameEmbeddingRebuildTrigger) {
	s.nameEmbeddingRebuilder = rebuilder
}
func (s *knowledgeBaseService) SetNameEmbeddingRebuilder(rebuilder ContextNameEmbeddingRebuildTrigger) {
	s.nameEmbeddingRebuilder = rebuilder
}
func (w *KnowledgeContextWorker) SetNameEmbeddingRebuilder(rebuilder ContextNameEmbeddingRebuildTrigger) {
	w.nameEmbeddingRebuilder = rebuilder
}

func (s *KnowledgeContextService) SetNameEmbeddingRebuilder(rebuilder *ContextNameEmbeddingRebuilder) {
	s.nameEmbeddingRebuilder = rebuilder
}

func (s *modelService) SetNameEmbeddingRebuilder(rebuilder ContextNameEmbeddingRebuildTrigger) {
	s.nameEmbeddingRebuilder = rebuilder
}
