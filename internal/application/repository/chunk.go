package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"reliquary/internal/common"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

var ErrChunkRevisionConflict = errors.New("chunk revision conflict")

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
		chunk.Content = common.CleanInvalidUTF8(chunk.Content)
		chunk.ContextHeader = common.CleanInvalidUTF8(chunk.ContextHeader)
		if chunk.SourceContent == "" {
			chunk.SourceContent = chunk.Content
		}
		if chunk.IndexStatus == "" {
			chunk.IndexStatus = "ready"
		}
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

// GetChunkByID retrieves a chunk by its ID and tenant ID
func (r *chunkRepository) GetChunkByID(ctx context.Context, tenantID uint64, id string) (*types.Chunk, error) {
	var chunk types.Chunk
	if err := r.db.WithContext(ctx).Where("tenant_id = ? AND id = ?", tenantID, id).First(&chunk).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrChunkNotFound
		}
		return nil, err
	}
	return &chunk, nil
}

// GetChunkByIDOnly retrieves a chunk by ID without tenant filter (for permission resolution).
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

// GetChunkBySeqID retrieves a chunk by its seq_id and tenant ID
func (r *chunkRepository) GetChunkBySeqID(ctx context.Context, tenantID uint64, seqID int64) (*types.Chunk, error) {
	var chunk types.Chunk
	if err := r.db.WithContext(ctx).Where("tenant_id = ? AND seq_id = ?", tenantID, seqID).First(&chunk).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrChunkNotFound
		}
		return nil, err
	}
	return &chunk, nil
}

// ListChunksByID retrieves multiple chunks by their IDs
func (r *chunkRepository) ListChunksByID(
	ctx context.Context, tenantID uint64, ids []string,
) ([]*types.Chunk, error) {
	var chunks []*types.Chunk
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND id IN ?", tenantID, ids).
		Find(&chunks).Error; err != nil {
		return nil, err
	}
	return chunks, nil
}

// ListChunksByIDOnly retrieves multiple chunks by their IDs without tenant filter (for shared KB resolution).
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
	ctx context.Context, tenantID uint64, seqIDs []int64,
) ([]*types.Chunk, error) {
	if len(seqIDs) == 0 {
		return []*types.Chunk{}, nil
	}
	var chunks []*types.Chunk
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND seq_id IN ?", tenantID, seqIDs).
		Find(&chunks).Error; err != nil {
		return nil, err
	}
	return chunks, nil
}

// ListChunksByKnowledgeID lists all chunks for a knowledge ID
func (r *chunkRepository) ListChunksByKnowledgeID(
	ctx context.Context, tenantID uint64, knowledgeID string,
) ([]*types.Chunk, error) {
	var chunks []*types.Chunk
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND knowledge_id = ? and chunk_type = ?", tenantID, knowledgeID, "text").
		Order("chunk_index ASC").
		Find(&chunks).Error; err != nil {
		return nil, err
	}
	return chunks, nil
}

// ListChunksByKnowledgeIDAndTypes lists a knowledge's chunks restricted to the
// given chunk types. ListChunksByKnowledgeID is text-only by design, so callers
// that also need summary / parent_text / image chunks come through here rather
// than widening that query underneath its existing callers.
func (r *chunkRepository) ListChunksByKnowledgeIDAndTypes(
	ctx context.Context, tenantID uint64, knowledgeID string, chunkTypes []types.ChunkType,
) ([]*types.Chunk, error) {
	if len(chunkTypes) == 0 {
		return nil, nil
	}
	var chunks []*types.Chunk
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND knowledge_id = ? AND chunk_type IN ?", tenantID, knowledgeID, chunkTypes).
		Order("chunk_index ASC").
		Find(&chunks).Error; err != nil {
		return nil, err
	}
	return chunks, nil
}

