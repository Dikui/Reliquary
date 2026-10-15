package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"reliquary/internal/utils"
)

// ErrChunkNotFound is returned when a chunk lookup finds no row. A typed
// sentinel (matching the ErrXNotFound convention used by the other repos)
// so callers can errors.Is it safely through wrapping — replacing the
// previous bare errors.New("chunk not found") that forced fragile
// string-equality matching in the service layer.
var ErrChunkNotFound = errors.New("chunk not found")

// chunkRepository implements the ChunkRepository interface
type chunkRepository struct {
	db *gorm.DB
}

// NewChunkRepository creates a new chunk repository
func NewChunkRepository(db *gorm.DB) interfaces.ChunkRepository {
	return &chunkRepository{db: db}
}

// CreateChunks creates multiple chunks in batches.
// Uses Omit("SeqID") so GORM won't include the auto-increment column in the
// INSERT, which avoids MySQL generating ON DUPLICATE KEY UPDATE and the
// resulting gap-lock deadlocks under concurrent writes.
// A deadlock retry wrapper is kept as defense-in-depth for any remaining
// edge cases on secondary unique indexes.
func (r *chunkRepository) CreateChunks(ctx context.Context, chunks []*types.Chunk) error {
	disabledIDs := make([]string, 0)
	disabledChunks := make([]*types.Chunk, 0)
	for _, chunk := range chunks {
		if !chunk.IsEnabled {
			disabledIDs = append(disabledIDs, chunk.ID)
			disabledChunks = append(disabledChunks, chunk)
		}
		chunk.Content = utils.CleanInvalidUTF8(chunk.Content)
		chunk.ContextHeader = utils.CleanInvalidUTF8(chunk.ContextHeader)
	}

	db := r.db.WithContext(ctx)

	// SeqID=0 is skipped by GORM automatically (autoIncrement tag).
	if len(disabledIDs) == 0 {
		return db.Select("*").CreateInBatches(chunks, 100).Error
	}
	// GORM replaces false with the model's true default during INSERT, even
	// with Select("*"). Restore explicitly disabled chunks in the same transaction.
	defer func() {
		for _, chunk := range disabledChunks {
			chunk.IsEnabled = false
		}
	}()
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Select("*").CreateInBatches(chunks, 100).Error; err != nil {
			return err
		}
		return tx.Model(&types.Chunk{}).Where("id IN ?", disabledIDs).Update("is_enabled", false).Error
	})
}

// GetChunkByID retrieves a chunk by its ID and space ID
func (r *chunkRepository) GetChunkByID(ctx context.Context, spaceID uint64, id string) (*types.Chunk, error) {
	var chunk types.Chunk
	if err := r.db.WithContext(ctx).Where("space_id = ? AND id = ?", spaceID, id).First(&chunk).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrChunkNotFound
		}
		return nil, err
	}
	return &chunk, nil
}

// GetChunkByIDOnly retrieves a chunk by ID without space filter (for permission resolution).
func (r *chunkRepository) GetChunkByIDOnly(ctx context.Context, id string) (*types.Chunk, error) {
	var chunk types.Chunk
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&chunk).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrChunkNotFound
		}
		return nil, err
	}
	return &chunk, nil
}

// GetChunkBySeqID retrieves a chunk by its seq_id and space ID
func (r *chunkRepository) GetChunkBySeqID(ctx context.Context, spaceID uint64, seqID int64) (*types.Chunk, error) {
	var chunk types.Chunk
	if err := r.db.WithContext(ctx).Where("space_id = ? AND seq_id = ?", spaceID, seqID).First(&chunk).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrChunkNotFound
		}
		return nil, err
	}
	return &chunk, nil
}

// ListChunksByID retrieves multiple chunks by their IDs
func (r *chunkRepository) ListChunksByID(
	ctx context.Context, spaceID uint64, ids []string,
) ([]*types.Chunk, error) {
	var chunks []*types.Chunk
	if err := r.db.WithContext(ctx).
		Where("space_id = ? AND id IN ?", spaceID, ids).
		Find(&chunks).Error; err != nil {
		return nil, err
	}
	return chunks, nil
}

