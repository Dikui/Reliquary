package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"

	"reliquary/internal/diagnostics"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/logger"
	"reliquary/internal/modelcalls"
	"reliquary/internal/types"
)

func (s *entryService) SetIngestDiagnostics(d *diagnostics.Service) {
	s.diagnostics = d
	if d != nil {
		d.SetRecoveryLookup(s.lookupIngestRecovery)
	}
}

func (s *entryService) lookupIngestRecovery(ctx context.Context, failure *types.IngestFailure, input []byte) (string, error) {
	var request types.IngestRequest
	if failure.LibraryID == "" || json.Unmarshal(input, &request) != nil {
		return "", nil
	}
	if validateLibraryIngest(&request, nil) != nil {
		return "", nil
	}
	entry, err := s.findExactIngestSource(ctx, &types.Entry{
		SpaceID: failure.SpaceID, LibraryID: failure.LibraryID,
		SourceRef: request.SourceRef, Identifier: request.Identifier,
		NormalizedURL: normalizeMessageURL(request.URL),
	})
	var conflict *apperrors.AppError
	if errors.As(err, &conflict) && conflict.HTTPCode == http.StatusConflict {
		return "", nil
	}
	if err != nil || entry == nil {
		return "", err
	}
	// Only a committed ingest after the failure proves recovery. Background
	// processing or an unrelated edit must not clear a failed submission.
	var recovered bool
	err = s.db.WithContext(ctx).Model(&types.OperationLog{}).Select("count(*) > 0").
		Where("space_id = ? AND object_id = ? AND operation_type = ? AND created_at > ?",
			failure.SpaceID, entry.ID, "ingest", failure.CreatedAt).Scan(&recovered).Error
	if err != nil || !recovered {
		return "", err
	}
	return entry.ID, nil
}

func technicalIngestError(err error) bool {
	if err == nil {
		return false
	}
	var identity *ingestIdentityError
	if errors.As(err, &identity) {
		return false
	}
	var app *apperrors.AppError
	return !errors.As(err, &app) || app.HTTPCode >= 500
}

func (s *entryService) ingestWithDiagnostics(ctx context.Context, req *types.IngestRequest, index int) (result *types.IngestResult, err error) {
	ctx = types.WithPrivateIngestContent(ctx)
	if s.diagnostics == nil || validateIngestRequest(req) != nil {
		return s.Ingest(ctx, req)
	}
	if req.RecoveryFailureID != "" {
		if _, err := s.diagnostics.Get(ctx, req.RecoveryFailureID); err != nil {
			return nil, apperrors.NewBadRequestError("recovery_failure_id does not identify an available diagnostic record")
		}
	}
	input := req.RawInput
	if len(input) == 0 {
		input, _ = json.Marshal(req)
	}
	ctx, attempt := s.diagnostics.Begin(ctx, input, index, req.Library, "")
	attempt.SetRecoveryFailure(req.RecoveryFailureID)
	panicked := false
	defer func() {
		for _, call := range attempt.DrainModelCalls(err == nil && !panicked) {
			if e := modelcalls.NewStore(s.db).Enqueue(ctx, call); e != nil {
				logger.Warnf(ctx, "model call persistence unavailable")
			}
		}
	}()
	for i, image := range req.Images {
		if image.Data != "" {
			if data, e := decodeIngestInline(image.Data); e == nil {
				diagnostics.CaptureImage(ctx, fmt.Sprintf("image-%d", i), ingestImageSource(image), http.DetectContentType(data), data)
			}
		}
	}
	if s.config != nil && s.config.ArticleProcessing != nil {
		attempt.SetConfig("article_processing", s.config.ArticleProcessing)
	}
	defer func() {
		if v := recover(); v != nil {
			panicked = true
			attempt.Panic(v)
			panic(v)
		}
		if technicalIngestError(err) {
			for _, image := range req.Images {
				if image.AssetID != "" {
					attempt.RequireImage("asset_id:" + image.AssetID)
				}
			}
			outcome := ""
			retryable := false
			var network net.Error
			if errors.As(err, &network) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				outcome = "unknown"
				retryable = true
			}
			ref := attempt.Record(err, "ingest_failed", retryable, outcome)
			if result == nil {
				result = &types.IngestResult{}
			}
			result.Diagnostics = ref
		} else {
			if cleanupErr := attempt.Discard(); cleanupErr != nil {
				logger.Warnf(ctx, "diagnostic temporary cleanup failed")
			}
			if err == nil && result != nil && req.RecoveryFailureID != "" {
				var e error
				if result.Status == "discarded" {
					e = s.diagnostics.RemoveFailureMaterials(ctx, req.RecoveryFailureID)
				} else if result.EntryID != "" {
					e = s.diagnostics.LinkRecovery(ctx, req.RecoveryFailureID, result.EntryID)
				}
				if e != nil {
					logger.Warnf(ctx, "diagnostic recovery link update failed")
				}
			}
		}
	}()
	return s.Ingest(ctx, req)
}

func (s *entryService) removeEntryDiagnosticMaterials(ctx context.Context, entry *types.Entry) error {
	if s.diagnostics == nil {
		return nil
	}
	var metadata struct {
		RecoveryID string `json:"recovery_failure_id"`
	}
	_ = json.Unmarshal(entry.Metadata, &metadata)
	return s.diagnostics.RemoveEntryMaterials(ctx, entry.ID, metadata.RecoveryID)
}