// ListPagedChunksByKnowledgeID lists chunks for a knowledge ID with pagination
func (r *chunkRepository) ListPagedChunksByKnowledgeID(ctx context.Context,
	tenantID uint64,
	knowledgeID string,
	page *types.Pagination,
	chunkType []types.ChunkType,
	tagIDs []string,
	keyword string,
	sortOrder string,
	isEnabled *bool) ([]*types.Chunk, int64, error) {
	var chunks []*types.Chunk
	var total int64
	keyword = strings.TrimSpace(keyword)

	baseFilter := func(db *gorm.DB) *gorm.DB {
		db = db.Where("tenant_id = ? AND knowledge_id = ? AND chunk_type IN (?) AND status in (?)",
			tenantID, knowledgeID, chunkType, []int{int(types.ChunkStatusIndexed), int(types.ChunkStatusDefault)})
		if len(tagIDs) > 0 {
			db = db.Where("tag_id IN ?", tagIDs)
		}
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

	// Determine sort order based on knowledge type
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
	tenantID uint64,
	parentID string,
) ([]*types.Chunk, error) {
	var chunks []*types.Chunk
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND parent_chunk_id = ?", tenantID, parentID).
		Find(&chunks).Error; err != nil {
		return nil, err
	}
	return chunks, nil
}

func (r *chunkRepository) ListChunksByParentIDs(
	ctx context.Context,
	tenantID uint64,
	parentIDs []string,
) ([]*types.Chunk, error) {
	if len(parentIDs) == 0 {
		return nil, nil
	}
	var chunks []*types.Chunk
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND parent_chunk_id IN ?", tenantID, parentIDs).
		Find(&chunks).Error; err != nil {
		return nil, err
	}
	return chunks, nil
}

// UpdateChunk updates a chunk using GORM Save, which updates ALL fields
// except SeqID (auto-increment, must not be overwritten).
// Make sure the chunk object is complete (e.g., fetched from DB) before calling this method.
func (r *chunkRepository) UpdateChunk(ctx context.Context, chunk *types.Chunk) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		changed, err := changedWikiSourceChunks(tx, []*types.Chunk{chunk})
		if err != nil {
			return err
		}
		if err := tx.Omit("SeqID").Save(chunk).Error; err != nil {
			return err
		}
		return markChangedWikiSourceChunks(ctx, tx, changed, time.Now())
	})
}

// Lock existing rows before comparing the fields consumed by Wiki generation.
// New chunks and changes to indexing or other metadata do not mark pages.
func changedWikiSourceChunks(tx *gorm.DB, chunks []*types.Chunk) (map[uint64][]string, error) {
	ids := make([]string, 0, len(chunks))
	targets := make(map[string]*types.Chunk, len(chunks))
	for _, chunk := range chunks {
		ids = append(ids, chunk.ID)
		targets[chunk.ID] = chunk
	}
	var existing []*types.Chunk
	if err := tx.Select("id", "tenant_id", "content", "is_enabled").
		Where("id IN ?", ids).Order("id").Clauses(clause.Locking{Strength: "UPDATE"}).
		Find(&existing).Error; err != nil {
		return nil, err
	}
	changed := make(map[uint64][]string)
	for _, old := range existing {
		target := targets[old.ID]
		if old.Content != common.CleanInvalidUTF8(target.Content) || old.IsEnabled != target.IsEnabled {
			changed[old.TenantID] = append(changed[old.TenantID], old.ID)
		}
	}
	return changed, nil
}

func markChangedWikiSourceChunks(ctx context.Context, tx *gorm.DB, changed map[uint64][]string, at time.Time) error {
	for tenantID, ids := range changed {
		if err := NewWikiPageRepository(tx).MarkSourceChanged(ctx, tenantID, ids, at); err != nil {
			return err
		}
	}
	return nil
}

func (r *chunkRepository) CreateChunkRevision(ctx context.Context, revision *types.ChunkRevision) error {
	return r.db.WithContext(ctx).Create(revision).Error
}