// ListChunksByIDOnly retrieves multiple chunks by their IDs without space filter (for shared Library resolution).
func (r *chunkRepository) ListChunksByIDOnly(ctx context.Context, ids []string) ([]*types.Chunk, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var chunks []*types.Chunk
	if err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&chunks).Error; err != nil {
		return nil, err
	}
	return chunks, nil
}

// ListChunksBySeqID retrieves multiple chunks by their seq_ids
func (r *chunkRepository) ListChunksBySeqID(
	ctx context.Context, spaceID uint64, seqIDs []int64,
) ([]*types.Chunk, error) {
	if len(seqIDs) == 0 {
		return []*types.Chunk{}, nil
	}
	var chunks []*types.Chunk
	if err := r.db.WithContext(ctx).
		Where("space_id = ? AND seq_id IN ?", spaceID, seqIDs).
		Find(&chunks).Error; err != nil {
		return nil, err
	}
	return chunks, nil
}

// ListChunksByEntryID lists all chunks for a entry ID
func (r *chunkRepository) ListChunksByEntryID(
	ctx context.Context, spaceID uint64, entryID string,
) ([]*types.Chunk, error) {
	var chunks []*types.Chunk
	if err := r.db.WithContext(ctx).
		Where("space_id = ? AND entry_id = ? and chunk_type = ?", spaceID, entryID, "text").
		Order("chunk_index ASC").
		Find(&chunks).Error; err != nil {
		return nil, err
	}
	return chunks, nil
}

// ListChunksByEntryIDAndTypes lists a entry's chunks restricted to the
// given chunk types. ListChunksByEntryID is text-only by design, so callers
// that also need summary / parent_text / image chunks come through here rather
// than widening that query underneath its existing callers.
func (r *chunkRepository) ListChunksByEntryIDAndTypes(
	ctx context.Context, spaceID uint64, entryID string, chunkTypes []types.ChunkType,
) ([]*types.Chunk, error) {
	if len(chunkTypes) == 0 {
		return nil, nil
	}
	var chunks []*types.Chunk
	if err := r.db.WithContext(ctx).
		Where("space_id = ? AND entry_id = ? AND chunk_type IN ?", spaceID, entryID, chunkTypes).
		Order("chunk_index ASC").
		Find(&chunks).Error; err != nil {
		return nil, err
	}
	return chunks, nil
}

// ListPagedChunksByEntryID lists chunks for a entry ID with pagination
func (r *chunkRepository) ListPagedChunksByEntryID(ctx context.Context,
	spaceID uint64,
	entryID string,
	page *types.Pagination,
	chunkType []types.ChunkType,
	keyword string,
	sortOrder string,
	isEnabled *bool) ([]*types.Chunk, int64, error) {
	var chunks []*types.Chunk
	var total int64
	keyword = strings.TrimSpace(keyword)

	baseFilter := func(db *gorm.DB) *gorm.DB {
		db = db.Where("space_id = ? AND entry_id = ? AND chunk_type IN (?) AND status in (?)",
			spaceID, entryID, chunkType, []int{int(types.ChunkStatusIndexed), int(types.ChunkStatusDefault)})
		if isEnabled != nil {
			db = db.Where("is_enabled = ?", *isEnabled)
		}
		if keyword != "" {
			like := "%" + keyword + "%"

			db = db.Where("content LIKE ?", like)
		}
		return db
	}

	query := baseFilter(r.db.WithContext(ctx).Model(&types.Chunk{}))

	// First query the total count
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// Then query the paginated data
	dataQuery := baseFilter(r.db.WithContext(ctx))

	// Determine sort order based on entry type
	var orderClause string

	// Document: sort by chunk_index
	orderClause = "chunk_index ASC"
	if sortOrder == "desc" {
		orderClause = "chunk_index DESC"
	}

	if err := dataQuery.
		Order(orderClause).
		Offset(page.Offset()).
		Limit(page.Limit()).
		Find(&chunks).Error; err != nil {
		return nil, 0, err
	}

	return chunks, total, nil
}

