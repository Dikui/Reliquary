package service

import (
	"context"
	"io"
	"sort"
	"strings"

	werrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

// EntryReadResult exposes the database-owned header and a Unicode-safe body slice.
type EntryReadResult struct {
	Header           VaultArticleHeader `json:"header"`
	Body             string             `json:"body"`
	OriginalURL      string             `json:"original_url"`
	ParseStatus      string             `json:"parse_status"`
	SourceUnverified bool               `json:"source_unverified"`
	Acquisition      string             `json:"acquisition"`
	Offset           int                `json:"offset"`
	Length           int                `json:"length"`
	TotalLength      int                `json:"total_length"`
	NextOffset       *int               `json:"next_offset,omitempty"`
}

// ReadEntry reuses the entry and immutable-body services used by HTTP.
// Offsets and lengths count Unicode code points, so slices never split UTF-8.
func (s *entryService) ReadEntry(ctx context.Context, id string, offset, length int) (*EntryReadResult, error) {
	if strings.TrimSpace(id) == "" || offset < 0 || length < 0 || length > 100000 {
		return nil, werrors.NewBadRequestError("id is required; offset must be nonnegative and length must be between 0 and 100000")
	}
	if length == 0 {
		length = 20000
	}
	entry, err := s.GetEntryByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if s.vault == nil {
		return nil, werrors.NewInternalServerError("article vault is unavailable")
	}
	header, err := s.vault.articleHeader(ctx, entry)
	if err != nil {
		return nil, err
	}
	if header.Entities == nil {
		header.Entities = []string{}
	}
	if header.Topics == nil {
		header.Topics = []string{}
	}
	sort.Strings(header.Entities)
	sort.Strings(header.Topics)
	result := &EntryReadResult{Header: header, OriginalURL: header.URL,
		ParseStatus: entry.ParseStatus, SourceUnverified: entry.SourceUnverified, Acquisition: entry.Acquisition, Offset: offset}
	reader, _, err := s.GetEntryBody(ctx, id)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	runes := []rune(string(body))
	if offset > len(runes) {
		return nil, werrors.NewBadRequestError("offset exceeds the stored body length")
	}
	end := min(offset+length, len(runes))
	result.Body, result.Length, result.TotalLength = string(runes[offset:end]), end-offset, len(runes)
	if end < len(runes) {
		result.NextOffset = &end
	}
	return result, nil
}

// ListPending lists intake and processing reviews without changing entry visibility.
func (s *entryService) ListPending(ctx context.Context, page, pageSize int, kinds ...string) (*types.PageResult, error) {
	if s.db == nil {
		return nil, werrors.NewInternalServerError("pending reviews require PostgreSQL")
	}
	if page == 0 {
		page = 1
	}
	if pageSize == 0 {
		pageSize = 20
	}
	if page < 1 || pageSize < 1 || pageSize > 100 {
		return nil, werrors.NewBadRequestError("page must be positive and page_size must be between 1 and 100")
	}
	kind := ""
	if len(kinds) > 0 {
		kind = kinds[0]
	}
	if kind != "" && kind != "intake" {
		return nil, werrors.NewBadRequestError("kind must be intake")
	}
	library, err := s.libraryService.GetLibrary(ctx)
	if err != nil {
		return nil, err
	}
	query := s.db.WithContext(ctx).Model(&types.Entry{}).Where("space_id=? AND library_id=? AND source_kind IN ('article','mine')", library.SpaceID, library.ID)
	query = query.Where("parse_status=?", types.ParseStatusRejected)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}
	items := []*types.Entry{}
	if err := query.Order("created_at DESC, id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error; err != nil {
		return nil, err
	}
	for _, item := range items {
		if item.ParseStatus == types.ParseStatusRejected {
			item.ReviewKind = "intake"
		} else {
			item.ReviewKind = "processing"
		}
	}
	return types.NewPageResult(total, &types.Pagination{Page: page, PageSize: pageSize}, items), nil
}
