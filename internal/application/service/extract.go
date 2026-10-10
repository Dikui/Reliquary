package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	_ "github.com/duckdb/duckdb-go/v2"
	"reliquary/internal/infrastructure/datatable"
	"reliquary/internal/logger"
	"reliquary/internal/models/chat"
	"reliquary/internal/types"
)

const (
	// tableDescriptionPromptTemplate is the prompt template for generating table descriptions
	tableDescriptionPromptTemplate = `You are a data analysis expert. Based on the following table structure information and data samples, generate a concise table metadata description (200-300 words).

Table name: %s

%s

%s

Please describe the table from the following dimensions:
1. **Data Subject**: What type of data does this table record? (e.g., user information, sales records, log data, etc.)
2. **Core Fields**: List 3-5 most important fields and their meanings
3. **Data Scale**: Total number of rows and columns
4. **Business Scenarios**: What business analysis or application scenarios might this table be used for?
5. **Key Characteristics**: What notable features does the data have? (e.g., contains geographic locations, has category labels, has hierarchical relationships, etc.)

**Important Notes**:
- Do not output specific data values or sample content
- Use general descriptions so users can quickly determine if this table contains the information they need
- Use concise and professional language for easy retrieval and understanding
- Write the description in the same language as the data content`

	// columnDescriptionsPromptTemplate is the prompt template for generating column descriptions
	columnDescriptionsPromptTemplate = `You are a data analysis expert. Based on the following table structure information and data samples, generate structured description information for each column.

Table name: %s

%s

%s

Please generate a detailed description for each column, including the following information:
1. **Field Meaning**: What information does this column store? (e.g., user ID, order amount, creation time, etc.)
2. **Data Type**: The type and format of the data (e.g., integer, string, datetime, boolean, etc.)
3. **Business Purpose**: The role of this field in business (e.g., for user identification, amount calculation, time sorting, etc.)
4. **Data Characteristics**: Notable features of the data (e.g., unique identifier, nullable, has enum values, has units, etc.)

Please output in the following format (one paragraph per column):

**Column1** (data type)
- Field Meaning: xxx
- Business Purpose: xxx
- Data Characteristics: xxx

**Column2** (data type)
- Field Meaning: xxx
- Business Purpose: xxx
- Data Characteristics: xxx

**Important Notes**:
- Do not output specific data values, only describe the field metadata
- Use clear business terms for easy user understanding and search
- If enum value ranges can be inferred from sample data, provide a summary (e.g., status field contains pending/in-progress/completed states)
- Write descriptions in the same language as the data content`
)

// DataTableSummaryService retains spreadsheet descriptions as parser input.
// It does not create article summaries, chunks or separate queue jobs.
type DataTableSummaryService struct{}

func (s *knowledgeService) prepareTableProcessingBody(ctx context.Context, kb *types.KnowledgeBase, k *types.Knowledge) (string, error) {
	db, err := sql.Open("duckdb", ":memory:")
	if err != nil {
		return "", fmt.Errorf("open table parser: %w", err)
	}
	defer db.Close()
	loader := datatable.NewLoader(s.resolveFileServiceForPath(ctx, kb, k.FilePath), db, "article_table_"+k.ID)
	defer loader.Cleanup(ctx)
	schema, err := loader.LoadFromKnowledge(ctx, k)
	if err != nil {
		return "", fmt.Errorf("parse data table: %w", err)
	}
	rows, err := loader.SampleRows(ctx, schema, 10)
	if err != nil {
		return "", err
	}
	model, err := GetTierChatModel(ctx, s.db, s.modelService, kb, types.ModelTierGeneration)
	if err != nil {
		return "", err
	}
	helper := &DataTableSummaryService{}
	overrides, _ := k.ProcessOverrides()
	instructions := ResolveProcessConfig(kb, overrides).ChunkingConfig.TableMetadataInstructions
	sample := helper.buildSampleDataDescription(ctx, rows, 10)
	ctx = types.WithLLMCallMetadata(ctx, "table_metadata", "")
	table, err := helper.generateTableDescription(ctx, model, schema.TableName, schema.Description(), sample, instructions)
	if err != nil {
		return "", err
	}
	columns, err := helper.generateColumnDescriptions(ctx, model, schema.TableName, schema.Description(), sample, instructions)
	if err != nil {
		return "", err
	}
	return table + "\n\n" + columns, nil
}

func (s *DataTableSummaryService) generateTableDescription(ctx context.Context, chatModel chat.Chat,
	tableName, schemaDesc, sampleDesc, customInstructions string,
) (string, error) {
	prompt := fmt.Sprintf(tableDescriptionPromptTemplate, tableName, schemaDesc, sampleDesc)
	prompt = types.AppendCustomPromptInstructions(prompt, customInstructions, "table_metadata")

	thinking := false
	response, err := chatModel.Chat(ctx, []chat.Message{
		{Role: "user", Content: prompt},
	}, &chat.ChatOptions{
		Temperature: 0.3,
		MaxTokens:   512,
		Thinking:    &thinking,
	})
	if err != nil {
		return "", fmt.Errorf("failed to generate table description: %w", err)
	}

	return fmt.Sprintf("# Table Summary\n\nTable name: %s\n\n%s", tableName, response.Content), nil
}

// generateColumnDescriptions generates descriptions for each column in batch
func (s *DataTableSummaryService) generateColumnDescriptions(ctx context.Context, chatModel chat.Chat,
	tableName, schemaDesc, sampleDesc, customInstructions string,
) (string, error) {
	// Build batch prompt for all columns
	prompt := fmt.Sprintf(columnDescriptionsPromptTemplate, tableName, schemaDesc, sampleDesc)
	prompt = types.AppendCustomPromptInstructions(prompt, customInstructions, "table_metadata")

	// Call LLM once for all columns
	thinking := false
	response, err := chatModel.Chat(ctx, []chat.Message{
		{Role: "user", Content: prompt},
	}, &chat.ChatOptions{
		Temperature: 0.3,
		MaxTokens:   2048,
		Thinking:    &thinking,
	})
	if err != nil {
		return "", fmt.Errorf("failed to generate column descriptions: %w", err)
	}

	return fmt.Sprintf("# Table Column Information\n\nTable name: %s\n\n%s", tableName, response.Content), nil
}

// buildSampleDataDescription builds a formatted sample data description
func (s *DataTableSummaryService) buildSampleDataDescription(ctx context.Context, rawRows interface{}, maxRows int) string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("Sample data (first %d rows):\n", maxRows))

	var rows []interface{}
	switch typedRows := rawRows.(type) {
	case []map[string]string:
		rows = make([]interface{}, len(typedRows))
		for i, row := range typedRows {
			rows[i] = row
		}
	case []map[string]interface{}:
		rows = make([]interface{}, len(typedRows))
		for i, row := range typedRows {
			rows[i] = row
		}
	default:
		logger.Warnf(ctx, "[TableSummary] Unsupported sample rows type: %T", rawRows)
		return builder.String()
	}

	for i, row := range rows {
		if i >= maxRows {
			break
		}
		jsonBytes, err := json.Marshal(row)
		if err != nil {
			continue
		}
		builder.WriteString(string(jsonBytes))
		builder.WriteString("\n")
	}

	return builder.String()
}