func (r *chunkRepository) SaveChunkRevision(
	ctx context.Context, chunk *types.Chunk, revision *types.ChunkRevision, expectedRevision int,
) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&types.Chunk{}).
			Where("id = ? AND tenant_id = ? AND content_revision = ?", chunk.ID, chunk.TenantID, expectedRevision).
			Updates(map[string]interface{}{
				"content":          common.CleanInvalidUTF8(chunk.Content),
				"source_content":   common.CleanInvalidUTF8(chunk.SourceContent),
				"content_revision": chunk.ContentRevision,
				"is_enabled":       chunk.IsEnabled,
				"metadata":         chunk.Metadata,
				"index_status":     chunk.IndexStatus,
				"last_editor_id":   chunk.LastEditorID,
				"updated_at":       chunk.UpdatedAt,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrChunkRevisionConflict
		}
		if err := tx.Create(revision).Error; err != nil {
			return err
		}
		if common.CleanInvalidUTF8(chunk.Content) != revision.Content || chunk.IsEnabled != revision.IsEnabled {
			return NewWikiPageRepository(tx).MarkSourceChanged(ctx, chunk.TenantID, []string{chunk.ID}, chunk.UpdatedAt)
		}
		return nil
	})
}

func (r *chunkRepository) ListChunkRevisions(
	ctx context.Context, tenantID uint64, chunkID string,
) ([]*types.ChunkRevision, error) {
	var revisions []*types.ChunkRevision
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND chunk_id = ?", tenantID, chunkID).
		Order("revision DESC").Find(&revisions).Error
	return revisions, err
}

func (r *chunkRepository) GetChunkRevision(
	ctx context.Context, tenantID uint64, chunkID string, revision int,
) (*types.ChunkRevision, error) {
	var item types.ChunkRevision
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND chunk_id = ? AND revision = ?", tenantID, chunkID, revision).
		First(&item).Error
	return &item, err
}

// SaveChunks persists full chunk objects in a single transaction using GORM Save (UPDATE).
func (r *chunkRepository) SaveChunks(ctx context.Context, chunks []*types.Chunk) error {
	if len(chunks) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		changed, err := changedWikiSourceChunks(tx, chunks)
		if err != nil {
			return err
		}
		for _, chunk := range chunks {
			if err := tx.Omit("SeqID").Save(chunk).Error; err != nil {
				return err
			}
		}
		return markChangedWikiSourceChunks(ctx, tx, changed, time.Now())
	})
}

// UpdateChunks updates chunks in batch using raw SQL for efficiency.
// Uses raw SQL to bypass GORM's default value handling for boolean fields.
//
// IMPORTANT: This method only updates the following fields:
//   - content
//   - is_enabled
//   - tag_id
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
	tagIDCases := make([]string, 0, len(chunks))
	flagsCases := make([]string, 0, len(chunks))
	statusCases := make([]string, 0, len(chunks))

	var contentArgs []interface{}
	var isEnabledArgs []interface{}
	var tagIDArgs []interface{}
	var flagsArgs []interface{}
	var statusArgs []interface{}

	for _, chunk := range chunks {
		ids = append(ids, chunk.ID)
		content := common.CleanInvalidUTF8(chunk.Content)

		contentCases = append(contentCases, "WHEN id = ? THEN ?")
		contentArgs = append(contentArgs, chunk.ID, content)

		// Convert bool to string for PostgreSQL compatibility
		isEnabledStr := "false"
		if chunk.IsEnabled {
			isEnabledStr = "true"
		}
		isEnabledCases = append(isEnabledCases, "WHEN id = ? THEN ?")
		isEnabledArgs = append(isEnabledArgs, chunk.ID, isEnabledStr)

		tagIDCases = append(tagIDCases, "WHEN id = ? THEN ?")
		tagIDArgs = append(tagIDArgs, chunk.ID, chunk.TagID)

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

	// Combine args in correct order: content, is_enabled, tag_id, flags, status, then IN clause
	var args []interface{}
	args = append(args, contentArgs...)
	args = append(args, isEnabledArgs...)
	args = append(args, tagIDArgs...)
	args = append(args, flagsArgs...)
	args = append(args, statusArgs...)
	for _, id := range ids {
		args = append(args, id)
	}

	sql := fmt.Sprintf(`
			UPDATE chunks SET
				content = CASE %s END,
				is_enabled = (CASE %s END)::boolean,
				tag_id = CASE %s END,
				flags = (CASE %s END)::integer,
				status = (CASE %s END)::integer,
				updated_at = NOW()
			WHERE id IN (%s)
		`,
		strings.Join(contentCases, " "),
		strings.Join(isEnabledCases, " "),
		strings.Join(tagIDCases, " "),
		strings.Join(flagsCases, " "),
		strings.Join(statusCases, " "),
		strings.Join(inPlaceholders, ","),
	)
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		changed, err := changedWikiSourceChunks(tx, chunks)
		if err != nil {
			return err
		}
		if err := tx.Exec(sql, args...).Error; err != nil {
			return err
		}
		return markChangedWikiSourceChunks(ctx, tx, changed, time.Now())
	})
}

