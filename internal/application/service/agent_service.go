package service

import (
	"context"
	"database/sql"
	"fmt"

	"gorm.io/gorm"
	"reliquary/internal/agent"
	"reliquary/internal/agent/approval"
	"reliquary/internal/agent/tools"
	"reliquary/internal/config"
	"reliquary/internal/datasource/connector/rss"
	"reliquary/internal/event"
	"reliquary/internal/logger"
	"reliquary/internal/mcp"
	"reliquary/internal/models/chat"
	"reliquary/internal/models/rerank"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	secutils "reliquary/internal/utils"
)

const MAX_ITERATIONS = 100 // Max iterations for agent execution

// dedupStrings removes duplicate strings while preserving the first occurrence order.
func dedupStrings(in []string) []string {
	if len(in) == 0 {
		return in
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// agentHasKnowledgeScope reports whether the agent has any KB retrieval scope for
// this turn. Tag-only @mentions populate SearchTargets (with TagIDs) but leave
// KnowledgeBases / KnowledgeIDs empty — those must still count as in-scope.
func agentHasKnowledgeScope(config *types.AgentConfig) bool {
	if config == nil {
		return false
	}
	return types.HasKnowledgeRetrievalScope(
		config.SearchTargets,
		config.KnowledgeBases,
		config.KnowledgeIDs,
	)
}

func knowledgeBaseScopesForPrompt(config *types.AgentConfig) []string {
	if config == nil {
		return nil
	}
	if len(config.KnowledgeBases) > 0 {
		return config.KnowledgeBases
	}
	return config.SearchTargets.GetAllKnowledgeBaseIDs()
}

// agentService implements agent-related business logic
type agentService struct {
	cfg                  *config.Config
	modelService         interfaces.ModelService
	mcpServiceService    interfaces.MCPServiceService
	mcpManager           *mcp.MCPManager
	eventBus             *event.EventBus
	db                   *gorm.DB
	knowledgeBaseService interfaces.KnowledgeBaseService
	knowledgeService     interfaces.KnowledgeService
	knowledgeTagService  interfaces.KnowledgeTagService
	fileService          interfaces.FileService
	chunkService         interfaces.ChunkService
	duckdb               *sql.DB
	tenantService        interfaces.TenantService
	messageService       interfaces.MessageService

	toolApprovalGate approval.MCPApproval
}

// NewAgentService creates a new agent service
func NewAgentService(
	cfg *config.Config,
	modelService interfaces.ModelService,
	knowledgeBaseService interfaces.KnowledgeBaseService,
	knowledgeService interfaces.KnowledgeService,
	knowledgeTagService interfaces.KnowledgeTagService,
	fileService interfaces.FileService,
	chunkService interfaces.ChunkService,
	mcpServiceService interfaces.MCPServiceService,
	mcpManager *mcp.MCPManager,
	eventBus *event.EventBus,
	db *gorm.DB,
	duckdb *sql.DB,
	tenantService interfaces.TenantService,
	messageService interfaces.MessageService,

	toolApprovalGate approval.MCPApproval,
) interfaces.AgentService {
	return &agentService{
		cfg:                  cfg,
		modelService:         modelService,
		knowledgeBaseService: knowledgeBaseService,
		knowledgeService:     knowledgeService,
		knowledgeTagService:  knowledgeTagService,
		fileService:          fileService,
		chunkService:         chunkService,
		mcpServiceService:    mcpServiceService,
		mcpManager:           mcpManager,
		eventBus:             eventBus,
		db:                   db,
		duckdb:               duckdb,
		tenantService:        tenantService,
		messageService:       messageService,

		toolApprovalGate: toolApprovalGate,
	}
}

// CreateAgentEngine creates an agent engine with the given configuration and EventBus.
// History is loaded once per turn by the caller (see service.LoadAgentHistory)
// and handed to AgentEngine.Execute as llmContext; the engine is stateless across turns.
func (s *agentService) CreateAgentEngine(
	ctx context.Context,
	config *types.AgentConfig,
	chatModel chat.Chat,
	rerankModel rerank.Reranker,
	eventBus *event.EventBus,
	sessionID, assistantMessageID string,
) (interfaces.AgentEngine, error) {
	logger.Infof(ctx, "Creating agent engine with custom EventBus")

	// 1. Validate config
	if err := s.ValidateConfig(config); err != nil {
		return nil, fmt.Errorf("invalid agent config: %w", err)
	}
	if chatModel == nil {
		return nil, fmt.Errorf("chat model is nil after initialization")
	}

	// 2. Build tool registry
	toolRegistry := tools.NewToolRegistry()
	if config.MaxToolOutputChars > 0 {
		toolRegistry.SetMaxToolOutputSize(config.MaxToolOutputChars)
	}
	if err := s.registerTools(ctx, toolRegistry, config, rerankModel, chatModel, sessionID); err != nil {
		return nil, fmt.Errorf("failed to register tools: %w", err)
	}
	s.registerMCPTools(ctx, toolRegistry, config)

	// 3. Resolve knowledge base and selected document metadata
	kbInfos, selectedDocs := s.resolveKBAndDocInfos(ctx, config)

	// 4. Resolve system prompt template
	systemPromptTemplate := ""
	if config.UseCustomSystemPrompt || config.SystemPrompt != "" {
		systemPromptTemplate = config.ResolveSystemPrompt()
	}

	// 5. Create engine
	engine := agent.NewAgentEngine(
		config, chatModel, toolRegistry, eventBus,
		kbInfos, selectedDocs, sessionID,
		systemPromptTemplate,
	)
	engine.SetAppConfig(s.cfg)
	pinnedMCP := s.resolvePinnedMCPServiceInfos(ctx, config)
	s.attachPinnedMCPToolNames(toolRegistry, pinnedMCP)
	engine.SetPinnedMentions(pinnedMCP)

	// Set VLM image describer for MCP tool result image analysis.
	// When an MCP tool returns images, the engine uses VLM to generate text descriptions
	// and appends them to the tool result content (since Chat Completions API does not
	// reliably support images in tool role messages across providers).
	if config.VLMModelID != "" {
		if vlmModel, err := s.modelService.GetVLMModel(ctx, config.VLMModelID); err == nil {
			engine.SetImageDescriber(func(ctx context.Context, imgBytes []byte, prompt string) (string, error) {
				return vlmModel.Predict(ctx, [][]byte{imgBytes}, prompt)
			})
			logger.Infof(ctx, "VLM image describer set for MCP tool result analysis (model: %s)", config.VLMModelID)
		} else {
			logger.Warnf(ctx, "Failed to load VLM model %s for MCP image fallback: %v", config.VLMModelID, err)
		}
	}

	return engine, nil
}

// registerMCPTools registers MCP tools from enabled services for this tenant.
func (s *agentService) registerMCPTools(
	ctx context.Context,
	toolRegistry *tools.ToolRegistry,
	config *types.AgentConfig,
) {
	tenantID := uint64(0)
	if tid, ok := types.TenantIDFromContext(ctx); ok {
		tenantID = tid
	}
	if tenantID == 0 || s.mcpServiceService == nil || s.mcpManager == nil {
		return
	}

	mcpMode := config.MCPSelectionMode
	if mcpMode == "" {
		mcpMode = "all"
	}
	if mcpMode == "none" {
		logger.Infof(ctx, "MCP services disabled by agent config (mode: none)")
		return
	}

	var mcpServices []*types.MCPService
	var err error

	if mcpMode == "selected" {
		if len(config.MCPServices) == 0 {
			logger.Infof(ctx, "MCP services disabled by agent config (mode: selected, no services)")
			return
		}
		mcpServices, err = s.mcpServiceService.ListMCPServicesByIDs(ctx, tenantID, config.MCPServices)
		if err != nil {
			logger.Warnf(ctx, "Failed to list selected MCP services: %v", err)
			return
		}
		logger.Infof(ctx, "Using %d selected MCP services from agent config", len(mcpServices))
	} else {
		mcpServices, err = s.mcpServiceService.ListMCPServices(ctx, tenantID)
		if err != nil {
			logger.Warnf(ctx, "Failed to list MCP services: %v", err)
			return
		}
	}

	enabledServices := make([]*types.MCPService, 0)
	for _, svc := range mcpServices {
		if svc != nil && svc.Enabled {
			enabledServices = append(enabledServices, svc)
		}
	}
	if len(enabledServices) > 0 {
		registered, err := tools.RegisterMCPTools(
			ctx, toolRegistry, enabledServices, s.mcpManager, s.toolApprovalGate,
		)
		if err != nil {
			logger.Warnf(ctx, "Failed to register MCP tools: %v", err)
		} else if registered == 0 {
			logger.Warnf(ctx, "No MCP tools registered from %d enabled service(s)", len(enabledServices))
		} else {
			logger.Infof(ctx, "Registered %d MCP tool(s) from %d enabled service(s)", registered, len(enabledServices))
		}
	}
}

// resolveKBAndDocInfos loads knowledge base metadata and selected document info for prompt.
func (s *agentService) resolveKBAndDocInfos(
	ctx context.Context,
	config *types.AgentConfig,
) ([]*agent.KnowledgeBaseInfo, []*agent.SelectedDocumentInfo) {
	kbIDs := knowledgeBaseScopesForPrompt(config)
	kbInfos, err := s.getKnowledgeBaseInfos(ctx, kbIDs)
	if err != nil {
		logger.Warnf(ctx, "Failed to get knowledge base details, using IDs only: %v", err)
		kbInfos = make([]*agent.KnowledgeBaseInfo, 0, len(kbIDs))
		for _, kbID := range kbIDs {
			kbInfos = append(kbInfos, &agent.KnowledgeBaseInfo{
				ID:          kbID,
				Name:        kbID,
				Description: "",
				DocCount:    0,
			})
		}
	}

	selectedDocs, err := s.getSelectedDocumentInfos(ctx, config.KnowledgeIDs)
	if err != nil {
		logger.Warnf(ctx, "Failed to get selected document details: %v", err)
		selectedDocs = []*agent.SelectedDocumentInfo{}
	}

	return kbInfos, selectedDocs
}

// registerTools registers tools based on the agent configuration
func (s *agentService) registerTools(
	ctx context.Context,
	registry *tools.ToolRegistry,
	config *types.AgentConfig,
	rerankModel rerank.Reranker,
	chatModel chat.Chat,
	sessionID string,
) error {
	// Source of truth policy:
	//   - `config.AllowedTools` is the explicit, user-editable whitelist —
	//     populated by the agent-type preset on create and freely editable
	//     afterwards.
	//   - We never silently *inject* tools the user didn't pick.
	//   - We still *filter out* tools whose capability prerequisites are missing
	//     (no KB in scope) so the LLM can't call tools
	//     that would error at runtime.
	//   - Legacy agents without AllowedTools fall back to DefaultAllowedTools().
	var allowedTools []string
	if len(config.AllowedTools) > 0 {
		allowedTools = make([]string, len(config.AllowedTools))
		copy(allowedTools, config.AllowedTools)
		logger.Infof(ctx, "Using custom allowed tools from config: %v", allowedTools)
	} else {
		allowedTools = tools.DefaultAllowedTools()
		logger.Infof(ctx, "Using default allowed tools: %v", allowedTools)
	}

	for i, name := range allowedTools {
		allowedTools[i] = tools.CanonicalToolName(name)
	}

	// ---- Capability detection from SearchTargets ----
	var hasVectorKB bool
	for _, target := range config.SearchTargets {
		if target == nil || target.KnowledgeBaseID == "" {
			continue
		}
		kb, err := s.knowledgeBaseService.GetKnowledgeBaseByIDOnly(ctx, target.KnowledgeBaseID)
		if err != nil {
			continue
		}
		if kb.IsVectorEnabled() || kb.IsKeywordEnabled() {
			hasVectorKB = true
		}
	}

	// Filter out knowledge base tools if no knowledge scope is configured for this turn.
	hasKnowledge := agentHasKnowledgeScope(config)
	if !hasKnowledge {
		filteredTools := make([]string, 0)
		kbTools := map[string]bool{
			tools.ToolKnowledgeSearch:        true,
			tools.ToolGrepChunks:             true,
			tools.ToolListKnowledgeChunks:    true,
			tools.ToolQueryKnowledgeContext:  true,
			tools.ToolGetDocumentInfo:        true,
			tools.ToolDataAnalysis:           true,
			tools.ToolDataSchema:             true,
			tools.ToolKnowledgeReportAnomaly: true,
			tools.ToolKnowledgeImportURL:     true,
			tools.ToolKnowledgeApplyAnomaly:  true,
		}

		kbTools[tools.ToolTodoWrite] = true

		for _, toolName := range allowedTools {
			if !kbTools[toolName] {
				filteredTools = append(filteredTools, toolName)
			}
		}
		allowedTools = filteredTools
		logger.Infof(ctx, "Pure Agent Mode: Knowledge base tools filtered out, remaining: %v", allowedTools)
	}

	// Tool capability sets — used by the hard safety nets below to drop tools
	// whose runtime prerequisite (a matching KB surface) is missing.
	//
	// NOTE: ragToolSet must stay in sync with frontend `knowledgeBaseTools`
	// in AgentEditorModal.vue. These are *all* tools that retrieve/inspect
	// content from RAG-style knowledge bases.
	ragToolSet := map[string]bool{
		tools.ToolKnowledgeSearch:       true,
		tools.ToolGrepChunks:            true,
		tools.ToolListKnowledgeChunks:   true,
		tools.ToolQueryKnowledgeContext: true,
		tools.ToolGetDocumentInfo:       true,
	}
	if !hasVectorKB {
		filtered := make([]string, 0, len(allowedTools))
		dropped := make([]string, 0)
		for _, t := range allowedTools {
			if ragToolSet[t] {
				dropped = append(dropped, t)
				continue
			}
			filtered = append(filtered, t)
		}
		allowedTools = filtered
		if len(dropped) > 0 {
			logger.Warnf(ctx, "Dropped RAG tools %v because no RAG-capable KB is in scope", dropped)
		}
	}

	// Deduplicate while preserving original order.
	allowedTools = dedupStrings(allowedTools)

	// Register each allowed tool
	for _, toolName := range allowedTools {
		var toolToRegister types.Tool

		switch toolName {
		case tools.ToolThinking:
			toolToRegister = tools.NewSequentialThinkingTool()
		case tools.ToolTodoWrite:
			toolToRegister = tools.NewTodoWriteTool()
		case tools.ToolKnowledgeSearch:
			toolToRegister = tools.NewKnowledgeSearchTool(
				s.knowledgeBaseService,
				s.knowledgeService,
				s.chunkService,
				config.SearchTargets,
				rerankModel,
				s.cfg,
			).WithSourceDisputes(NewKnowledgeAnomalyService(s.db, s.knowledgeService, s.knowledgeTagService).ForMessages)
		case tools.ToolGrepChunks:
			toolToRegister = tools.NewGrepChunksTool(s.db, config.SearchTargets).
				WithSourceDisputes(NewKnowledgeAnomalyService(s.db, s.knowledgeService, s.knowledgeTagService).ForMessages)
			logger.Infof(ctx, "Registered grep_chunks tool with searchTargets: %d targets", len(config.SearchTargets))
		case tools.ToolListKnowledgeChunks:
			toolToRegister = tools.NewListKnowledgeChunksTool(s.knowledgeService, s.chunkService, config.SearchTargets)
		case tools.ToolQueryKnowledgeContext:
			toolToRegister = tools.NewQueryKnowledgeContextTool(s.knowledgeBaseService, config.SearchTargets).
				WithKnowledgeScope(s.knowledgeService).
				WithContextQuery(NewKnowledgeContextService(s.db, s.modelService).Query)
		case tools.ToolGetDocumentInfo:
			toolToRegister = tools.NewGetDocumentInfoTool(s.knowledgeService, s.chunkService, config.SearchTargets)
		case tools.ToolWebFetch:
			toolToRegister = tools.NewWebFetchTool(chatModel)
			logger.Infof(ctx, "Registered web_fetch tool for session: %s", sessionID)

		case tools.ToolDataAnalysis:
			toolToRegister = tools.NewDataAnalysisTool(s.knowledgeBaseService, s.knowledgeService, s.tenantService, s.fileService, s.duckdb, sessionID).
				WithSearchTargets(config.SearchTargets)
			logger.Infof(ctx, "Registered data_analysis tool for session: %s", sessionID)

		case tools.ToolDataSchema:
			toolToRegister = tools.NewDataSchemaTool(s.knowledgeService, s.chunkService.GetRepository()).
				WithSearchTargets(config.SearchTargets)
			logger.Infof(ctx, "Registered data_schema tool")

		case tools.ToolKnowledgeReportAnomaly:
			anomalies := NewKnowledgeAnomalyService(s.db, s.knowledgeService, nil)
			toolToRegister = tools.NewKnowledgeReportAnomalyTool(config.SearchTargets, func(ctx context.Context, input tools.KnowledgeAnomalyReport) (string, error) {
				item, err := anomalies.Create(ctx, input.KnowledgeBaseID, KnowledgeAnomalyInput{
					KnowledgeIDs: input.KnowledgeIDs, Kind: input.Kind, Description: input.Description,
					ProposalType: input.ProposalType, ProposedTagIDs: input.ProposedTagIDs,
					ProposedRemoveTagIDs: input.ProposedRemoveTagIDs, ProposedDeleteTagID: input.ProposedDeleteTagID,
				})
				if err != nil {
					return "", err
				}
				return item.ID, nil
			})
		case tools.ToolKnowledgeImportURL:
			toolToRegister = tools.NewKnowledgeImportURLTool(config.SearchTargets, func(ctx context.Context, input tools.KnowledgeImportURLInput) error {
				tenantID, ok := types.TenantIDFromContext(ctx)
				if !ok || tenantID == 0 {
					return fmt.Errorf("workspace required")
				}
				userID, ok := types.UserIDFromContext(ctx)
				if !ok || userID == "" {
					return fmt.Errorf("user required for knowledge intake")
				}
				kb, err := s.knowledgeBaseService.GetKnowledgeBaseByIDOnly(ctx, input.KnowledgeBaseID)
				if err != nil {
					return err
				}
				if kb == nil || kb.TenantID != tenantID || !types.IsAuthenticatedFromContext(ctx) {
					return fmt.Errorf("an authenticated user is required to import a URL")
				}
				if blockedIntakeURL(input.URL) {
					return fmt.Errorf("X/Twitter sources require Grok ingestion")
				}
				article, err := rss.FetchArticle(ctx, input.URL)
				if article != nil {
					if recordErr := recordSourceAdapterFailures(ctx, s.db, tenantID, kb.ID, article.AdapterFailures); recordErr != nil {
						return recordErr
					}
				}
				if err != nil {
					return err
				}
				_, err = s.knowledgeService.CreateKnowledgeFromManual(ctx, kb.ID, &types.ManualKnowledgePayload{
					Title: article.Title, Content: string(article.Content), Status: types.ManualKnowledgeStatusPublish,
					SourceInfo: types.SourceInfo{
						Author: article.Metadata["author"], Publisher: article.Metadata["publisher"],
						OriginalURL: input.URL, PublishedAt: article.Metadata["published_at"],
					},
				}, "url_import")
				return err
			})
		case tools.ToolKnowledgeApplyAnomaly:
			anomalies := NewKnowledgeAnomalyService(s.db, s.knowledgeService, s.knowledgeTagService)
			approvalGate, _ := s.toolApprovalGate.(approval.RequiredApproval)
			toolToRegister = tools.NewKnowledgeApplyAnomalyTool(config.SearchTargets, approvalGate, func(ctx context.Context, kbID, anomalyID string) (string, error) {
				item, err := anomalies.Resolve(ctx, kbID, anomalyID, "apply", "")
				if err != nil {
					return "", err
				}
				return item.Result, nil
			})
		default:
			logger.Warnf(ctx, "Unknown tool: %s", toolName)
		}

		if toolToRegister != nil {
			if toolToRegister.Name() != toolName {
				logger.Warnf(ctx, "Tool name mismatch: expected %s, got %s", toolName, toolToRegister.Name())
			}
			registry.RegisterTool(toolToRegister)
		}
	}

	logger.Infof(ctx, "Registered %d tools", len(registry.ListTools()))
	return nil
}

// ValidateConfig validates the agent configuration
func (s *agentService) ValidateConfig(config *types.AgentConfig) error {
	if config == nil {
		return fmt.Errorf("config cannot be nil")
	}

	if config.MaxIterations < 0 {
		config.MaxIterations = types.UnlimitedMaxIterations
	} else if config.MaxIterations == 0 {
		config.MaxIterations = 5
	} else if config.MaxIterations > MAX_ITERATIONS {
		return fmt.Errorf("max iterations too high: %d (max %d)", config.MaxIterations, MAX_ITERATIONS)
	}

	return nil
}

func (s *agentService) getKnowledgeBaseInfos(ctx context.Context, kbIDs []string) ([]*agent.KnowledgeBaseInfo, error) {
	if len(kbIDs) == 0 {
		return []*agent.KnowledgeBaseInfo{}, nil
	}

	kbInfos := make([]*agent.KnowledgeBaseInfo, 0, len(kbIDs))

	for _, kbID := range kbIDs {
		// Get knowledge base details
		kb, err := s.knowledgeBaseService.GetKnowledgeBaseByID(ctx, kbID)
		if err != nil {
			logger.Warnf(ctx, "Failed to get knowledge base %s: %v", secutils.SanitizeForLog(kbID), err)
			kbInfos = append(kbInfos, &agent.KnowledgeBaseInfo{
				ID:          kbID,
				Name:        kbID,
				Type:        "document",
				Description: "",
				DocCount:    0,
				RecentDocs:  []agent.RecentDocInfo{},
			})
			continue
		}

		// Skip hidden/system-managed knowledge bases (e.g., __chat_history__)
		if kb.IsTemporary {
			logger.Debugf(ctx, "Skipping temporary knowledge base %s (%s) from prompt", kb.ID, kb.Name)
			continue
		}

		metaCtx := ctx

		// Get document count and recent documents
		docCount := 0
		recentDocs := []agent.RecentDocInfo{}

		{
			pageResult, err := s.knowledgeService.ListPagedKnowledgeByKnowledgeBaseID(metaCtx, kbID, &types.Pagination{
				Page:     1,
				PageSize: 10,
			}, types.KnowledgeListFilter{
				ParseStatus: types.ParseStatusCompleted,
			})

			if err == nil && pageResult != nil {
				docCount = int(pageResult.Total)

				// Convert to Knowledge slice
				if knowledges, ok := pageResult.Data.([]*types.Knowledge); ok {
					for _, k := range knowledges {
						if len(recentDocs) >= 10 {
							break
						}
						recentDocs = append(recentDocs, agent.RecentDocInfo{
							KnowledgeID: k.ID,
							Title:       k.Title,
							Description: k.Description,
							FileName:    k.FileName,
							Type:        k.FileType,
							CreatedAt:   k.CreatedAt.Format("2006-01-02"),
							FileSize:    k.FileSize,
						})
					}
				}
			}
		}

		kbType := kb.Type
		if kbType == "" {
			kbType = "document" // Default type
		}
		kbInfos = append(kbInfos, &agent.KnowledgeBaseInfo{
			ID:           kb.ID,
			Name:         kb.Name,
			Type:         kbType,
			Description:  kb.Description,
			DocCount:     docCount,
			Capabilities: kbRetrievalCapabilities(kb),
			RecentDocs:   recentDocs,
		})
	}

	return kbInfos, nil
}

// kbRetrievalCapabilities reports which retrieval surfaces a KB exposes.
// Surfaces are the static facts the hybrid agent prompt consults to pick its
// retrieval strategy — the agent should NOT need to probe this via search.
//
// Returned values contain "chunks" when retrieval indexing is enabled:
//   - "chunks" → the KB has vector and/or keyword (BM25) indexing enabled
//     (knowledge_search / grep_chunks)
func kbRetrievalCapabilities(kb *types.KnowledgeBase) []string {
	if kb == nil {
		return nil
	}
	caps := make([]string, 0, 2)
	if kb.IsVectorEnabled() || kb.IsKeywordEnabled() {
		caps = append(caps, "chunks")
	}
	return caps
}

// getSelectedDocumentInfos retrieves detailed information for user-selected documents (via @ mention)
// This loads the actual content of the documents to include in the system prompt
func (s *agentService) getSelectedDocumentInfos(ctx context.Context, knowledgeIDs []string) ([]*agent.SelectedDocumentInfo, error) {
	if len(knowledgeIDs) == 0 {
		return []*agent.SelectedDocumentInfo{}, nil
	}

	// Get tenant ID from context
	tenantID := uint64(0)
	if tid, ok := types.TenantIDFromContext(ctx); ok {
		tenantID = tid
	}

	knowledges, err := s.knowledgeService.GetKnowledgeBatch(ctx, tenantID, knowledgeIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to get knowledge batch: %w", err)
	}

	// Build map for quick lookup
	knowledgeMap := make(map[string]*types.Knowledge)
	for _, k := range knowledges {
		if k != nil {
			knowledgeMap[k.ID] = k
		}
	}

	selectedDocs := make([]*agent.SelectedDocumentInfo, 0, len(knowledgeIDs))

	for _, kid := range knowledgeIDs {
		k, ok := knowledgeMap[kid]
		if !ok {
			logger.Warnf(ctx, "Selected knowledge %s not found", kid)
			continue
		}

		docInfo := &agent.SelectedDocumentInfo{
			KnowledgeID:     k.ID,
			KnowledgeBaseID: k.KnowledgeBaseID,
			Title:           k.Title,
			FileName:        k.FileName,
			FileType:        k.FileType,
		}

		selectedDocs = append(selectedDocs, docInfo)
	}

	logger.Infof(ctx, "Loaded %d selected documents metadata for prompt", len(selectedDocs))
	return selectedDocs, nil
}

func (s *agentService) resolvePinnedMCPServiceInfos(
	ctx context.Context,
	config *types.AgentConfig,
) []*agent.PinnedMCPServiceInfo {
	if len(config.PinnedMCPServiceIDs) == 0 || s.mcpServiceService == nil {
		return nil
	}
	tenantID := uint64(0)
	if tid, ok := types.TenantIDFromContext(ctx); ok {
		tenantID = tid
	}
	if tenantID == 0 {
		return fallbackPinnedMCPInfos(config.PinnedMCPServiceIDs)
	}

	services, err := s.mcpServiceService.ListMCPServicesByIDs(ctx, tenantID, config.PinnedMCPServiceIDs)
	if err != nil {
		logger.Warnf(ctx, "Failed to resolve pinned MCP services: %v", err)
		return fallbackPinnedMCPInfos(config.PinnedMCPServiceIDs)
	}
	byID := make(map[string]*types.MCPService, len(services))
	for _, svc := range services {
		if svc != nil {
			byID[svc.ID] = svc
		}
	}
	result := make([]*agent.PinnedMCPServiceInfo, 0, len(config.PinnedMCPServiceIDs))
	for _, id := range config.PinnedMCPServiceIDs {
		if id == "" {
			continue
		}
		if svc, ok := byID[id]; ok {
			result = append(result, &agent.PinnedMCPServiceInfo{
				ID:          svc.ID,
				Name:        svc.Name,
				Description: svc.Description,
			})
			continue
		}
		result = append(result, &agent.PinnedMCPServiceInfo{ID: id, Name: id})
	}
	return result
}

func (s *agentService) attachPinnedMCPToolNames(
	registry *tools.ToolRegistry,
	pinned []*agent.PinnedMCPServiceInfo,
) {
	if registry == nil || len(pinned) == 0 {
		return
	}
	byService := tools.MCPToolNamesByServiceID(registry)
	for _, info := range pinned {
		if info == nil || info.ID == "" {
			continue
		}
		info.ToolNames = append([]string(nil), byService[info.ID]...)
	}
}

func fallbackPinnedMCPInfos(ids []string) []*agent.PinnedMCPServiceInfo {
	result := make([]*agent.PinnedMCPServiceInfo, 0, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		result = append(result, &agent.PinnedMCPServiceInfo{ID: id, Name: id})
	}
	return result
}
