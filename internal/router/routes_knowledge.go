package router

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"reliquary/internal/handler"
)

// RegisterChunkerDebugRoutes wires the read-only chunker preview endpoint
// used by the KB editor's debug panel. Stateless — uses no service deps.
//
// Viewer+ floor: the endpoint surfaces inside the tenant UI, so any
// authenticated tenant member can call it; revoked accounts whose JWT
// has not yet expired are kept out by the role check, matching the
// rest of the RBAC matrix in this file.
func RegisterChunkerDebugRoutes(r *gin.RouterGroup, g *rbacGuards) {
	g.apiKeyRoute(r, http.MethodPost, "/chunker/preview", apiKeyRetrieve(apiKeyIngest(apiKeyFullAccess())), g.Viewer(), handler.PreviewChunking)
}

func RegisterChunkRoutes(r *gin.RouterGroup, handler *handler.ChunkHandler, g *rbacGuards) {
	// 分块路由组。Scoped API key 需要 ingest 能力写内容，retrieve 能力读内容；
	// 两者仍受 KB 白名单约束。
	chunks := g.apiKeyGroup(r.Group("/chunks"), apiKeyIngest(apiKeyFullAccess()))
	chunkRead := chunks.With(apiKeyRetrieve(apiKeyFullAccess()))
	{
		chunkRead.GET("/:knowledge_id", g.Viewer(), g.KBAccessReadFromKnowledgeIDParam("knowledge_id"), handler.ListKnowledgeChunks)
		// 通过chunk_id获取单个chunk（不需要knowledge_id） — Viewer+ 且对父 KB 有 read 权限
		chunkRead.GET("/by-id/:id", g.Viewer(), g.KBAccessReadFromChunkIDParam("id"), handler.GetChunkByIDOnly)
		chunkRead.GET("/:knowledge_id/:id/revisions", g.Viewer(), g.KBAccessReadFromKnowledgeIDParam("knowledge_id"), handler.ListChunkRevisions)
		chunks.DELETE("/:knowledge_id/:id", g.Contributor(), g.KBAccessWriteFromKnowledgeIDParam("knowledge_id"), handler.DeleteChunk)
		chunks.DELETE("/:knowledge_id", g.Contributor(), g.KBAccessWriteFromKnowledgeIDParam("knowledge_id"), handler.DeleteChunksByKnowledgeID)
		chunks.PUT("/:knowledge_id/:id", g.Contributor(), g.KBAccessWriteFromKnowledgeIDParam("knowledge_id"), handler.UpdateChunk)
		chunks.POST("/:knowledge_id/:id/revert", g.Contributor(), g.KBAccessWriteFromKnowledgeIDParam("knowledge_id"), handler.RevertChunk)
		chunks.DELETE("/by-id/:id/questions", g.Contributor(), g.KBAccessWriteFromChunkIDParam("id"), handler.DeleteGeneratedQuestion)
		chunks.PUT("/by-id/:id/questions", g.Contributor(), g.KBAccessWriteFromChunkIDParam("id"), handler.UpsertGeneratedQuestion)
		chunks.POST("/by-id/:id/questions/regenerate", g.Contributor(), g.KBAccessWriteFromChunkIDParam("id"), handler.RegenerateGeneratedQuestions)
	}
}