// DeleteChunk deletes a chunk by its ID
func (r *chunkRepository) DeleteChunk(ctx context.Context, tenantID uint64, id string) error {
	return r.db.WithContext(ctx).Where("tenant_id = ? AND id = ?", tenantID, id).Delete(&types.Chunk{}).Error
}

// DeleteChunks deletes chunks by IDs in batch.
// To avoid MySQL Error 1390 (too many placeholders), IDs are split into batches.
func (r *chunkRepository) DeleteChunks(ctx context.Context, tenantID uint64, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	const batchSize = 5000
	for i := 0; i < len(ids); i += batchSize {
		end := i + batchSize
		if end > len(ids) {
			end = len(ids)
		}
		if err := r.db.WithContext(ctx).Where("tenant_id = ? AND id IN ?", tenantID, ids[i:end]).Delete(&types.Chunk{}).Error; err != nil {
			return err
		}
	}
	return nil
}

// DeleteChunksByKnowledgeID deletes all chunks for a knowledge ID
func (r *chunkRepository) DeleteChunksByKnowledgeID(ctx context.Context, tenantID uint64, knowledgeID string) error {
	return r.db.WithContext(ctx).Where(
		"tenant_id = ? AND knowledge_id = ?", tenantID, knowledgeID,
	).Delete(&types.Chunk{}).Error
}

// ListImageInfoByKnowledgeIDs returns non-empty image_info values for the given knowledge IDs.
// No chunk_type filter — collects from text, image_ocr, and image_caption chunks.
func (r *chunkRepository) ListImageInfoByKnowledgeIDs(
	ctx context.Context, tenantID uint64, knowledgeIDs []string,
) ([]interfaces.ChunkImageInfo, error) {
	var results []interfaces.ChunkImageInfo
	err := r.db.WithContext(ctx).
		Model(&types.Chunk{}).
		Select("knowledge_id, image_info").
		Where("tenant_id = ? AND knowledge_id IN ? AND image_info != ''", tenantID, knowledgeIDs).
		Scan(&results).Error
	return results, err
}

// DeleteByKnowledgeList deletes all chunks for a knowledge list
func (r *chunkRepository) DeleteByKnowledgeList(ctx context.Context, tenantID uint64, knowledgeIDs []string) error {
	return r.db.WithContext(ctx).Where(
		"tenant_id = ? AND knowledge_id in ?", tenantID, knowledgeIDs,
	).Delete(&types.Chunk{}).Error
}

