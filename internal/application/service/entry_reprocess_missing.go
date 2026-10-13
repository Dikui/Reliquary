package service

import (
	"context"
	"encoding/json"
	"io"
	"strings"

	werrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

// Missing saved Markdown cannot fall back to retrieving or parsing a source.
func (s *entryService) markReprocessContentMissing(ctx context.Context, entry *types.Entry) (*types.Entry, error) {
	return nil, werrors.NewBadRequestError("stored Markdown body is required; convert the source to Markdown and ingest it again")
}

// reprocessStoredBody reads saved article text without using its original URL.
func (s *entryService) reprocessStoredBody(ctx context.Context, entry *types.Entry) (string, bool, error) {

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