func RegisterKnowledgeRoutes(r *gin.RouterGroup, handler *handler.KnowledgeHandler, g *rbacGuards) {
	// 知识库下的知识路由组（URL :id is the KB id）。Scoped API key 需要
	// ingest 能力才能写内容，且仍受 KB 范围限制；清空 KB 只允许 full-access key。
	kb := g.apiKeyGroup(r.Group("/knowledge-bases/:id/knowledge"), apiKeyIngest(apiKeyFullAccess()))
	kbRead := kb.With(apiKeyRetrieve(apiKeyFullAccess()))
	{
		kb.POST("/file", g.Contributor(), g.KBAccessWrite("id"), handler.CreateKnowledgeFromFile)
		kb.POST("/url", g.Contributor(), g.KBAccessWrite("id"), handler.CreateKnowledgeFromURL)
		kb.POST("/manual", g.Contributor(), g.KBAccessWrite("id"), handler.CreateManualKnowledge)
		kbRead.GET("", g.Viewer(), g.KBAccessRead("id"), handler.ListKnowledge)
		kbRead.GET("/folders", g.Viewer(), g.KBAccessRead("id"), handler.ListKnowledgeFolders)
		kb.PUT("/folders", g.Contributor(), g.KBAccessWrite("id"), handler.RenameKnowledgeFolder)
		// Clearing all contents under a KB is a destructive op; gate
		// behind Admin instead of Contributor.
		kb.With(apiKeyFullAccess()).DELETE("", g.Admin(), g.KBAccessWrite("id"), handler.ClearKnowledgeBaseContents)
	}

	// 知识路由组（URL :id is a knowledge id; the guard walks it to the parent KB）
	kgrp := r.Group("/knowledge")
	k := g.apiKeyGroup(kgrp, apiKeyIngest(apiKeyFullAccess()))
	kRead := k.With(apiKeyRetrieve(apiKeyFullAccess()))
	{
		// Cross-knowledge endpoints (no :id) can't be gated on a single
		// KB via the URL — they accept a kb_id (or source/target KB) in the
		// body and the handler fans out the access check itself. /batch and
		// /search are read routes (retrieve). /move, /batch-delete,
		// /batch-reparse and /tags are content writes that each bound
		// themselves to a single (or source+target) KB and enforce the API
		// key's KB allow-list downstream — MoveKnowledge via
		// requireTenantAPIKeyKnowledgeBases(source,target); the batch ops via
		// validateKnowledgeBaseAccessWithKBID (requireTenantAPIKeyKnowledgeBase)
		// plus a per-item "belongs to the authorized KB" check in the handler
		// and service. They are therefore declared for API keys under the
		// ingest capability, matching their single-document siblings
		// (k.DELETE("/:id"), k.POST("/:id/reparse"), k.PUT("/:id")).
		kRead.GET("/batch", g.Viewer(), handler.GetKnowledgeBatch)
		kRead.GET("/:id", g.Viewer(), g.KBAccessReadFromKnowledgeIDParam("id"), handler.GetKnowledge)
		kRead.GET("/:id/stages", g.Viewer(), g.KBAccessReadFromKnowledgeIDParam("id"), handler.GetKnowledgeSpans)
		kRead.GET("/:id/spans", g.Viewer(), g.KBAccessReadFromKnowledgeIDParam("id"), handler.GetKnowledgeSpans)
		k.DELETE("/:id", g.Contributor(), g.KBAccessWriteFromKnowledgeIDParam("id"), handler.DeleteKnowledge)
		k.PUT("/:id", g.Contributor(), g.KBAccessWriteFromKnowledgeIDParam("id"), handler.UpdateKnowledge)
		k.POST("/:id/regenerate-summary", g.Contributor(), g.KBAccessWriteFromKnowledgeIDParam("id"), handler.RegenerateKnowledgeSummary)
		k.PUT("/manual/:id", g.Contributor(), g.KBAccessWriteFromKnowledgeIDParam("id"), handler.UpdateManualKnowledge)
		k.POST("/:id/reparse", g.Contributor(), g.KBAccessWriteFromKnowledgeIDParam("id"), handler.ReparseKnowledge)
		k.POST("/:id/cancel-parse", g.Contributor(), g.KBAccessWriteFromKnowledgeIDParam("id"), handler.CancelKnowledgeParse)
		k.POST("/:id/review", g.Contributor(), g.KBAccessWriteFromKnowledgeIDParam("id"), handler.ReviewKnowledgeIntake)
		kRead.GET("/:id/download", g.Contributor(), g.KBAccessWriteFromKnowledgeIDParam("id"), handler.DownloadKnowledgeFile)
		kRead.GET("/:id/preview", g.Viewer(), g.KBAccessReadFromKnowledgeIDParam("id"), handler.PreviewKnowledgeFile)
		k.PUT("/image/:id/:chunk_id", g.Contributor(), g.KBAccessWriteFromKnowledgeIDParam("id"), handler.UpdateImageInfo)
		kRead.GET("/search", g.Viewer(), handler.SearchKnowledge)
		kRead.GET("/move/progress/:task_id", g.Viewer(), handler.GetKnowledgeMoveProgress)
		// Batch / cross-KB content writes: JWT Contributor+, or an API key
		// with the ingest capability (or full access). Each handler binds the
		// operation to a single (move: source+target) KB and rejects any KB
		// or knowledge id outside the key's allow-list, so a scoped ingest key
		// can only touch KBs it is already permitted to write.
		k.PUT("/tags", g.Contributor(), handler.UpdateKnowledgeTagBatch)
		k.POST("/batch-reparse", g.Contributor(), handler.BatchReparseKnowledge)
		k.POST("/batch-delete", g.Contributor(), handler.BatchDeleteKnowledge)
		k.POST("/folder", g.Contributor(), handler.MoveKnowledgeToFolder)
		k.POST("/move", g.Contributor(), handler.MoveKnowledge)
	}
}