// MoveChunksByKnowledgeID updates knowledge_base_id for all chunks of a knowledge item
func (r *chunkRepository) MoveChunksByKnowledgeID(ctx context.Context, tenantID uint64, knowledgeID string, targetKBID string) error {
	return r.db.WithContext(ctx).Model(&types.Chunk{}).
		Where("tenant_id = ? AND knowledge_id = ?", tenantID, knowledgeID).
		Update("knowledge_base_id", targetKBID).Error
}

// DeleteChunksByTagID deletes all chunks with the specified tag ID
// Returns the IDs of deleted chunks for index cleanup
func (r *chunkRepository) DeleteChunksByTagID(ctx context.Context, tenantID uint64, kbID string, tagID string, excludeIDs []string) ([]string, error) {
	// Build exclude set for O(1) lookup
	excludeSet := make(map[string]struct{}, len(excludeIDs))
	for _, id := range excludeIDs {
		excludeSet[id] = struct{}{}
	}

	// Get all chunk IDs for this tag
	var allIDs []string
	if err := r.db.WithContext(ctx).Model(&types.Chunk{}).
		Where("tenant_id = ? AND knowledge_base_id = ? AND tag_id = ?", tenantID, kbID, tagID).
		Pluck("id", &allIDs).Error; err != nil {
		return nil, err
	}

	// Filter out excluded IDs
	toDelete := make([]string, 0, len(allIDs))
	for _, id := range allIDs {
		if _, excluded := excludeSet[id]; !excluded {
			toDelete = append(toDelete, id)
		}
	}

	if len(toDelete) == 0 {
		return nil, nil
	}

	// Delete in batches
	const batchSize = 1000
	for i := 0; i < len(toDelete); i += batchSize {
		end := i + batchSize
		if end > len(toDelete) {
			end = len(toDelete)
		}
		batch := toDelete[i:end]

		if err := r.db.WithContext(ctx).Where("id IN ?", batch).Delete(&types.Chunk{}).Error; err != nil {
			// Return already planned deletions up to this point for index cleanup
			return toDelete[:i], err
		}
	}

	return toDelete, nil
}

// CountChunksByKnowledgeBaseID counts the number of chunks in a knowledge base
func (r *chunkRepository) CountChunksByKnowledgeBaseID(
	ctx context.Context,
	tenantID uint64,
	kbID string,
) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&types.Chunk{}).
		Where("tenant_id = ? AND knowledge_base_id = ?", tenantID, kbID).
		Count(&count).Error
	return count, err
}

// DeleteUnindexedChunks by knowledge id and chunk index range
func (r *chunkRepository) DeleteUnindexedChunks(
	ctx context.Context,
	tenantID uint64,
	knowledgeID string,
) ([]*types.Chunk, error) {
	var chunks []*types.Chunk
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND knowledge_id = ? AND status = ?", tenantID, knowledgeID, types.ChunkStatusStored).
		Find(&chunks).Error; err != nil {
		return nil, err
	}
	if len(chunks) > 0 {
		if err := r.db.WithContext(ctx).
			Where("tenant_id = ? AND knowledge_id = ? AND status = ?", tenantID, knowledgeID, types.ChunkStatusStored).
			Delete(&types.Chunk{}).Error; err != nil {
			return nil, err
		}
	}
	return chunks, nil
}

