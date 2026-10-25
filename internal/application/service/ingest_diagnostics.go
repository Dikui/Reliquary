package service

import (
	"context"
	"errors"
	"unicode/utf8"

	apperrors "reliquary/internal/errors"
	"reliquary/internal/logger"
	"reliquary/internal/types"
	"reliquary/internal/utils"
)

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

// ingestWithDiagnostics logs one line per technical failure; the body and images
// are not kept, so callers resubmit from their own copy of the source.
func (s *entryService) ingestWithDiagnostics(ctx context.Context, req *types.IngestRequest, index int) (*types.IngestResult, error) {
	ctx = types.WithPrivateIngestContent(ctx)
	result, err := s.Ingest(ctx, req)
	if technicalIngestError(err) {
		logger.Errorf(ctx, "ingest item failed: index=%d library=%s url=%s title=%s content_runes=%d images=%d error=%v",
			index, utils.SanitizeForLog(req.Library), utils.SanitizeForLog(req.URL), utils.SanitizeForLog(req.Title),
			utf8.RuneCountInString(req.Content), len(req.Images), err)
	}
	return result, err
}