// RegisterKnowledgeBaseRoutes 注册知识库相关的路由
func RegisterKnowledgeBaseRoutes(r *gin.RouterGroup, handler *handler.KnowledgeBaseHandler, g *rbacGuards) {
	kbgrp := r.Group("/knowledge-bases")
	kb := g.apiKeyGroup(kbgrp, apiKeyRetrieve(apiKeyFullAccess()))
	kbManagement := kb.With(apiKeyManageKnowledgeBases(apiKeyFullAccess()))
	{
		// 创建知识库 — JWT Contributor+；API key 需 manage_kbs 或 full-access。
		kbManagement.POST("", g.Contributor(), handler.CreateKnowledgeBase)
		// 获取知识库列表 — Viewer+ for JWT callers; retrieve-capable API keys pass via the gate.
		kb.GET("", g.Viewer(), handler.ListKnowledgeBases)
		// 获取知识库详情 — Viewer+ 且对 KB 有 read 权限
		kb.GET("/:id", g.Viewer(), g.KBAccessRead("id"), handler.GetKnowledgeBase)
		kbManagement.PUT("/:id", g.Contributor(), g.KBAccessWrite("id"), handler.UpdateKnowledgeBase)
		kbManagement.DELETE("/:id", g.Contributor(), g.KBAccessWrite("id"), handler.DeleteKnowledgeBase)
		kb.PUT("/:id/pin", g.Viewer(), g.KBAccessRead("id"), handler.TogglePinKnowledgeBase)
		// 混合搜索 — Viewer+ 且对 KB 有 read 权限 (read-only)
		// POST is preferred; GET with JSON body is kept for backward compatibility (#1727).
		kb.POST("/:id/hybrid-search", g.Viewer(), g.KBAccessRead("id"), handler.HybridSearch)
		kb.GET("/:id/hybrid-search", g.Viewer(), g.KBAccessRead("id"), handler.HybridSearch)
		kbManagement.POST("/copy", g.Contributor(), handler.CopyKnowledgeBase)
		// 创建知识库副本 — 产出新 KB，与 create 同档：JWT Contributor+，API key 需 manage_kbs 或 full-access；
		// 且对源 KB 有 read 权限（KBAccessRead 会对限定 key 兜住源 KB）。只创建新的 KB 设置记录，不复制内容/索引/分享。
		kbManagement.POST("/:id/duplicate", g.Contributor(), g.KBAccessRead("id"), handler.DuplicateKnowledgeBase)
		// 获取知识库复制进度 — Viewer+；只读。manage_kbs（发起 copy 的 key）或
		// retrieve 均可轮询；任务按租户隔离（requireTaskProgressTenant），key 只能
		// 查本租户任务。
		kb.With(apiKeyRetrieve(apiKeyManageKnowledgeBases(apiKeyFullAccess()))).
			GET("/copy/progress/:task_id", g.Viewer(), handler.GetKBCloneProgress)
		// 获取可移动目标知识库列表 — Viewer+ 且对 KB 有 read 权限
		kb.GET("/:id/move-targets", g.Viewer(), g.KBAccessRead("id"), handler.ListMoveTargets)
	}
}

