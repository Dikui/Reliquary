package agent

import (
	"fmt"
	"strings"
	"time"

	"reliquary/internal/config"
	"reliquary/internal/types"
)

// formatFileSize formats file size in human-readable format
func formatFileSize(size int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)

	if size < KB {
		return fmt.Sprintf("%d B", size)
	} else if size < MB {
		return fmt.Sprintf("%.2f KB", float64(size)/KB)
	} else if size < GB {
		return fmt.Sprintf("%.2f MB", float64(size)/MB)
	}
	return fmt.Sprintf("%.2f GB", float64(size)/GB)
}

// formatDocSummary cleans and truncates document summaries for table display
func formatDocSummary(summary string, maxLen int) string {
	cleaned := strings.TrimSpace(summary)
	if cleaned == "" {
		return "-"
	}
	cleaned = strings.ReplaceAll(cleaned, "\n", " ")
	cleaned = strings.ReplaceAll(cleaned, "\r", " ")
	cleaned = strings.Join(strings.Fields(cleaned), " ")

	runes := []rune(cleaned)
	if len(runes) <= maxLen {
		return cleaned
	}
	return strings.TrimSpace(string(runes[:maxLen])) + "..."
}

// RecentDocInfo contains brief information about a recently added document
type RecentDocInfo struct {
	ChunkID         string
	KnowledgeBaseID string
	KnowledgeID     string
	Title           string
	Description     string
	FileName        string
	FileSize        int64
	Type            string
	CreatedAt       string // Formatted time string
}

// SelectedDocumentInfo contains summary information about a user-selected document (via @ mention).
// Injected into the user message runtime_context (pinned_documents); content is fetched via tools.
type SelectedDocumentInfo struct {
	KnowledgeID     string // Knowledge ID
	KnowledgeBaseID string // Knowledge base ID
	Title           string // Document title
	FileName        string // Original file name
	FileType        string // File type (pdf, docx, etc.)
}

// PinnedMCPServiceInfo describes an MCP service explicitly @mentioned for this turn.
type PinnedMCPServiceInfo struct {
	ID          string
	Name        string
	Description string
	ToolNames   []string // Registered tool.function names for this service (mcp_{service}_{tool})
}

// KnowledgeBaseInfo contains essential information about a knowledge base for agent prompt
type KnowledgeBaseInfo struct {
	ID          string
	Name        string
	Type        string // Knowledge base type: "document" or "messages-history"
	Description string
	DocCount    int
	// Capabilities lists the retrieval surfaces this KB exposes. Any subset of
	// {"chunks"}. "chunks" is present when the KB has vector and/or
	// keyword (BM25) indexing enabled. This is the *deterministic* source of
	// truth the agent should consult before picking a retrieval strategy —
	// significantly more reliable than running probing searches.
	Capabilities []string
	RecentDocs   []RecentDocInfo // Recently added documents (up to 10)
}

// PlaceholderDefinition defines a placeholder exposed to UI/configuration
// Deprecated: Use types.PromptPlaceholder instead
type PlaceholderDefinition struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

// AvailablePlaceholders lists all supported prompt placeholders for UI hints
// This returns agent mode specific placeholders
func AvailablePlaceholders() []PlaceholderDefinition {
	// Use centralized placeholder definitions from types package
	placeholders := types.PlaceholdersByField(types.PromptFieldAgentSystemPrompt)
	result := make([]PlaceholderDefinition, len(placeholders))
	for i, p := range placeholders {
		result[i] = PlaceholderDefinition{
			Name:        p.Name,
			Label:       p.Label,
			Description: p.Description,
		}
	}
	return result
}

// formatKnowledgeBaseList formats knowledge base information as XML for the prompt
func formatKnowledgeBaseList(kbInfos []*KnowledgeBaseInfo) string {
	if len(kbInfos) == 0 {
		return "<knowledge_bases />"
	}

	var b strings.Builder
	b.WriteString("<knowledge_bases>\n")
	for _, kb := range kbInfos {
		kbType := kb.Type
		if kbType == "" {
			kbType = "document"
		}
		capsAttr := ""
		if len(kb.Capabilities) > 0 {
			capsAttr = fmt.Sprintf(" capabilities=\"%s\"", strings.Join(kb.Capabilities, ","))
		}
		b.WriteString(fmt.Sprintf("<knowledge_base id=\"%s\" name=\"%s\" type=\"%s\" doc_count=\"%d\"%s>\n",
			kb.ID, kb.Name, kbType, kb.DocCount, capsAttr))
		if kb.Description != "" {
			b.WriteString(fmt.Sprintf("<description>%s</description>\n", kb.Description))
		}

		if len(kb.RecentDocs) > 0 {

			b.WriteString("<recent_documents>\n")
			for j, doc := range kb.RecentDocs {
				if j >= 2 {
					break
				}
				docName := doc.Title
				if docName == "" {
					docName = doc.FileName
				}
				fileSize := formatFileSize(doc.FileSize)
				b.WriteString(fmt.Sprintf("<document knowledge_id=\"%s\" type=\"%s\" file_size=\"%s\" created_at=\"%s\">\n",
					doc.KnowledgeID, doc.Type, fileSize, doc.CreatedAt))
				b.WriteString(fmt.Sprintf("<name>%s</name>\n", docName))
				if doc.Description != "" {
					summary := formatDocSummary(doc.Description, 120)
					b.WriteString(fmt.Sprintf("<summary>%s</summary>\n", summary))
				}
				b.WriteString("</document>\n")
			}
			b.WriteString("</recent_documents>\n")

		}
		b.WriteString("</knowledge_base>\n")
	}
	b.WriteString("</knowledge_bases>")
	return b.String()
}