// UpdateChunkFlagsBatch updates flags for multiple chunks in batch using SQL CASE expressions.
// This is more efficient than updating chunks one by one.
// setFlags: map of chunk ID to flags to set (OR operation)
// clearFlags: map of chunk ID to flags to clear (AND NOT operation)
func (r *chunkRepository) UpdateChunkFlagsBatch(
	ctx context.Context,
	tenantID uint64,
	kbID string,
	setFlags map[string]types.ChunkFlags,
	clearFlags map[string]types.ChunkFlags,
) error {
	if len(setFlags) == 0 && len(clearFlags) == 0 {
		return nil
	}

	// Collect all IDs
	allIDs := make([]string, 0, len(setFlags)+len(clearFlags))
	for id := range setFlags {
		allIDs = append(allIDs, id)
	}
	for id := range clearFlags {
		if _, exists := setFlags[id]; !exists {
			allIDs = append(allIDs, id)
		}
	}

	if len(allIDs) == 0 {
		return nil
	}

	// Build CASE expression for flags update
	// flags = (flags | setFlag) & ~clearFlag
	var setCases, clearCases []string
	var args []interface{}

	// Build SET cases: flags | value
	for id, flag := range setFlags {
		setCases = append(setCases, "WHEN id = ? THEN ?")
		args = append(args, id, int(flag))
	}

	// Build CLEAR cases: flags & ~value
	for id, flag := range clearFlags {
		clearCases = append(clearCases, "WHEN id = ? THEN ?")
		args = append(args, id, int(flag))
	}

	setExpr := "0"
	clearExpr := "0"

	if len(setCases) > 0 {
		setExpr = fmt.Sprintf("CASE %s ELSE 0 END", strings.Join(setCases, " "))
	}

	if len(clearCases) > 0 {
		clearExpr = fmt.Sprintf("CASE %s ELSE 0 END", strings.Join(clearCases, " "))
	}

	// Build IN clause placeholders manually for raw SQL
	inPlaceholders := make([]string, len(allIDs))
	for i := range allIDs {
		inPlaceholders[i] = "?"
	}

	nowFunc := "NOW()"
	sql := fmt.Sprintf(`
	UPDATE chunks
    SET flags = (flags | (%s)) & ~(%s),
        updated_at = %s
    WHERE tenant_id = ?
      AND knowledge_base_id = ?
      AND id IN (%s)
`, setExpr, clearExpr, nowFunc, strings.Join(inPlaceholders, ","))

	args = append(args, tenantID, kbID)
	for _, id := range allIDs {
		args = append(args, id)
	}

	return r.db.WithContext(ctx).Exec(sql, args...).Error
}

// ListRecentDocumentChunksWithQuestions lists recent document chunks that have generated questions.
// Filter by kbIDs and/or knowledgeIDs (OR relationship). At least one must be non-empty.
// Returns up to `limit` chunks sorted by updated_at descending.
func (r *chunkRepository) ListRecentDocumentChunksWithQuestions(
	ctx context.Context,
	tenantID uint64,
	kbIDs []string,
	knowledgeIDs []string,
	limit int,
) ([]*types.Chunk, error) {
	if limit <= 0 {
		limit = 10
	}
	if len(kbIDs) == 0 && len(knowledgeIDs) == 0 {
		return nil, nil
	}
	var chunks []*types.Chunk

	baseQuery := r.db.WithContext(ctx).
		Select("id, knowledge_id, knowledge_base_id, chunk_type, metadata, updated_at").
		Where("tenant_id = ? AND chunk_type = ? AND status IN ? AND is_enabled = ?",
			tenantID, types.ChunkTypeText, []int{int(types.ChunkStatusIndexed), int(types.ChunkStatusDefault)}, true)

	if len(kbIDs) > 0 && len(knowledgeIDs) > 0 {
		baseQuery = baseQuery.Where("knowledge_base_id IN ? OR knowledge_id IN ?", kbIDs, knowledgeIDs)
	} else if len(knowledgeIDs) > 0 {
		// 指定了具体知识文档，直接按 knowledge_id 过滤（忽略 kbIDs）
		baseQuery = baseQuery.Where("knowledge_id IN ?", knowledgeIDs)
	} else if len(kbIDs) > 0 {
		baseQuery = baseQuery.Where("knowledge_base_id IN ?", kbIDs)
	}

	orderClause := "RANDOM()"

	if err := baseQuery.
		Where("metadata IS NOT NULL AND metadata::text != '{}' AND jsonb_array_length(COALESCE(metadata->'generated_questions', '[]'::jsonb)) > 0").
		Order(orderClause).Limit(limit).Find(&chunks).Error; err != nil {
		return nil, err
	}

	return chunks, nil
}
