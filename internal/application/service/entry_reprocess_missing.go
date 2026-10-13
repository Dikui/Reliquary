package service

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"time"

	"reliquary/internal/types"
)

// prepareMissingContentOriginalURL keeps valid intake provenance and recovers the
// original link from legacy metadata or source when saved content is absent.
func prepareMissingContentOriginalURL(entry *types.Entry) (string, error) {
	source, err := entrySourceInfo(entry)
	if err != nil {
		return "", err
	}
	originalURL := strings.TrimSpace(source.OriginalURL)
	if !validIngestURL(originalURL) {
		originalURL = strings.TrimSpace(entry.GetMetadata()["original_url"])
	}
	if !validIngestURL(originalURL) {
		originalURL = strings.TrimSpace(entry.Source)
	}
	if !validIngestURL(originalURL) {
		originalURL = ""
	}
	source.OriginalURL = originalURL
	intake, err := entry.IntakeMetadata()
	if err != nil {
		return "", err
	}
	if intake == nil {
		intake = &types.EntryIntakeMetadata{Source: entry.Type, Decision: "approved", SourceInfo: source}
		if err := entry.SetIntakeMetadata(intake); err != nil {
			return "", err
		}
	} else if !validIngestURL(intake.SourceInfo.OriginalURL) && originalURL != "" {
		intake.SourceInfo.OriginalURL = originalURL
		if err := entry.SetIntakeMetadata(intake); err != nil {
			return "", err
		}
	}
	if entry.NormalizedURL == "" {
		entry.NormalizedURL = normalizeMessageURL(originalURL)
	}
	return originalURL, nil
}

// markReprocessContentMissing preserves old content and indexes while an
// external tool supplies the body through the same original link.
func (s *entryService) markReprocessContentMissing(ctx context.Context, entry *types.Entry) (*types.Entry, error) {
	originalURL, err := prepareMissingContentOriginalURL(entry)
	if err != nil {
		return nil, err
	}
	missing := entry.IngestMissing()
	found := false
	for i := range missing {
		if missing[i].Type == "content" {
			missing[i].Reason = "重新处理需要正文"
			if !validIngestURL(missing[i].SourceURL) {
				missing[i].SourceURL = originalURL
			}
			found = true
		}
	}
	if !found {
		missing = append(missing, types.IngestMissing{Type: "content", SourceURL: originalURL, Reason: "重新处理需要正文", APIKeyID: entry.LastIngestAPIKeyID, APIKeyName: entry.LastIngestAPIKeyName, SubmittedAt: time.Now()})
	}
	if err := entry.SetIngestMissing(missing); err != nil {
		return nil, err
	}
	entry.Incomplete, entry.ExternalCompletionAttempted, entry.UpdatedAt = true, false, time.Now()
	values := map[string]any{"metadata": entry.Metadata, "normalized_url": entry.NormalizedURL, "missing_materials": entry.MissingMaterials, "incomplete": true, "external_completion_attempted": false, "updated_at": entry.UpdatedAt}
	if entry.SourceKind == "" {
		entry.SourceKind = "article"
		values["source_kind"] = "article"
	}
	if err := s.repo.UpdateEntryColumns(ctx, entry.ID, values); err != nil {
		return nil, err
	}
	if s.vault != nil {
		s.vault.ScheduleRefresh(ctx, entry.ID)
	}
	return entry, nil
}

// reprocessStoredBody reads saved article text without using its original URL.
func (s *entryService) reprocessStoredBody(ctx context.Context, entry *types.Entry) (string, bool, error) {
	if entry.ProcessingMode == types.ProcessingModeSummaryOnly {
		return "", false, nil
	}
	if s.vault != nil && entry.VaultPath != "" {
		body, err := s.vault.ReadBody(entry)
		return string(body), strings.TrimSpace(string(body)) != "", err
	}
	if strings.TrimSpace(entry.VaultBody) != "" {
		return entry.VaultBody, true, nil
	}
	intake, err := entry.IntakeMetadata()
	if err != nil {
		return "", false, err
	}
	var metadata struct {
		ProcessedBodyPath string `json:"processed_body_path"`
	}
	if len(entry.Metadata) > 0 {
		if err := json.Unmarshal(entry.Metadata, &metadata); err != nil {
			return "", false, err
		}
	}
	if (intake != nil && intake.OriginalPath != "") || metadata.ProcessedBodyPath != "" {
		file, _, err := s.GetEntryBody(ctx, entry.ID)
		if err != nil {
			return "", false, err
		}
		defer file.Close()
		body, err := io.ReadAll(file)
		return string(body), strings.TrimSpace(string(body)) != "", err
	}
	if entry.IsManual() {
		manual, err := entry.ManualMetadata()
		if err != nil {
			return "", false, err
		}
		if manual != nil {
			return manual.Content, strings.TrimSpace(manual.Content) != "", nil
		}
	}
	return "", false, nil
}
