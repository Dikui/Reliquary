package router

import (
	"github.com/gin-gonic/gin"
	"reliquary/internal/handler"
)

// Use the registered Gin route template and method. POST queries, previews,
// model setup, credentials and maintenance operations remain available.
var businessWriteRoutes = map[string]struct{}{
	"POST /api/v1/ingest":                                                 {},
	"POST /api/v1/ingest/assets":                                          {},
	"POST /api/v1/ingest/:id/discard":                                     {},
	"DELETE /api/v1/documents/:id":                                        {},
	"POST /api/v1/entries/batches":                                        {},
	"POST /api/v1/entries/batches/:batch_id/retry-failed":                 {},
	"POST /api/v1/libraries/:id/rebuild":                                  {},
	"PUT /api/v1/entries/:id/body":                                        {},
	"PUT /api/v1/entries/:id":                                             {},
	"PUT /api/v1/entries/:id/star":                                        {},
	"PUT /api/v1/entries/:id/content-type":                                {},
	"PUT /api/v1/entries/:id/topics":                                      {},
	"POST /api/v1/entries/:id/reprocess":                                  {},
	"POST /api/v1/entries/:id/reparse":                                    {},
	"POST /api/v1/entries/:id/cancel-parse":                               {},
	"POST /api/v1/entries/:id/review":                                     {},
	"POST /api/v1/libraries":                                              {},
	"DELETE /api/v1/libraries/:id":                                        {},
	"POST /api/v1/libraries/:id/shelf/import-plans/suggest":               {},
	"POST /api/v1/libraries/:id/shelf/imports":                            {},
	"POST /api/v1/libraries/:id/shelf/imports/suggest-text":               {},
	"POST /api/v1/libraries/:id/shelf/changesets":                         {},
	"PATCH /api/v1/libraries/:id/shelf/changesets/:changeset_id":          {},
	"POST /api/v1/libraries/:id/shelf/changesets/:changeset_id/confirm":   {},
	"POST /api/v1/libraries/:id/entity-attachments":                       {},
	"DELETE /api/v1/entity-attachments/:attachment_id":                    {},
	"POST /api/v1/libraries/:id/entities":                                 {},
	"POST /api/v1/libraries/:id/relations":                                {},
	"POST /api/v1/libraries/:id/relation-types/merge":                     {},
	"PUT /api/v1/entries/:id/entity-links/:entity_id":                     {},
	"DELETE /api/v1/relations/:relation_id":                               {},
	"PATCH /api/v1/relations/:relation_id":                                {},
	"POST /api/v1/entities/merge-suggestions/:suggestion_id/accept":       {},
	"POST /api/v1/entities/merge-suggestions/:suggestion_id/reject":       {},
	"POST /api/v1/entities/merge":                                         {},
	"PATCH /api/v1/entities/:entity_id":                                   {},
	"DELETE /api/v1/entities/:entity_id":                                  {},
	"POST /api/v1/entities/:entity_id/merge":                              {},
	"POST /api/v1/entities/:entity_id/split":                              {},
	"PATCH /api/v1/entities/:entity_id/description":                       {},
	"PATCH /api/v1/entities/:entity_id/name":                              {},
	"POST /api/v1/entities/name-embeddings/rebuild":                       {},
	"POST /api/v1/embeddings/rebuild":                                     {},
	"POST /api/v1/topics":                                                 {},
	"PUT /api/v1/topics/:id":                                              {},
	"DELETE /api/v1/topics/:id":                                           {},
	"POST /api/v1/topics/:id/backfill/retry":                              {},
	"POST /api/v1/follows/view":                                           {},
	"PUT /api/v1/follows/:type/:id":                                       {},
	"DELETE /api/v1/follows/:type/:id":                                    {},
	"PUT /api/v1/diagnostics/content-checks/settings":                     {},
	"POST /api/v1/diagnostics/content-checks/:id/recheck":                 {},
	"POST /api/v1/evaluation/sets":                                        {},
	"PUT /api/v1/evaluation/sets/:set_id":                                 {},
	"DELETE /api/v1/evaluation/sets/:set_id":                              {},
	"POST /api/v1/evaluation/sets/:set_id/questions":                      {},
	"PUT /api/v1/evaluation/sets/:set_id/questions/:question_id":          {},
	"DELETE /api/v1/evaluation/sets/:set_id/questions/:question_id":       {},
	"POST /api/v1/evaluation/sets/:set_id/questions/:question_id/confirm": {},
	"POST /api/v1/evaluation/sets/:set_id/from-record":                    {},
	"POST /api/v1/evaluation/sets/:set_id/candidates":                     {},
	"POST /api/v1/evaluation/sets/:set_id/runs":                           {},
}

// These handlers enqueue or update queue state directly.
var businessWriteOperations = map[string]struct{}{
	"DELETE /api/v1/entries/:id":                                               {},
	"DELETE /api/v1/libraries/:id/entries":                                     {},
	"POST /api/v1/entries/batch-reparse":                                       {},
	"POST /api/v1/entries/batch-delete":                                        {},
	"POST /api/v1/system/runtime/queues/:queue/tasks/:task_id/actions/:action": {},
	"DELETE /api/v1/system/runtime/queues/:queue/archived":                     {},
}

func maintenanceWriteGate(h *handler.MaintenanceHandler) gin.HandlerFunc {
	return func(c *gin.Context) {
		route := c.Request.Method + " " + c.FullPath()
		if _, write := businessWriteRoutes[route]; write {
			h.BusinessWriteGate(c)
			return
		}
		if _, write := businessWriteOperations[route]; write {
			h.BusinessWriteOperation(c)
			return
		}
		c.Next()
	}
}
