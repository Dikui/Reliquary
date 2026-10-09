package tools

// maxFunctionNameLength is the maximum length for a tool/function name
// imposed by the OpenAI API.
const maxFunctionNameLength = 64

// Tool names constants
const (
	ToolThinking               = "thinking"
	ToolTodoWrite              = "todo_write"
	ToolGrepChunks             = "grep_chunks"
	ToolKnowledgeSearch        = "knowledge_search"
	ToolListKnowledgeChunks    = "list_knowledge_chunks"
	ToolQueryKnowledgeContext  = "query_knowledge_context"
	ToolGetDocumentInfo        = "get_document_info"
	ToolDatabaseQuery          = "database_query"
	ToolDataAnalysis           = "data_analysis"
	ToolDataSchema             = "data_schema"
	ToolWebFetch               = "web_fetch"
	ToolKnowledgeReportAnomaly = "knowledge_report_anomaly"
	ToolKnowledgeApplyAnomaly  = "knowledge_apply_anomaly"
	ToolKnowledgeImportURL     = "knowledge_import_url"
)

// AvailableTool defines a simple tool metadata used by settings APIs.
type AvailableTool struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

// AvailableToolDefinitions returns the list of tools exposed to the UI.
// Keep this in sync with registered tools in this package.
func AvailableToolDefinitions() []AvailableTool {
	return []AvailableTool{
		{Name: ToolThinking, Label: "思考", Description: "动态和反思性的问题解决思考工具"},
		{Name: ToolTodoWrite, Label: "制定计划", Description: "创建结构化的研究计划"},
		{Name: ToolGrepChunks, Label: "关键词搜索", Description: "快速定位包含特定关键词的文档和分块"},
		{Name: ToolKnowledgeSearch, Label: "语义搜索", Description: "理解问题并查找语义相关内容"},
		{Name: ToolListKnowledgeChunks, Label: "查看文档分块", Description: "获取文档完整分块内容"},
		{Name: ToolQueryKnowledgeContext, Label: "查询知识上下文", Description: "查询对象、事实、关系和事件"},
		{Name: ToolGetDocumentInfo, Label: "获取文档信息", Description: "查看文档元数据"},
		{Name: ToolDatabaseQuery, Label: "查询数据库", Description: "查询数据库中的信息"},
		{Name: ToolDataAnalysis, Label: "数据分析", Description: "理解数据文件并进行数据分析"},
		{Name: ToolWebFetch, Label: "抓取网页", Description: "抓取指定网址的正文并分析内容"},
		{Name: ToolDataSchema, Label: "查看数据元信息", Description: "获取表格文件的元信息"},
		{Name: ToolKnowledgeReportAnomaly, Label: "报告消息异常", Description: "提交消息纠错或标签建议，等待知识库本人处理"},
		{Name: ToolKnowledgeImportURL, Label: "按网址收录", Description: "抓取原文并进入统一质量审核，审核前不作为正式消息"},
		{Name: ToolKnowledgeApplyAnomaly, Label: "执行消息纠错", Description: "仅在知识库本人明确指示后执行指定纠错建议"},
	}
}

// DefaultAllowedTools returns the default allowed tools list.
func DefaultAllowedTools() []string {
	return []string{
		ToolThinking,
		ToolTodoWrite,
		ToolKnowledgeSearch,
		ToolGrepChunks,
		ToolListKnowledgeChunks,
		ToolQueryKnowledgeContext,
		ToolGetDocumentInfo,
		ToolKnowledgeReportAnomaly,
		ToolKnowledgeImportURL,
		ToolDatabaseQuery,
		ToolDataAnalysis,
		ToolDataSchema,
		ToolWebFetch,
	}
}

// CanonicalToolName accepts historical tool names while advertising only current names.
func CanonicalToolName(name string) string {
	if name == "query_knowledge_graph" {
		return ToolQueryKnowledgeContext
	}
	return name
}
