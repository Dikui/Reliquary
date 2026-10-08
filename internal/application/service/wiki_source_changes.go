package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"reliquary/internal/agent"
	"reliquary/internal/models/chat"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

func (s *wikiPageService) IgnoreSourceChanges(ctx context.Context, kbID, slug string) (*types.WikiPage, error) {
	if err := s.repo.ClearSourceChanges(ctx, kbID, slug, nil); err != nil {
		return nil, err
	}
	return s.repo.GetBySlug(ctx, kbID, slug)
}

func (s *wikiPageService) ClearSourceChanges(ctx context.Context, kbID, slug string, changes types.WikiSourceChanges) error {
	return s.repo.ClearSourceChanges(ctx, kbID, slug, changes)
}

func (s *wikiPageService) MissingChunkRefs(ctx context.Context, tenantID uint64, kbID string, chunkIDs []string) ([]string, error) {
	if len(chunkIDs) == 0 || s.chunkRepo == nil {
		return nil, nil
	}
	chunks, err := s.chunkRepo.ListChunksByID(ctx, tenantID, chunkIDs)
	if err != nil {
		return nil, err
	}
	live := make(map[string]bool, len(chunks))
	for _, chunk := range chunks {
		if chunk != nil && chunk.KnowledgeBaseID == kbID {
			live[chunk.ID] = true
		}
	}
	var missing []string
	seen := map[string]bool{}
	for _, id := range chunkIDs {
		if id != "" && !live[id] && !seen[id] {
			missing = append(missing, id)
			seen[id] = true
		}
	}
	return missing, nil
}

func (s *wikiPageService) RegeneratePage(ctx context.Context, kbID, slug string, task interfaces.TaskEnqueuer) (*types.WikiPage, error) {
	page, err := s.repo.GetBySlug(ctx, kbID, slug)
	if err != nil {
		return nil, err
	}
	if len(page.SourceChanges) == 0 {
		return nil, errors.New("wiki page has no modified sources")
	}
	if task == nil || s.taskPendingRepo == nil {
		return nil, errors.New("wiki regeneration queue is unavailable")
	}
	op := WikiPendingOp{
		Op: WikiOpRegenerate, KnowledgeID: "regenerate:" + page.ID,
		PageSlug: slug, Language: types.LanguageFromContextOrDefault(ctx),
		SourceChanges: page.SourceChanges,
	}
	encoded, err := json.Marshal(op)
	if err != nil {
		return nil, err
	}
	accepted, err := enqueueWikiPendingOp(ctx, s.taskPendingRepo, &types.TaskPendingOp{
		TenantID: page.TenantID, TaskType: wikiTaskType, Scope: wikiTaskScope,
		ScopeID: kbID, Op: WikiOpRegenerate, DedupKey: op.KnowledgeID, Payload: encoded,
	})
	if err != nil {
		return nil, err
	}
	if !accepted {
		return nil, errors.New("wiki knowledge base is unavailable")
	}
	if err := enqueueWikiIngestTrigger(ctx, task, page.TenantID, kbID); err != nil {
		return nil, err
	}
	return page, nil
}

// regenerateSourcePage uses the existing wiki editor prompt and page write path.
// The queued snapshot is acknowledged only after the generated page is saved.
func (s *wikiIngestService) regenerateSourcePage(ctx context.Context, model chat.Chat, payload WikiIngestPayload, op WikiPendingOp, batch *WikiBatchContext) error {
	if op.PageSlug == "" || len(op.SourceChanges) == 0 {
		return errors.New("wiki regeneration is missing its source-change snapshot")
	}
	acquired, err := s.withSlugLock(ctx, payload.KnowledgeBaseID, op.PageSlug, func() error {
		page, err := s.wikiService.GetPageBySlug(ctx, payload.KnowledgeBaseID, op.PageSlug)
		if err != nil {
			return err
		}
		chunks, err := s.chunkRepo.ListChunksByID(ctx, payload.TenantID, page.ChunkRefs)
		if err != nil {
			return fmt.Errorf("read wiki source chunks: %w", err)
		}
		byID := make(map[string]*types.Chunk, len(chunks))
		for _, chunk := range chunks {
			if chunk != nil && chunk.KnowledgeBaseID == payload.KnowledgeBaseID && chunk.IsEnabled && chunk.ChunkType != types.ChunkTypeWikiPage {
				byID[chunk.ID] = chunk
			}
		}
		var source strings.Builder
		refs := types.StringArray{}
		for _, id := range page.ChunkRefs {
			if chunk := byID[id]; chunk != nil {
				fmt.Fprintf(&source, "<source_chunk id=\"%s\">\n%s\n</source_chunk>\n", id, chunk.Content)
				refs = appendUnique(refs, id)
			}
		}
		if len(refs) == 0 {
			page.Content = ""
			page.Summary = ""
		} else {
			var links strings.Builder
			titles := batch.SlugTitleMany(ctx, []string(page.OutLinks))
			slugs := make([]string, 0, len(titles))
			for slug := range titles {
				slugs = append(slugs, slug)
			}
			sort.Strings(slugs)
			handles := newWikiSlugHandles()
			for _, slug := range slugs {
				fmt.Fprintf(&links, "- %s (%s)\n", handles.handle(slug), titles[slug])
			}
			content, err := s.generateWithTemplate(ctx, model, agent.WikiPageModifyUserPrompt, map[string]string{
				"HasAdditions": "1", "HasRetractions": "", "PageSlug": page.Slug,
				"PageTitle": page.Title, "PageType": page.PageType, "PageAliases": strings.Join(page.Aliases, ", "),
				"ExistingContent": "(New page)", "SharedSourceContexts": "", "NewContent": source.String(),
				"DeletedContent": "", "RemainingSourcesContent": "", "AvailableSlugs": links.String(),
				"Language": types.ResolveLanguageName(ctx, op.Language), "CustomInstructions": batch.ContentInstructions,
				"InstructionScope": "wiki_content",
			})
			if err != nil {
				return err
			}
			if strings.TrimSpace(content) == "" {
				return errors.New("wiki regeneration returned empty content")
			}
			page.Summary, page.Content = splitSummaryLine(handles.decodeContent(content))
			if page.Content == "" {
				page.Content = handles.decodeContent(content)
			}
		}
		// Keep references to disabled chunks so enabling them later marks the page.
		if _, err := s.wikiService.UpdatePage(ctx, page); err != nil {
			return err
		}
		return s.wikiService.ClearSourceChanges(ctx, payload.KnowledgeBaseID, page.Slug, op.SourceChanges)
	})
	if err != nil {
		return err
	}
	if !acquired {
		return ErrWikiIngestConcurrent
	}
	return nil
}