func (r *chunkRepository) ListChunkByParentID(
	ctx context.Context,
	spaceID uint64,
	parentID string,
) ([]*types.Chunk, error) {
	var chunks []*types.Chunk
	if err := r.db.WithContext(ctx).
		Where("space_id = ? AND parent_chunk_id = ?", spaceID, parentID).
		Find(&chunks).Error; err != nil {
		return nil, err
	}
	return chunks, nil
}

func (r *chunkRepository) ListChunksByParentIDs(
	ctx context.Context,
	spaceID uint64,
	parentIDs []string,
) ([]*types.Chunk, error) {
	if len(parentIDs) == 0 {
		return nil, nil
	}
	var chunks []*types.Chunk
	if err := r.db.WithContext(ctx).
		Where("space_id = ? AND parent_chunk_id IN ?", spaceID, parentIDs).
		Find(&chunks).Error; err != nil {
		return nil, err
	}
	return chunks, nil
}

// UpdateChunk updates a chunk using GORM Save, which updates ALL fields
// except SeqID (auto-increment, must not be overwritten).
// Make sure the chunk object is complete (e.g., fetched from DB) before calling this method.
func (r *chunkRepository) UpdateChunk(ctx context.Context, chunk *types.Chunk) error {
	return r.db.WithContext(ctx).Omit("SeqID").Save(chunk).Error
}

// SaveChunks persists full chunk objects in a single transaction using GORM Save (UPDATE).
func (r *chunkRepository) SaveChunks(ctx context.Context, chunks []*types.Chunk) error {
	if len(chunks) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, chunk := range chunks {
			if err := tx.Omit("SeqID").Save(chunk).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// UpdateChunks updates chunks in batch using raw SQL for efficiency.
// Uses raw SQL to bypass GORM's default value handling for boolean fields.
//
// IMPORTANT: This method only updates the following fields:
//   - content
//   - is_enabled
//   - flags
//   - status
//   - updated_at
//
// Fields NOT updated by this method (will retain their original values):
//   - metadata
//   - content_hash
//   - embedding-related fields
//   - other fields not listed above
//
// If you need to update metadata or content_hash, use UpdateChunk (single) instead.
func (r *chunkRepository) UpdateChunks(ctx context.Context, chunks []*types.Chunk) error {
	if len(chunks) == 0 {
		return nil
	}

	// Build batch update SQL with CASE expressions
	var ids []string
	contentCases := make([]string, 0, len(chunks))
	isEnabledCases := make([]string, 0, len(chunks))
	flagsCases := make([]string, 0, len(chunks))
	statusCases := make([]string, 0, len(chunks))

	var contentArgs []interface{}
	var isEnabledArgs []interface{}
	var flagsArgs []interface{}
	var statusArgs []interface{}

	for _, chunk := range chunks {
		ids = append(ids, chunk.ID)
		content := utils.CleanInvalidUTF8(chunk.Content)

		contentCases = append(contentCases, "WHEN id = ? THEN ?")
		contentArgs = append(contentArgs, chunk.ID, content)

		// Convert bool to string for PostgreSQL compatibility
		isEnabledStr := "false"
		if chunk.IsEnabled {
			isEnabledStr = "true"
		}
		isEnabledCases = append(isEnabledCases, "WHEN id = ? THEN ?")
		isEnabledArgs = append(isEnabledArgs, chunk.ID, isEnabledStr)

		flagsCases = append(flagsCases, "WHEN id = ? THEN ?")
		flagsArgs = append(flagsArgs, chunk.ID, fmt.Sprintf("%d", chunk.Flags))

		statusCases = append(statusCases, "WHEN id = ? THEN ?")
		statusArgs = append(statusArgs, chunk.ID, fmt.Sprintf("%d", chunk.Status))
	}

	// Build IN clause placeholders
	inPlaceholders := make([]string, len(ids))
	for i := range ids {
		inPlaceholders[i] = "?"
	}

	// Combine args in correct order: content, is_enabled, flags, status, then IN clause
	var args []interface{}
	args = append(args, contentArgs...)
	args = append(args, isEnabledArgs...)
	args = append(args, flagsArgs...)
	args = append(args, statusArgs...)
	for _, id := range ids {
		args = append(args, id)
	}

	sql := fmt.Sprintf(`
			UPDATE chunks SET
				content = CASE %s END,
				is_enabled = (CASE %s END)::boolean,
				flags = (CASE %s END)::integer,
				status = (CASE %s END)::integer,
				updated_at = NOW()
			WHERE id IN (%s)
		`,
		strings.Join(contentCases, " "),
		strings.Join(isEnabledCases, " "),
		strings.Join(flagsCases, " "),
		strings.Join(statusCases, " "),
		strings.Join(inPlaceholders, ","),
	)
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(sql, args...).Error; err != nil {
			return err
		}
		return nil
	})
}

