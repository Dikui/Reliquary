package router

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reliquary/internal/models/chat"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type resumeHTTPChat struct {
	mcpFixtureChat
	calls      map[int]int
	fail       int
	candidates map[int][]string
}

func (m *resumeHTTPChat) Chat(ctx context.Context, messages []chat.Message, options *chat.ChatOptions) (*types.ChatResponse, error) {
	stage, _ := types.LLMCallMetadataFromContext(ctx)
	if stage != "entry_understanding" {
		return m.mcpFixtureChat.Chat(ctx, messages, options)
	}
	var input struct {
		Segment struct {
			Number int `json:"number"`
		} `json:"segment"`
		Topics []struct {
			ID string `json:"id"`
		} `json:"existing_topics"`
	}
	if err := json.Unmarshal([]byte(messages[len(messages)-1].Content), &input); err != nil {
		return nil, err
	}
	n := input.Segment.Number
	m.calls[n]++
	if n == m.fail {
		return nil, fmt.Errorf("fixture failure at segment %d", n)
	}
	response, err := m.mcpFixtureChat.Chat(ctx, messages, options)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(response.Content), &result); err != nil {
		return nil, err
	}
	ids := []string{}
	result["topics"] = ids
	encoded, err := json.Marshal(result)
	response.Content = string(encoded)
	return response, err
}

func TestHTTPProcessingEntrypointsResumeFailedAndCancelledSegments(t *testing.T) {
	for _, endpoint := range []string{"reprocess", "reparse"} {
		for _, cancelled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/cancelled=%t", endpoint, cancelled), func(t *testing.T) {
				model := &resumeHTTPChat{fail: 4, calls: map[int]int{}, candidates: map[int][]string{}}
				f := newMCPFixture(t, mcpProcessingFixture{chat: model, maxBodyChars: 140})
				f.seedProcessingModels(t)
				router := ingestAuthHTTPRouter(f)
				body, _ := json.Marshal(map[string]any{"items": []any{map[string]any{"library": "personal", "title": "HTTP segmented original", "acquisition": "verbatim", "content": "# Measurements\n\n" + strings.Repeat("Complete original measurements. ", 30)}}})
				response := personalRequest(t, router, http.MethodPost, "/api/v1/ingest", string(body), f.key, true)
				require.Equal(t, http.StatusOK, response.Code, response.Body.String())
				var envelope struct{ Data types.IngestBatchResult }
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
				require.Equal(t, "accepted", envelope.Data.Items[0].Status)
				id := envelope.Data.Items[0].EntryID
				process := func() error {
					return f.articles.(interfaces.EntryService).ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1])
				}
				require.ErrorContains(t, process(), "第 4/")
				var entry types.Entry
				require.NoError(t, f.db.First(&entry, "id=?", id).Error)
				require.Equal(t, types.ParseStatusCompleted, entry.ParseStatus)
				require.Equal(t, types.SummaryStatusFailed, entry.SummaryStatus)
				var runID string
				require.NoError(t, f.db.Table("entry_segment_runs").Select("id").Where("entry_id=? AND phase='processing'", id).Scan(&runID).Error)
				require.NotEmpty(t, runID)
				// A live topic catalog update must not invalidate completed document work.
				request := func(path string) {
					response := personalRequest(t, router, http.MethodPost, "/api/v1/entries/"+id+"/"+path, `{}`, f.key, true)
					require.Equal(t, http.StatusOK, response.Code, response.Body.String())
				}
				if cancelled {
					request(endpoint)
					request("cancel-parse")
				}
				model.fail = 0
				request(endpoint)
				require.NoError(t, process())
				require.NoError(t, f.db.First(&entry, "id=?", id).Error)
				require.Equal(t, types.ParseStatusCompleted, entry.ParseStatus, entry.ErrorMessage)
				for n := 1; n <= 3; n++ {
					require.Equal(t, 1, model.calls[n], "completed segment %d repeated", n)
				}
				require.Equal(t, 2, model.calls[4])
				var runs int64
				require.NoError(t, f.db.Table("entry_segment_runs").Where("entry_id=? AND phase='processing'", id).Count(&runs).Error)
				require.EqualValues(t, 1, runs)
				var status string
				require.NoError(t, f.db.Table("entry_segment_runs").Select("status").Where("id=?", runID).Scan(&status).Error)
				require.Equal(t, "completed", status)
				// Explicit processing of an already completed document starts a fresh run.
				request(endpoint)
				require.NoError(t, process())
				require.Equal(t, 2, model.calls[1])
				require.NoError(t, f.db.Table("entry_segment_runs").Where("entry_id=? AND phase='processing'", id).Count(&runs).Error)
				require.EqualValues(t, 2, runs)
			})
		}
	}
}