// renderPromptPlaceholders renders placeholders in the prompt template.
//
// Supported placeholders:
//   - {{knowledge_bases}} - Historically expanded to the full bound-KB XML
//     block. Since that block now lives in the user message's
//     `<runtime_context>` (see observe.buildRuntimeContextBlock), the
//     placeholder is expanded to a short pointer so legacy / custom
//     templates that still reference `{{knowledge_bases}}` degrade
//     gracefully instead of dumping the detail twice.
//   - `<must_use>` is NOT a placeholder — when the user @mentions MCP,
//     observe.buildMustUseBlock injects it as a sibling block in the user
//     message; system prompts document it by convention (see agent_system_prompt.yaml).
func renderPromptPlaceholders(template string, knowledgeBases []*KnowledgeBaseInfo) string {
	result := template

	if strings.Contains(result, "{{knowledge_bases}}") {
		var replacement string
		if len(knowledgeBases) == 0 {
			replacement = "(no knowledge bases bound to this session)"
		} else {
			replacement = "(see `<bound_knowledge_bases>` inside the user message's `<runtime_context>` for the current bound KB list and their capabilities)"
		}
		result = strings.ReplaceAll(result, "{{knowledge_bases}}", replacement)
	}

	return result
}

// renderRuntimePromptPlaceholders renders runtime prompt values
// Supported placeholders:
//   - {{knowledge_bases}}
//   - {{current_time}} -> current time string
//   - {{language}} -> user language name (e.g. "Chinese (Simplified)", "English")
func renderRuntimePromptPlaceholders(
	template string,
	knowledgeBases []*KnowledgeBaseInfo,
	currentTime string,
	language string,
) string {
	// Knowledge bases need special formatting, so handle it first
	result := renderPromptPlaceholders(template, knowledgeBases)

	result = types.RenderPromptPlaceholders(result, types.PlaceholderValues{
		"current_time": currentTime,
		"language":     language,
	})
	return result
}

// BuildSystemPromptOptions contains optional parameters for BuildSystemPrompt
type BuildSystemPromptOptions struct {
	Language string         // User language name for {{language}} placeholder (e.g. "Chinese (Simplified)")
	Config   *config.Config // Config for reading prompt templates; nil falls back to hardcoded defaults
}

// BuildSystemPrompt builds the progressive RAG system prompt
// The prompt combines the template with current knowledge base context.
func BuildSystemPrompt(
	knowledgeBases []*KnowledgeBaseInfo,
	systemPromptTemplate ...string,
) string {
	return BuildSystemPromptWithOptions(knowledgeBases, nil, systemPromptTemplate...)
}

// BuildSystemPromptWithOptions builds the system prompt with additional prompt options
func BuildSystemPromptWithOptions(
	knowledgeBases []*KnowledgeBaseInfo,
	options *BuildSystemPromptOptions,
	systemPromptTemplate ...string,
) string {
	var basePrompt string
	var template string

	// Determine template to use
	if len(systemPromptTemplate) > 0 && systemPromptTemplate[0] != "" {
		template = systemPromptTemplate[0]
	} else if len(knowledgeBases) == 0 {
		var cfg *config.Config
		if options != nil {
			cfg = options.Config
		}
		template = GetPureAgentSystemPrompt(cfg)
	} else {
		var cfg *config.Config
		if options != nil {
			cfg = options.Config
		}
		template = GetProgressiveRAGSystemPrompt(cfg)
	}

	currentTime := time.Now().Format("2006-01-02")
	language := ""
	if options != nil {
		language = options.Language
	}
	basePrompt = renderRuntimePromptPlaceholders(template, knowledgeBases, currentTime, language)

	if len(knowledgeBases) > 0 {
		basePrompt += "\n\nFor knowledge-backed answers, compare relevant retrieved messages and cite the source of each conclusion. After feasible retrieval, deliver supported findings even if evidence remains incomplete; identify missing evidence and conflicting sources, and do not state unsupported inferences as facts. Pending-review material is not formal evidence."
	}

	return basePrompt
}

// GetPureAgentSystemPrompt returns the Pure Agent system prompt from config templates.
// The template must be defined in config/prompt_templates/agent_system_prompt.yaml
// with mode "pure". Returns empty string if config is nil or template not found.
func GetPureAgentSystemPrompt(cfg *config.Config) string {
	if cfg != nil && cfg.PromptTemplates != nil {
		if t := config.DefaultTemplateByMode(cfg.PromptTemplates.AgentSystemPrompt, "pure"); t != nil && t.Content != "" {
			return t.Content
		}
	}
	return ""
}

// GetProgressiveRAGSystemPrompt returns the Progressive RAG Agent system prompt from config templates.
// The template must be defined in config/prompt_templates/agent_system_prompt.yaml
// with mode "rag". Returns empty string if config is nil or template not found.
func GetProgressiveRAGSystemPrompt(cfg *config.Config) string {
	if cfg != nil && cfg.PromptTemplates != nil {
		if t := config.DefaultTemplateByMode(cfg.PromptTemplates.AgentSystemPrompt, "rag"); t != nil && t.Content != "" {
			return t.Content
		}
	}
	return ""
}