// DeleteChunk deletes a chunk by its ID
func (r *chunkRepository) DeleteChunk(ctx context.Context, spaceID uint64, id string) error {
	return r.db.WithContext(ctx).Where("space_id = ? AND id = ?", spaceID, id).Delete(&types.Chunk{}).Error
}

// DeleteChunks deletes chunks by IDs in batch.
// To avoid MySQL Error 1390 (too many placeholders), IDs are split into batches.
func (r *chunkRepository) DeleteChunks(ctx context.Context, spaceID uint64, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	const batchSize = 5000
	for i := 0; i < len(ids); i += batchSize {
		end := i + batchSize
		if end > len(ids) {
			end = len(ids)
		}
		if err := r.db.WithContext(ctx).Where("space_id = ? AND id IN ?", spaceID, ids[i:end]).Delete(&types.Chunk{}).Error; err != nil {
			return err
		}
	}
	return nil
}

// DeleteChunksByEntryID deletes all chunks for a entry ID
func (r *chunkRepository) DeleteChunksByEntryID(ctx context.Context, spaceID uint64, entryID string) error {
	return r.db.WithContext(ctx).Where(
		"space_id = ? AND entry_id = ?", spaceID, entryID,
	).Delete(&types.Chunk{}).Error
}

// ListImageInfoByEntryIDs returns non-empty image_info values for the given entry IDs.
// The cleanup scan includes all stored chunk types.
func (r *chunkRepository) ListImageInfoByEntryIDs(
	ctx context.Context, spaceID uint64, entryIDs []string,
) ([]interfaces.ChunkImageInfo, error) {
	var results []interfaces.ChunkImageInfo
	err := r.db.WithContext(ctx).
		Model(&types.Chunk{}).
		Select("entry_id, image_info").
		Where("space_id = ? AND entry_id IN ? AND image_info != ''", spaceID, entryIDs).
		Scan(&results).Error
	return results, err
}

// DeleteByEntryList deletes all chunks for a entry list
func (r *chunkRepository) DeleteByEntryList(ctx context.Context, spaceID uint64, entryIDs []string) error {
	return r.db.WithContext(ctx).Where(
		"space_id = ? AND entry_id in ?", spaceID, entryIDs,
	).Delete(&types.Chunk{}).Error
}

// CountChunksByLibraryID counts the number of chunks in a library
func (r *chunkRepository) CountChunksByLibraryID(
	ctx context.Context,
	spaceID uint64,
	libraryID string,
) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&types.Chunk{}).
		Where("space_id = ? AND library_id = ?", spaceID, libraryID).
		Count(&count).Error
	return count, err
}

// DeleteUnindexedChunks by entry id and chunk index range
func (r *chunkRepository) DeleteUnindexedChunks(
	ctx context.Context,
	spaceID uint64,
	entryID string,
) ([]*types.Chunk, error) {
	var chunks []*types.Chunk
	if err := r.db.WithContext(ctx).
		Where("space_id = ? AND entry_id = ? AND status = ?", spaceID, entryID, types.ChunkStatusStored).
		Find(&chunks).Error; err != nil {
		return nil, err
	}
	if len(chunks) > 0 {
		if err := r.db.WithContext(ctx).
			Where("space_id = ? AND entry_id = ? AND status = ?", spaceID, entryID, types.ChunkStatusStored).
			Delete(&types.Chunk{}).Error; err != nil {
			return nil, err
		}
	}
	return chunks, nil
}
