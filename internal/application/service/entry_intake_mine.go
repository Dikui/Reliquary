package service

import (
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"time"

	werrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

// The user's explicit intake decision replaces model review. Keep the stored
// materials and ID, then use the imported document's ordinary processing path.
func (s *entryService) convertIntakeToMine(ctx context.Context, kb *types.KnowledgeBase, k *types.Entry, before types.EntryIntakeMetadata) (*types.Entry, error) {
	if s.vault == nil || s.db == nil || before.OriginalPath == "" {
		return nil, werrors.NewBadRequestError("stored original and configured vault are required")
	}
	file, err := s.resolveFileServiceForPath(ctx, kb, before.OriginalPath).GetFile(ctx, before.OriginalPath)
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(file)
	_ = file.Close()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(data)) == "" {
		return nil, werrors.NewBadRequestError("stored original is empty")
	}
	intake := before
	intake.Decision, intake.Reason = "approved", ""
	if title := strings.TrimSuffix(k.Title, filepath.Ext(k.FileName)); title != "" {
		k.Title = title
	}
	k.SourceKind, k.SourceUnverified, k.ProcessingMode = "mine", false, types.ProcessingModeFull
	k.ParseStatus, k.ErrorMessage, k.UpdatedAt = types.ParseStatusPending, "", time.Now()
	metadata := map[string]json.RawMessage{}
	if len(k.Metadata) > 0 {
		if err := json.Unmarshal(k.Metadata, &metadata); err != nil {
			return nil, err
		}
	}
	delete(metadata, "intake_reject_reason")
	k.Metadata, err = json.Marshal(metadata)
	if err != nil {
		return nil, err
	}
	if err := k.SetIntakeMetadata(&intake); err != nil {
		return nil, err
	}
	setMessageFingerprint(k, string(data))
	if err := lockMessageDeduplication(s.db.WithContext(ctx), k); err != nil {
		return nil, err
	}
	if duplicate, _, _, err := findMessageDuplicate(s.db.WithContext(ctx), k); err != nil {
		return nil, err
	} else if duplicate != nil {
		return nil, werrors.NewConflictError("the user document has already been imported")
	}
	if err := s.db.WithContext(ctx).Model(&types.Entry{}).Where("id=? AND space_id=?", k.ID, k.SpaceID).Updates(map[string]any{
		"source_kind": k.SourceKind, "title": k.Title, "source_unverified": false, "processing_mode": k.ProcessingMode, "parse_status": k.ParseStatus,
		"error_message": "", "updated_at": k.UpdatedAt, "metadata": k.Metadata, "normalized_url": k.NormalizedURL,
		"content_hash": k.ContentHash, "content_sim_hash": k.ContentSimHash, "content_rune_count": k.ContentRuneCount,
	}).Error; err != nil {
		return nil, err
	}
	if err := NewOperationLogService(s.db).RecordTx(ctx, s.db, "review_convert_to_mine", k.ID, before, map[string]any{"source_kind": "mine", "intake": intake}); err != nil {
		return nil, err
	}
	if _, err := s.enqueueManualProcessing(ctx, k, string(data), false); err != nil {
		return nil, err
	}
	return k, nil
}
