package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

var getDocumentInfoTool = BaseTool{
	name:        ToolGetDocumentInfo,
	description: "Retrieve detailed metadata and processing status for documents. Use knowledge_ids from retrieval results.",
	schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "knowledge_ids": {
      "type": "array",
      "items": {
        "type": "string"
      },
      "description": "Document IDs from retrieval results"
    }
  },
  "required": [
    "knowledge_ids"
  ]
}`),
}

// GetDocumentInfoInput defines the input parameters for get document info tool.
type GetDocumentInfoInput struct {
	KnowledgeIDs []string `json:"knowledge_ids,omitempty"`
}

// GetDocumentInfoTool retrieves detailed information about a document/knowledge
type GetDocumentInfoTool struct {
	BaseTool
	knowledgeService interfaces.KnowledgeService
	chunkService     interfaces.ChunkService
	searchTargets    types.SearchTargets // Pre-computed unified search targets with KB-tenant mapping
}

// NewGetDocumentInfoTool creates a new get document info tool
func NewGetDocumentInfoTool(
	knowledgeService interfaces.KnowledgeService,
	chunkService interfaces.ChunkService,
	searchTargets types.SearchTargets,
) *GetDocumentInfoTool {
	return &GetDocumentInfoTool{
		BaseTool:         getDocumentInfoTool,
		knowledgeService: knowledgeService,
		chunkService:     chunkService,
		searchTargets:    searchTargets,
	}
}

// Execute retrieves document information with concurrent processing
func (t *GetDocumentInfoTool) Execute(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
	// Parse args from json.RawMessage
	var input GetDocumentInfoInput
	if err := json.Unmarshal(args, &input); err != nil {
		return &types.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("Failed to parse args: %v", err),
		}, err
	}

	knowledgeIDs := input.KnowledgeIDs
	if len(knowledgeIDs) == 0 {
		return &types.ToolResult{
			Success: false,
			Error:   "knowledge_ids is required (non-empty array)",
		}, fmt.Errorf("missing ids")
	}

	type docInfo struct {
		knowledge  *types.Knowledge
		chunk      *types.Chunk
		chunkCount int
		err        error
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	results := make(map[string]*docInfo)
	enabled := true

	for _, knowledgeID := range knowledgeIDs {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()

			knowledge, err := authorizeKnowledgeInSearchTargets(
				ctx, t.searchTargets, id, t.knowledgeService,
			)
			if err != nil {
				mu.Lock()
				results[id] = &docInfo{
					err: fmt.Errorf("failed to get document info: %v", err),
				}
				mu.Unlock()
				return
			}

			_, total, err := t.chunkService.GetRepository().
				ListPagedChunksByKnowledgeID(ctx,
					knowledge.TenantID,
					id,
					&types.Pagination{
						Page:     1,
						PageSize: 1,
					},
					[]types.ChunkType{types.ChunkTypeText},
					nil,
					"",
					"",
					&enabled)
			if err != nil {
				mu.Lock()
				results[id] = &docInfo{
					err: fmt.Errorf("failed to get document info: %v", err),
				}
				mu.Unlock()
				return
			}
			chunkCount := int(total)

			mu.Lock()
			results[id] = &docInfo{
				knowledge:  knowledge,
				chunkCount: chunkCount,
			}
			mu.Unlock()
		}(knowledgeID)
	}

	wg.Wait()

	requested := len(knowledgeIDs)
	successDocs := make([]*docInfo, 0)
	var errors []string

	for _, knowledgeID := range knowledgeIDs {
		result := results[knowledgeID]
		if result == nil {
			errors = append(errors, fmt.Sprintf("%s: not found", knowledgeID))
			continue
		}
		if result.err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", knowledgeID, result.err))
		} else if result.knowledge != nil {
			successDocs = append(successDocs, result)
		}
	}

	if len(successDocs) == 0 {
		return &types.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("Failed to retrieve any document info. Errors: %v", errors),
		}, fmt.Errorf("all document retrievals failed")
	}

	output := "=== Document Info ===\n\n"
	output += fmt.Sprintf("Successfully retrieved %d / %d entries\n\n", len(successDocs), requested)

	if len(errors) > 0 {
		output += "=== Partial Failures ===\n"
		for _, errMsg := range errors {
			output += fmt.Sprintf("  - %s\n", errMsg)
		}
		output += "\n"
	}

	formattedDocs := make([]map[string]interface{}, 0, len(successDocs))
	for i, doc := range successDocs {
		output += fmt.Sprintf("[Entry #%d]\n", i+1)

		k := doc.knowledge
		output += fmt.Sprintf("  ID:           %s\n", k.ID)
		output += fmt.Sprintf("  Title:        %s\n", k.Title)

		if k.Description != "" {
			output += fmt.Sprintf("  Description:  %s\n", k.Description)
		}

		output += fmt.Sprintf("  Source:       %s\n", formatSource(k.Type, k.Source))

		if k.FileName != "" {
			output += fmt.Sprintf("  File Name:    %s\n", k.FileName)
			output += fmt.Sprintf("  File Type:    %s\n", k.FileType)
			output += fmt.Sprintf("  File Size:    %s\n", formatFileSize(k.FileSize))
		}

		output += fmt.Sprintf("  Parse Status: %s\n", formatParseStatus(k.ParseStatus))
		output += fmt.Sprintf("  Chunk Count:  %d\n", doc.chunkCount)

		if k.Metadata != nil {
			if metadata, err := k.Metadata.Map(); err == nil && len(metadata) > 0 {
				output += "  Metadata:\n"
				for key, value := range metadata {
					output += fmt.Sprintf("    - %s: %v\n", key, value)
				}
			}
		}

		output += "\n"

		formattedDocs = append(formattedDocs, map[string]interface{}{
			"knowledge_id": k.ID,
			"title":        k.Title,
			"description":  k.Description,
			"type":         k.Type,
			"source":       k.Source,
			"file_name":    k.FileName,
			"file_type":    k.FileType,
			"file_size":    k.FileSize,
			"parse_status": k.ParseStatus,
			"chunk_count":  doc.chunkCount,
			"metadata":     k.GetMetadata(),
		})
	}

	var firstTitle string
	if len(formattedDocs) > 0 {
		if t, ok := formattedDocs[0]["title"].(string); ok {
			firstTitle = t
		}
	}

	return &types.ToolResult{
		Success: true,
		Output:  output,
		Data: map[string]interface{}{
			"documents":    formattedDocs,
			"total_docs":   len(successDocs),
			"requested":    requested,
			"errors":       errors,
			"display_type": "document_info",
			"title":        firstTitle,
		},
	}, nil
}

func formatSource(knowledgeType, source string) string {
	switch knowledgeType {
	case "file":
		return "File Upload"
	case "url":
		return fmt.Sprintf("URL: %s", source)
	case "passage":
		return "Text Input"
	default:
		return knowledgeType
	}
}

func formatFileSize(size int64) string {
	if size == 0 {
		return "Unknown"
	}
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(size)/float64(div), "KMGTPE"[exp])
}

func formatParseStatus(status string) string {
	switch status {
	case "pending":
		return "Pending"
	case "processing":
		return "Processing"
	case "completed", "success":
		return "Completed"
	case "failed":
		return "Failed"
	default:
		return status
	}
}
