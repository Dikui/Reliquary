package service

import (
	"encoding/json"
	"reliquary/internal/types"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMarkdownOnlyProcessingNormalizesUnknownContentType(t *testing.T) {
	for _, value := range []any{nil, "", "uncertain", "不确定", "unknown"} {
		t.Run(stringValue(value), func(t *testing.T) {
			var payload map[string]any
			require.NoError(t, json.Unmarshal([]byte(validProcessingFixture("event")), &payload))
			if value == nil {
				delete(payload, "content_type")
			} else {
				payload["content_type"] = value
			}
			raw, err := json.Marshal(payload)
			require.NoError(t, err)
			result, err := parseArticleProcessingResult(string(raw))
			require.NoError(t, err)
			require.Equal(t, types.ContentTypeOther, libraryContentType(&types.Library{}, *result.ContentType))
		})
	}
}
func stringValue(v any) string {
	if v == nil {
		return "missing"
	}
	return v.(string)
}

func TestMarkdownOnlyEveryContentTypeKeepsFullBody(t *testing.T) {
	h, v := newIngestHarness(t)
	for _, contentType := range types.ContentTypes {
		t.Run(contentType, func(t *testing.T) {
			h.processor.output = validProcessingFixture(contentType)
			body := "# " + contentType + "\n\n" + strings.Repeat("Complete original body for "+contentType+". ", 20)
			result, err := h.service.Ingest(h.ctx, &types.IngestRequest{Library: "official", URL: "https://example.org/full-" + contentType, Content: body, Acquisition: "extracted", Publisher: "Fixture"})
			require.NoError(t, err)
			require.Equal(t, "accepted", result.Status)
			h.drain(t)
			article := h.article(t, result.EntryID)
			require.Equal(t, types.ParseStatusCompleted, article.ParseStatus)
			require.Equal(t, contentType, article.ContentType)
			saved, err := v.ReadBody(article)
			require.NoError(t, err)
			require.Equal(t, strings.TrimSpace(body), strings.TrimSpace(string(saved)))
			var chunks int64
			require.NoError(t, h.db.Model(&types.Chunk{}).Where("entry_id=?", article.ID).Count(&chunks).Error)
			require.Positive(t, chunks)
			var fields map[string]any
			encoded, err := json.Marshal(article)
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(encoded, &fields))
			for _, removed := range []string{"processing_mode", "processing_review_status", "content_type_uncertain", "vault_original_path", "ingest_attachments"} {
				require.NotContains(t, fields, removed)
			}
		})
	}
}

func TestMarkdownOnlyUnknownClassificationCompletes(t *testing.T) {
	h, vault := newIngestHarness(t)
	for _, value := range []string{"不确定", "missing"} {
		t.Run(value, func(t *testing.T) {
			var output map[string]any
			require.NoError(t, json.Unmarshal([]byte(validProcessingFixture("event")), &output))
			output["content_type_uncertain"] = true
			if value == "missing" {
				delete(output, "content_type")
			} else {
				output["content_type"] = value
			}
			raw, err := json.Marshal(output)
			require.NoError(t, err)
			h.processor.output = string(raw)
			body := "# " + value + "\n\n" + strings.Repeat(value+" complete source observations. ", 20)
			result, err := h.service.Ingest(h.ctx, &types.IngestRequest{Library: "official", URL: "https://example.org/fallback/" + value, Content: body, Acquisition: "extracted", Publisher: "Fixture"})
			require.NoError(t, err)
			require.Equal(t, "accepted", result.Status)
			h.drain(t)
			article := h.article(t, result.EntryID)
			require.Equal(t, types.ContentTypeOther, article.ContentType)
			require.Equal(t, types.ParseStatusCompleted, article.ParseStatus)
			require.Equal(t, "enabled", article.EnableStatus)
			saved, err := vault.ReadBody(article)
			require.NoError(t, err)
			require.Equal(t, strings.TrimSpace(body), strings.TrimSpace(string(saved)))
		})
	}
}