// RegisterKnowledgeBaseActivityRoutes exposes the read-only per-KB activity
// feed. It intentionally stays JWT-only: audit history is a sensitive owner
// surface and no existing workspace API-key capability grants audit access.
func RegisterKnowledgeBaseActivityRoutes(r *gin.RouterGroup, auditHandler *handler.AuditLogHandler, g *rbacGuards) {
	if auditHandler == nil {
		return
	}
	r.GET("/knowledge-bases/:id/activity",
		g.Contributor(), g.KBAccessRead("id"), auditHandler.ListKnowledgeBaseActivity)
}

func RegisterKnowledgeAnomalyRoutes(r *gin.RouterGroup, h *handler.KnowledgeAnomalyHandler, g *rbacGuards) {
	if h == nil {
		return
	}
	group := g.apiKeyGroup(r.Group("/knowledge-bases/:id/anomalies"), apiKeyIngest(apiKeyFullAccess()))
	read := group.With(apiKeyRetrieve(apiKeyFullAccess()))
	read.GET("", g.Viewer(), g.KBAccessRead("id"), h.List)
	group.POST("", g.Viewer(), g.KBAccessRead("id"), h.Create)
	group.POST("/:anomaly_id/decision", g.Contributor(), g.KBAccessWrite("id"), h.Resolve)
}

func RegisterKnowledgeStrategyRoutes(r *gin.RouterGroup, h *handler.KnowledgeStrategyHandler, g *rbacGuards) {
	if h == nil {
		return
	}
	// Strategy text and previews are owner-only, including reads.
	group := r.Group("/knowledge-bases/:id/strategies")
	group.GET("", g.Contributor(), g.KBAccessRead("id"), h.List)
	group.POST("", g.Contributor(), g.KBAccessWrite("id"), h.Propose)
	group.POST("/:version_id/activate", g.Contributor(), g.KBAccessWrite("id"), h.Activate)
	group.POST("/:version_id/evaluate", g.Contributor(), g.KBAccessWrite("id"), h.Evaluate)
	group.POST("/reprocess", g.Contributor(), g.KBAccessWrite("id"), h.Reprocess)
}

func RegisterKnowledgeTagRoutes(r *gin.RouterGroup, tagHandler *handler.TagHandler, g *rbacGuards) {
	if tagHandler == nil {
		return
	}
	kbTags := g.apiKeyGroup(r.Group("/knowledge-bases/:id/tags"), apiKeyIngest(apiKeyFullAccess()))
	kbTagsRead := kbTags.With(apiKeyRetrieve(apiKeyFullAccess()))
	{
		// KBAccessRead/Write resolve own/shared/agent-visible access and
		// rewrite the request's tenant context to the effective tenant
		// for the duration of the handler — so the handler no longer
		// needs its own effectiveCtxForKB helper.
		kbTagsRead.GET("", g.Viewer(), g.KBAccessRead("id"), tagHandler.ListTags)
		kbTags.POST("", g.Contributor(), g.KBAccessWrite("id"), tagHandler.CreateTag)
		kbTags.PUT("/:tag_id", g.Contributor(), g.KBAccessWrite("id"), tagHandler.UpdateTag)
		kbTags.DELETE("/:tag_id", g.Contributor(), g.KBAccessWrite("id"), tagHandler.DeleteTag)
	}
}

