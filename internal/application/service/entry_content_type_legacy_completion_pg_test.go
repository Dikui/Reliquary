package service

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

func TestLegacySummaryCorrectionBackfillsOriginalLinkForSameArticleCompletion(t *testing.T) {
	for _, channel := range []string{types.ChannelRSS, types.ChannelWeb} {
		for _, location := range []string{"source", "intake", "metadata", "metadata_with_intake"} {
			t.Run(channel+"_"+location, func(t *testing.T) {
				h, vault := newIngestHarness(t)
				transport := forbidIngestNetwork(t, h)
				originalURL := "https://example.org/legacy-correction-" + channel + "-" + location + "?utm_source=old"
				article := &types.Entry{ID: uuid.NewString(), SpaceID: h.library.SpaceID, LibraryID: h.library.ID,
					SourceKind: "article", Type: types.EntryTypeManual, Channel: channel, Source: types.EntryTypeManual,
					Title: "Legacy summary", ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled",
					ContentType: types.ContentTypeTutorial, ProcessingMode: types.ProcessingModeSummaryOnly,
					CreatedAt: time.Now(), UpdatedAt: time.Now(), Metadata: types.JSON(`{"publisher":"Legacy Journal","datasource_id":"retired-source"}`)}
				switch location {
				case "source":
					article.Type = "url"
					article.Source = originalURL
				case "intake":
					require.NoError(t, article.SetIntakeMetadata(&types.EntryIntakeMetadata{Source: "manual", Decision: "approved", SourceInfo: types.SourceInfo{OriginalURL: originalURL, Publisher: "Legacy Journal", Author: "Legacy Author"}}))
				case "metadata", "metadata_with_intake":
					article.Metadata = types.JSON(`{"publisher":"Legacy Journal","datasource_id":"retired-source","original_url":"` + originalURL + `"}`)
					if location == "metadata_with_intake" {
						require.NoError(t, article.SetIntakeMetadata(&types.EntryIntakeMetadata{Source: "manual", Decision: "approved", SourceInfo: types.SourceInfo{Publisher: "Legacy Journal", Author: "Legacy Author"}}))
					}
				}
				require.NoError(t, h.db.Create(article).Error)
				require.NoError(t, vault.WriteArticle(h.ctx, article, "", nil))
				beforeTasks, beforeModels := len(h.queue.tasks), len(h.processor.requests)
				corrected, err := h.service.CorrectContentType(h.ctx, article.ID, types.ContentTypeResearch, false)
				require.NoError(t, err)
				require.Equal(t, normalizeMessageURL(originalURL), corrected.NormalizedURL)
				require.Equal(t, channel, corrected.Channel)
				require.Equal(t, article.Source, corrected.Source)
				require.Empty(t, corrected.Acquisition, "correction must preserve the historical acquisition value")
				require.True(t, corrected.Incomplete)
				require.Equal(t, types.ProcessingModeFull, corrected.ProcessingMode)
				require.Len(t, corrected.IngestMissing(), 1)
				require.Equal(t, originalURL, corrected.IngestMissing()[0].SourceURL)
				require.Equal(t, "修正为全文保存，需要正文", corrected.IngestMissing()[0].Reason)
				require.Len(t, h.queue.tasks, beforeTasks)
				require.Len(t, h.processor.requests, beforeModels)
				checks, err := h.service.CheckURLs(h.ctx, []string{originalURL})
				require.NoError(t, err)
				require.Equal(t, article.ID, checks[0].EntryID)
				content := "# Original research\n\nThe journal publishes full instrument settings, measurements and research observations.\n"
				result, err := h.service.Ingest(h.ctx, &types.IngestRequest{URL: originalURL, Publisher: "Legacy Journal", Acquisition: "verbatim", Content: content})
				require.NoError(t, err)
				require.Equal(t, article.ID, result.EntryID)
				h.drain(t)
				completed := h.article(t, article.ID)
				require.Equal(t, types.ParseStatusCompleted, completed.ParseStatus)
				require.Equal(t, types.ContentTypeResearch, completed.ContentType)
				require.False(t, completed.Incomplete)
				require.Equal(t, content, completed.VaultBody)
				require.Equal(t, channel, completed.Channel)
				require.Equal(t, "retired-source", completed.GetMetadata()["datasource_id"])
				intake, err := completed.IntakeMetadata()
				require.NoError(t, err)
				require.Equal(t, originalURL, intake.SourceInfo.OriginalURL)
				if location == "intake" || location == "metadata_with_intake" {
					require.Equal(t, "Legacy Author", intake.SourceInfo.Author)
				}
				require.True(t, strings.HasSuffix(completed.VaultPath, ".md"))
				var count int64
				require.NoError(t, h.db.Model(&types.Entry{}).Count(&count).Error)
				require.EqualValues(t, 1, count)
				require.Zero(t, transport.calls.Load())
			})
		}
	}
}