func RegisterWikiPageRoutes(r *gin.RouterGroup, wikiHandler *handler.WikiPageHandler, g *rbacGuards) {
	wiki := g.apiKeyGroup(r.Group("/knowledgebase/:kb_id/wiki"), apiKeyIngest(apiKeyFullAccess()))
	wikiRead := wiki.With(apiKeyRetrieve(apiKeyFullAccess()))
	{
		// Page CRUD
		wikiRead.GET("/pages", g.Viewer(), g.KBAccessRead("kb_id"), wikiHandler.ListPages)
		wiki.POST("/pages", g.Contributor(), g.KBAccessWrite("kb_id"), wikiHandler.CreatePage)
		wiki.PUT("/move-page", g.Contributor(), g.KBAccessWrite("kb_id"), wikiHandler.MovePage)
		wikiRead.GET("/pages/*slug", g.Viewer(), g.KBAccessRead("kb_id"), wikiHandler.GetPage)
		wiki.PUT("/pages/*slug", g.Contributor(), g.KBAccessWrite("kb_id"), wikiHandler.UpdatePage)
		wiki.DELETE("/pages/*slug", g.Contributor(), g.KBAccessWrite("kb_id"), wikiHandler.DeletePage)

		// Revision history (slug is a catch-all like /pages; revert carries
		// the slug in the body for the same reason move-page does)
		wikiRead.GET("/revisions/*slug", g.Viewer(), g.KBAccessRead("kb_id"), wikiHandler.ListRevisions)
		wiki.POST("/revert", g.Contributor(), g.KBAccessWrite("kb_id"), wikiHandler.RevertPage)
		wiki.POST("/source-changes/ignore", g.Contributor(), g.KBAccessWrite("kb_id"), wikiHandler.IgnoreSourceChanges)
		wiki.POST("/source-changes/regenerate", g.Contributor(), g.KBAccessWrite("kb_id"), wikiHandler.RegeneratePage)

		// Folder tree (directory nodes)
		wikiRead.GET("/folders", g.Viewer(), g.KBAccessRead("kb_id"), wikiHandler.ListFolders)
		wiki.POST("/folders", g.Contributor(), g.KBAccessWrite("kb_id"), wikiHandler.CreateFolder)
		wiki.PUT("/folders/:folder_id", g.Contributor(), g.KBAccessWrite("kb_id"), wikiHandler.UpdateFolder)
		wiki.DELETE("/folders/:folder_id", g.Contributor(), g.KBAccessWrite("kb_id"), wikiHandler.DeleteFolder)

		// Special pages
		wikiRead.GET("/index", g.Viewer(), g.KBAccessRead("kb_id"), wikiHandler.GetIndex)

		// Graph and stats
		wikiRead.GET("/graph", g.Viewer(), g.KBAccessRead("kb_id"), wikiHandler.GetGraph)
		wikiRead.GET("/stats", g.Viewer(), g.KBAccessRead("kb_id"), wikiHandler.GetStats)

		// Search and maintenance
		wikiRead.GET("/search", g.Viewer(), g.KBAccessRead("kb_id"), wikiHandler.SearchPages)
		wiki.POST("/rebuild-links", g.Contributor(), g.KBAccessWrite("kb_id"), wikiHandler.RebuildLinks)
		wikiRead.GET("/lint", g.Viewer(), g.KBAccessRead("kb_id"), wikiHandler.Lint)
		wiki.POST("/auto-fix", g.Contributor(), g.KBAccessWrite("kb_id"), wikiHandler.AutoFix)

		// Issues
		wikiRead.GET("/issues", g.Viewer(), g.KBAccessRead("kb_id"), wikiHandler.ListIssues)
		wiki.PUT("/issues/:issue_id/status", g.Contributor(), g.KBAccessWrite("kb_id"), wikiHandler.UpdateIssueStatus)
	}
}
