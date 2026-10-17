package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"reliquary/internal/application/graph"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"reliquary/internal/utils"
)

var ErrEntryNotFound = errors.New("entry not found")

// likeEscapeChar is the SQL ESCAPE character paired with escapeLikeKeyword.
const likeEscapeChar = `\`

// escapeLikeKeyword escapes SQL LIKE wildcards (%, _) in a keyword
// so they are treated as literal characters.
func escapeLikeKeyword(keyword string) string {
	keyword = strings.ReplaceAll(keyword, `\`, `\\`)
	keyword = strings.ReplaceAll(keyword, "%", `\%`)
	keyword = strings.ReplaceAll(keyword, "_", `\_`)
	return keyword
}

// omitFieldsOnUpdate defines fields to omit when updating entry.
//
// PendingSubtasksCount is deliberately omitted from every full-row Save:
// it is an orchestration counter owned exclusively by the atomic helpers
// SetFinalizing (seed), FinalizeSubtask (decrement+promote) and the
// explicit UpdateEntryColumns resets (cancel/reparse). A generic
// UpdateEntry call persists the WHOLE in-memory struct, so any
// concurrent enrichment subtask that loaded the row, did slow work
// (e.g. an LLM call), then saved an unrelated field would otherwise
// write back the STALE counter it read at load time — clobbering the
// decrements other subtasks performed in the meantime. That made the
// counter jump back up and never reach zero (the "stuck
// pending_subtasks_count / never promoted to completed" bug). Omitting
// the column here means Save can never touch it.
// User star choices, collection reasons and the correction marker are owned by dedicated
// editing methods, so background snapshots cannot overwrite those columns.
var omitFieldsOnUpdate = []string{"SourceRef", "SourceBodyHash", "Identifier", "Graph", "DeletedAt", "PendingSubtasksCount", "Starred", "CollectReason", "ContentTypeCorrected", "SourceUnverified",
	"Acquisition", "Incomplete", "ExternalCompletionAttempted", "MissingMaterials", "ContentTypeHint", "RelatedEntryID", "IngestAPIKeyID", "IngestAPIKeyName", "LastIngestAPIKeyID", "LastIngestAPIKeyName"}

// entryRepository implements library and entry repository interface
type entryRepository struct {
	db *gorm.DB
}

// NewEntryRepository creates a new entry repository
func NewEntryRepository(db *gorm.DB) interfaces.EntryRepository {
	return &entryRepository{db: db}
}

// CreateEntry creates entry
func (r *entryRepository) CreateEntry(ctx context.Context, entry *types.Entry) error {
	entry.ErrorMessage = utils.CleanInvalidUTF8(entry.ErrorMessage)
	err := r.db.WithContext(ctx).Create(entry).Error
	return err
}

// GetEntryByID gets entry
func (r *entryRepository) GetEntryByID(
	ctx context.Context,
	spaceID uint64,
	id string,
) (*types.Entry, error) {
	var entry types.Entry
	if err := r.db.WithContext(ctx).Where("space_id = ? AND id = ?", spaceID, id).First(&entry).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrEntryNotFound
		}
		return nil, err
	}
	return &entry, nil
}

// GetEntryByIDOnly returns entry by ID without space filter (for permission resolution).
func (r *entryRepository) GetEntryByIDOnly(ctx context.Context, id string) (*types.Entry, error) {
	var entry types.Entry
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&entry).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrEntryNotFound
		}
		return nil, err
	}
	return &entry, nil
}

// ListEntryByLibraryID lists all entry in a library
func (r *entryRepository) ListEntryByLibraryID(
	ctx context.Context, spaceID uint64, libraryID string,
) ([]*types.Entry, error) {
	var entries []*types.Entry
	if err := r.db.WithContext(ctx).Where("space_id = ? AND library_id = ?", spaceID, libraryID).
		Order("created_at DESC").Find(&entries).Error; err != nil {
		return nil, err
	}
	return entries, nil
}

// applyEntryListFilter applies the optional filter dimensions of
// EntryListFilter to a GORM query. Space / library scoping must be
// applied by the caller before invoking this helper.
func applyEntryListFilter(query *gorm.DB, filter types.EntryListFilter) *gorm.DB {
	if len(filter.LibraryIDs) > 0 {
		query = query.Where("library_id IN ?", filter.LibraryIDs)
	}
	if filter.SourceKind != "" {
		query = query.Where("source_kind = ?", filter.SourceKind)
	}
	if filter.Starred != nil {
		query = query.Where("source_kind IN ('article','mine') AND starred=?", *filter.Starred)
	}
	if filter.Incomplete != nil {
		query = query.Where("incomplete = ?", *filter.Incomplete)
	}
	if filter.ContentType != "" {
		query = query.Where("content_type = ?", filter.ContentType)
	}
	if filter.Keyword != "" {
		// Case-insensitive (LOWER … LIKE LOWER) so keyword search matches
		// regardless of the stored casing — consistent with the sibling
		// LOWER() filters in this file and with the client-side `search library`
		// and article filters. Plain LIKE is case-sensitive in
		// Postgres, which surprised callers searching with lowercase.
		escaped := strings.ToLower(escapeLikeKeyword(filter.Keyword))
		query = query.Where("(LOWER(file_name) LIKE ? OR LOWER(title) LIKE ?)", "%"+escaped+"%", "%"+escaped+"%")
	}
	if filter.FileType != "" {
		switch strings.ToLower(filter.FileType) {
		case "md", "markdown", "manual", "url":
			query = query.Where("(LOWER(file_type) IN ? OR (COALESCE(file_type, '') = '' AND type IN ?))",
				[]string{"md", "markdown", "manual", "url"}, []string{"manual", "url"})
		default:
			query = query.Where("LOWER(file_type) = ?", strings.ToLower(filter.FileType))
		}
	}
	if filter.Source != "" {
		query = query.Where("channel = ?", filter.Source)
	}
	if filter.ParseStatus != "" {
		query = query.Where("parse_status = ?", filter.ParseStatus)
	} else {
		// Hide rows that are mid-deletion so an async delete never lingers in the
		// document list as if it were a normal entry (issue #2192). The delete
		// pipeline marks the row `deleting` before tearing down its resources; a
		// row whose delete task exhausts its retries is flipped to `failed` by the
		// dead-letter callback and stays visible so the failure remains actionable.
		query = query.Where("parse_status NOT IN ?", []string{types.ParseStatusDeleting, types.ParseStatusRejected})
	}
	if !filter.UpdatedFrom.IsZero() {
		query = query.Where("updated_at >= ?", filter.UpdatedFrom)
	}
	if !filter.UpdatedTo.IsZero() {
		query = query.Where("updated_at <= ?", filter.UpdatedTo)
	}
	return query
}

// ListPagedEntryByLibraryID lists all entry in a library with pagination
func (r *entryRepository) ListPagedEntryByLibraryID(
	ctx context.Context,
	spaceID uint64,
	libraryID string,
	page *types.Pagination,
	filter types.EntryListFilter,
) ([]*types.Entry, int64, error) {
	var entries []*types.Entry
	var total int64

	scope := func(q *gorm.DB) *gorm.DB {
		return applyEntryListFilter(
			func() *gorm.DB {
				q = q.Where("space_id=?", spaceID)
				if libraryID != "" {
					q = q.Where("library_id=?", libraryID)
				}
				return q
			}(),
			filter,
		)
	}

	if err := scope(r.db.WithContext(ctx).Model(&types.Entry{})).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	query := scope(r.db.WithContext(ctx)).Order("created_at DESC, id DESC")
	if page != nil {
		query = query.Offset(page.Offset()).Limit(page.Limit())
	}
	if err := query.Find(&entries).Error; err != nil {
		return nil, 0, err
	}

	return entries, total, nil
}

// UpdateEntry updates entry
func (r *entryRepository) UpdateEntry(ctx context.Context, entry *types.Entry) error {
	if err := r.checkEntryLibrary(ctx, entry); err != nil {
		return err
	}
	entry.ErrorMessage = utils.CleanInvalidUTF8(entry.ErrorMessage)
	omit := omitFieldsOnUpdate
	// Legacy/unit-test schemas created before custom_metadata should continue
	// to support unrelated updates when the caller did not provide the field.
	if entry.CustomMetadata == nil {
		omit = append(append([]string{}, omitFieldsOnUpdate...), "custom_metadata")
	}
	err := r.db.WithContext(ctx).Omit(omit...).Save(entry).Error
	return err
}

// UpdateEntryBatch updates entry items in batch
func (r *entryRepository) UpdateEntryBatch(ctx context.Context, entryList []*types.Entry) error {
	if len(entryList) == 0 {
		return nil
	}
	for _, entry := range entryList {
		if entry != nil {
			if err := r.checkEntryLibrary(ctx, entry); err != nil {
				return err
			}
			entry.ErrorMessage = utils.CleanInvalidUTF8(entry.ErrorMessage)
		}
	}
	return r.db.Debug().WithContext(ctx).Omit(omitFieldsOnUpdate...).Save(entryList).Error
}

// DeleteEntry deletes an article and its entity associations atomically.
func (r *entryRepository) DeleteEntry(ctx context.Context, spaceID uint64, id string) error {
	return r.DeleteEntryList(ctx, spaceID, []string{id})
}

// DeleteEntryList locks articles before deleting mentions, excluding concurrent resolution.
func (r *entryRepository) DeleteEntryList(ctx context.Context, spaceID uint64, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := graph.Lock(tx, spaceID); err != nil {
			return err
		}
		var articles []types.Entry
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("space_id=? AND id IN ?", spaceID, ids).Order("id").Find(&articles).Error; err != nil {
			return err
		}
		if err := tx.Where("space_id=? AND entry_id IN ?", spaceID, ids).Delete(&types.EntryEntity{}).Error; err != nil {
			return err
		}
		if err := tx.Where("space_id=? AND entry_id IN ?", spaceID, ids).Delete(&types.EntryTopic{}).Error; err != nil {
			return err
		}
		return graph.DeleteEntriesTx(tx, spaceID, ids, false)
	})
}

// GetEntryBatch gets entry in batch
func (r *entryRepository) GetEntryBatch(
	ctx context.Context, spaceID uint64, ids []string,
) ([]*types.Entry, error) {
	var entry []*types.Entry
	if err := r.db.WithContext(ctx).Debug().
		Where("space_id = ? AND id IN ?", spaceID, ids).
		Find(&entry).Error; err != nil {
		return nil, err
	}
	return entry, nil
}

// CheckEntryExists checks if entry already exists
func (r *entryRepository) CheckEntryExists(
	ctx context.Context,
	spaceID uint64,
	libraryID string,
	params *types.EntryCheckParams,
) (bool, *types.Entry, error) {
	query := r.db.WithContext(ctx).Model(&types.Entry{}).
		Where("space_id = ? AND library_id = ? AND parse_status <> ?", spaceID, libraryID, "failed")

	switch params.Type {
	case "file":
		// File content is only a duplicate within the same file type. This keeps
		// same-content documents with distinct formats (for example, .md and
		// .txt) available as separate entry items.
		if params.FileHash != "" {
			var entry types.Entry
			duplicateQuery := query.Where("type = ? AND file_hash = ?", "file", params.FileHash)
			if params.FileType != "" {
				duplicateQuery = duplicateQuery.Where("LOWER(file_type) = ?", strings.ToLower(params.FileType))
			}
			err := duplicateQuery.First(&entry).Error
			if err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return false, nil, nil
				}
				return false, nil, err
			}
			return true, &entry, nil
		}

		// If no hash or hash doesn't match, use filename, size, and file type.
		if params.FileName != "" && params.FileSize > 0 {
			var entry types.Entry
			duplicateQuery := query.Where(
				"type = ? AND file_name = ? AND file_size = ?",
				"file", params.FileName, params.FileSize,
			)
			if params.FileType != "" {
				duplicateQuery = duplicateQuery.Where("LOWER(file_type) = ?", strings.ToLower(params.FileType))
			}
			err := duplicateQuery.First(&entry).Error
			if err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return false, nil, nil
				}
				return false, nil, err
			}
			return true, &entry, nil
		}
	case "url":
		// If file hash exists, prioritize exact match using hash
		if params.FileHash != "" {
			var entry types.Entry
			err := query.Where("type = 'url' AND file_hash = ?", params.FileHash).First(&entry).Error
			if err == nil && entry.ID != "" {
				return true, &entry, nil
			}
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return false, nil, err
			}
		}

		if params.URL != "" {
			var entry types.Entry
			err := query.Where("type = 'url' AND source = ?", params.URL).First(&entry).Error
			if err == nil && entry.ID != "" {
				return true, &entry, nil
			}
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return false, nil, err
			}
		}
		return false, nil, nil
	}

	// No valid parameters, default to not existing
	return false, nil, nil
}

func (r *entryRepository) UpdateEntryColumn(
	ctx context.Context,
	id string,
	column string,
	value interface{},
) error {
	if column == "library_id" || column == "LibraryID" {
		return fmt.Errorf("entry library_id is immutable")
	}
	if column == "error_message" {
		switch v := value.(type) {
		case string:
			value = utils.CleanInvalidUTF8(v)
		case []byte:
			value = utils.CleanInvalidUTF8(string(v))
		}
	}
	err := r.db.WithContext(ctx).Model(&types.Entry{}).Where("id = ?", id).Update(column, value).Error
	return err
}

// UpdateEntryColumns writes multiple columns in a single UPDATE so callers
// that flip related fields together (parse_status + error_message after
// dead-letter, for example) cannot leave the row half-updated when the second
// write fails.
func (r *entryRepository) UpdateEntryColumns(
	ctx context.Context,
	id string,
	values map[string]interface{},
) error {
	if _, ok := values["library_id"]; ok {
		return fmt.Errorf("entry library_id is immutable")
	}
	if _, ok := values["LibraryID"]; ok {
		return fmt.Errorf("entry library_id is immutable")
	}
	if len(values) == 0 {
		return nil
	}
	if value, ok := values["error_message"]; ok {
		switch v := value.(type) {
		case string:
			values["error_message"] = utils.CleanInvalidUTF8(v)
		case []byte:
			values["error_message"] = utils.CleanInvalidUTF8(string(v))
		}
	}
	return r.db.WithContext(ctx).Model(&types.Entry{}).Where("id = ?", id).Updates(values).Error
}

// UpdateActiveDeletingEntryColumns only touches rows that are still visible
// to normal queries and have not moved out of the transient deleting state.
func (r *entryRepository) UpdateActiveDeletingEntryColumns(
	ctx context.Context,
	id string,
	values map[string]interface{},
) (bool, error) {
	if _, ok := values["library_id"]; ok {
		return false, fmt.Errorf("entry library_id is immutable")
	}
	if _, ok := values["LibraryID"]; ok {
		return false, fmt.Errorf("entry library_id is immutable")
	}
	if len(values) == 0 {
		return false, nil
	}
	result := r.db.WithContext(ctx).
		Model(&types.Entry{}).
		Where("id = ? AND parse_status = ?", id, types.ParseStatusDeleting).
		Updates(values)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// FinalizeSubtask atomically decrements pending_subtasks_count and, when
// the counter reaches zero while parse_status is still 'finalizing',
// flips the row to 'completed' in the same statement so concurrent
// subtask completions can't race the promotion. Both this promotion and
// SetFinalizing clear error_message: a row that re-enters processing or
// finishes successfully must not keep displaying a failure from a
// previous attempt.
//
// Returns (newCount, promoted, error). promoted is true iff this caller
// was the one whose UPDATE flipped 'finalizing'→'completed'.
//
// The implementation is two statements (atomic decrement, then a guarded
// promote UPDATE) because GORM does not expose a portable RETURNING
// across PostgreSQL. The promote UPDATE's WHERE clause
// (parse_status='finalizing' AND pending_subtasks_count=0) makes it
// safe to run from any number of concurrent callers — at most one wins.
func (r *entryRepository) FinalizeSubtask(
	ctx context.Context, id string,
) (int, bool, error) {
	now := time.Now()
	// 1) Atomic decrement, clamped at zero. The `pending_subtasks_count > 0`
	//    guard is purely a safety net for accounting bugs — under normal
	//    operation each subtask handler decrements at most once per task,
	//    so the counter cannot go negative.
	res := r.db.WithContext(ctx).Model(&types.Entry{}).
		Where("id = ? AND pending_subtasks_count > 0", id).
		Updates(map[string]interface{}{
			"pending_subtasks_count": gorm.Expr("pending_subtasks_count - 1"),
			"updated_at":             now,
		})
	if res.Error != nil {
		return 0, false, res.Error
	}

	// 2) Guarded promote. EVERY caller unconditionally attempts this after
	//    decrementing — we must NOT gate it on a separate SELECT of the
	//    counter. That read can be served by a lagging read-replica (or a
	//    stale connection snapshot) and return a non-zero value even after
	//    the counter has truly reached zero on the primary; if every caller
	//    trusts that stale read, NONE of them runs the promote and the row
	//    is stranded in `finalizing` forever (the observed "stuck
	//    pending_subtasks_count" bug). The promote is a WRITE, so it executes
	//    on the primary and its `pending_subtasks_count = 0` WHERE clause is
	//    the single authoritative, atomic check on the live row: only the
	//    caller whose decrement actually brought the counter to zero matches,
	//    and cancel/delete cannot be clobbered by a late promote.
	promoteRes := r.db.WithContext(ctx).Model(&types.Entry{}).
		Where("id = ? AND parse_status = ? AND pending_subtasks_count = 0",
			id, types.ParseStatusFinalizing).
		Updates(map[string]interface{}{
			"parse_status":  types.ParseStatusCompleted,
			"error_message": "",
			"processed_at":  now,
			"updated_at":    now,
		})
	if promoteRes.Error != nil {
		return 0, false, promoteRes.Error
	}
	promoted := promoteRes.RowsAffected > 0

	// 3) Best-effort re-read of the new count for diagnostics/return value
	//    only. This read may be replica-stale and is intentionally NOT used
	//    to decide whether to promote (see above). A read failure here does
	//    not affect correctness, so we don't propagate it as an error.
	var snap struct {
		PendingSubtasksCount int `gorm:"column:pending_subtasks_count"`
	}
	if err := r.db.WithContext(ctx).Model(&types.Entry{}).
		Select("pending_subtasks_count").
		Where("id = ?", id).Take(&snap).Error; err != nil {
		return 0, promoted, nil
	}
	return snap.PendingSubtasksCount, promoted, nil
}

// SetFinalizing atomically transitions a row from 'processing' to
// 'finalizing' and seeds the generic pending_subtasks_count field.
//
// The transition is conditional on parse_status='processing' so a row
// that the user cancelled or deleted cannot be moved into finalizing.
// Returns whether the transition happened.
func (r *entryRepository) SetFinalizing(
	ctx context.Context, id string, expectedSubtasks int,
) (bool, error) {
	if expectedSubtasks < 0 {
		expectedSubtasks = 0
	}
	now := time.Now()
	res := r.db.WithContext(ctx).Model(&types.Entry{}).
		Where("id = ? AND parse_status = ?", id, types.ParseStatusProcessing).
		Updates(map[string]interface{}{
			"parse_status":           types.ParseStatusFinalizing,
			"pending_subtasks_count": expectedSubtasks,
			"error_message":          "",
			"updated_at":             now,
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// CountEntryByLibraryID counts the number of entry items in a library
func (r *entryRepository) CountEntryByLibraryID(
	ctx context.Context,
	spaceID uint64,
	libraryID string,
) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&types.Entry{}).
		Where("space_id = ? AND library_id = ?", spaceID, libraryID).
		Count(&count).Error
	return count, err
}

// CountEntryByStatus counts the number of entry items with the specified parse status
func (r *entryRepository) CountEntryByStatus(
	ctx context.Context,
	spaceID uint64,
	libraryID string,
	parseStatuses []string,
) (int64, error) {
	if len(parseStatuses) == 0 {
		return 0, nil
	}

	var count int64
	query := r.db.WithContext(ctx).Model(&types.Entry{}).
		Where("space_id = ? AND library_id = ?", spaceID, libraryID).
		Where("parse_status IN ?", parseStatuses)

	if err := query.Count(&count).Error; err != nil {
		return 0, err
	}

	return count, nil
}

// HardDeleteEntry physically removes a entry row. Call it AFTER
// DeleteEntry's soft-delete cascade so sync-internal deletions never
// become tombstones that block a later re-sync of the same external item.
func (r *entryRepository) HardDeleteEntry(ctx context.Context, spaceID uint64, id string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return graph.DeleteEntriesTx(tx, spaceID, []string{id}, true) })
}

func (r *entryRepository) SearchEntry(
	ctx context.Context,
	spaceID uint64,
	keyword string,
	offset, limit int,
	fileTypes []string,
) ([]*types.Entry, bool, error) {
	// Use raw query to properly map library_name
	type EntryWithLibraryName struct {
		types.Entry
		LibraryName string `gorm:"column:library_name"`
	}

	var results []EntryWithLibraryName
	query := r.db.WithContext(ctx).
		Table("entries").
		Select("entries.*, libraries.name as library_name").
		Joins("JOIN libraries ON libraries.id = entries.library_id").
		Where("entries.space_id = ?", spaceID).
		Where("libraries.type = ?", types.LibraryTypeDocument).
		Where("entries.deleted_at IS NULL").
		Where("entries.parse_status <> ?", types.ParseStatusRejected)

	// If keyword is provided, filter by file_name or title (case-insensitive).
	if keyword != "" {
		escaped := strings.ToLower(escapeLikeKeyword(keyword))
		query = query.Where("(LOWER(entries.file_name) LIKE ? OR LOWER(entries.title) LIKE ?)", "%"+escaped+"%", "%"+escaped+"%")
	}

	// If fileTypes is provided, filter by file extension or type
	if len(fileTypes) > 0 {
		seen := make(map[string]bool)
		var uniquePatterns []string
		includeURL := false
		for _, ft := range fileTypes {
			ft = strings.ToLower(strings.TrimPrefix(ft, "."))
			if ft == "url" || ft == "html" {
				includeURL = true
				continue
			}
			pattern := "%." + ft
			if !seen[pattern] {
				seen[pattern] = true
				uniquePatterns = append(uniquePatterns, pattern)
			}
			// Handle common aliases
			var aliases []string
			switch ft {
			case "xlsx":
				aliases = []string{"%.xls"}
			case "xls":
				aliases = []string{"%.xlsx"}
			case "docx":
				aliases = []string{"%.doc"}
			case "doc":
				aliases = []string{"%.docx"}
			case "jpg":
				aliases = []string{"%.jpeg", "%.png"}
			case "jpeg":
				aliases = []string{"%.jpg", "%.png"}
			case "png":
				aliases = []string{"%.jpg", "%.jpeg"}
			}
			for _, alias := range aliases {
				if !seen[alias] {
					seen[alias] = true
					uniquePatterns = append(uniquePatterns, alias)
				}
			}
		}
		var orConditions []string
		var args []interface{}
		for _, p := range uniquePatterns {
			orConditions = append(orConditions, "LOWER(entries.file_name) LIKE ?")
			args = append(args, p)
		}
		if includeURL {
			orConditions = append(orConditions, "entries.type = ?")
			args = append(args, "url")
		}
		if len(orConditions) > 0 {
			query = query.Where("("+strings.Join(orConditions, " OR ")+")", args...)
		}
	}

	// Fetch limit+1 to check if there are more results
	err := query.Order("entries.created_at DESC").
		Offset(offset).
		Limit(limit + 1).
		Scan(&results).Error
	if err != nil {
		return nil, false, err
	}

	// Check if there are more results
	hasMore := len(results) > limit
	if hasMore {
		results = results[:limit]
	}

	// Convert to []*types.Entry
	entries := make([]*types.Entry, len(results))
	for i, r := range results {
		k := r.Entry
		k.LibraryName = r.LibraryName
		entries[i] = &k
	}
	return entries, hasMore, nil
}

// SearchEntryInScopes searches articles inside explicit space and library boundaries.
func (r *entryRepository) SearchEntryInScopes(
	ctx context.Context,
	scopes []types.SearchScope,
	keyword string,
	offset, limit int,
	fileTypes []string,
) ([]*types.Entry, bool, int64, error) {
	if len(scopes) == 0 {
		return nil, false, 0, nil
	}

	type EntryWithLibraryName struct {
		types.Entry
		LibraryName string `gorm:"column:library_name"`
	}

	placeholders := make([]string, len(scopes))
	args := make([]interface{}, 0, len(scopes)*2)
	for i, s := range scopes {
		placeholders[i] = "(?,?)"
		args = append(args, s.SpaceID, s.LibraryID)
	}
	scopeCondition := "(entries.space_id, entries.library_id) IN (" + strings.Join(placeholders, ",") + ")"

	query := r.db.WithContext(ctx).
		Table("entries").
		Select("entries.*, libraries.name as library_name").
		Joins("JOIN libraries ON libraries.id = entries.library_id AND libraries.space_id = entries.space_id").
		Where(scopeCondition, args...).
		Where("libraries.type = ?", types.LibraryTypeDocument).
		Where("entries.deleted_at IS NULL").
		Where("entries.parse_status <> ?", types.ParseStatusRejected)

	if keyword != "" {
		escaped := strings.ToLower(escapeLikeKeyword(keyword))
		query = query.Where("(LOWER(entries.file_name) LIKE ? OR LOWER(entries.title) LIKE ?)", "%"+escaped+"%", "%"+escaped+"%")
	}

	if len(fileTypes) > 0 {
		seen := make(map[string]bool)
		var uniquePatterns []string
		includeURL := false
		for _, ft := range fileTypes {
			ft = strings.ToLower(strings.TrimPrefix(ft, "."))
			if ft == "url" || ft == "html" {
				includeURL = true
				continue
			}
			pattern := "%." + ft
			if !seen[pattern] {
				seen[pattern] = true
				uniquePatterns = append(uniquePatterns, pattern)
			}
			var aliases []string
			switch ft {
			case "xlsx":
				aliases = []string{"%.xls"}
			case "xls":
				aliases = []string{"%.xlsx"}
			case "docx":
				aliases = []string{"%.doc"}
			case "doc":
				aliases = []string{"%.docx"}
			case "jpg":
				aliases = []string{"%.jpeg", "%.png"}
			case "jpeg":
				aliases = []string{"%.jpg", "%.png"}
			case "png":
				aliases = []string{"%.jpg", "%.jpeg"}
			}
			for _, alias := range aliases {
				if !seen[alias] {
					seen[alias] = true
					uniquePatterns = append(uniquePatterns, alias)
				}
			}
		}
		var orConditions []string
		var ftArgs []interface{}
		for _, p := range uniquePatterns {
			orConditions = append(orConditions, "LOWER(entries.file_name) LIKE ?")
			ftArgs = append(ftArgs, p)
		}
		if includeURL {
			orConditions = append(orConditions, "entries.type = ?")
			ftArgs = append(ftArgs, "url")
		}
		if len(orConditions) > 0 {
			query = query.Where("("+strings.Join(orConditions, " OR ")+")", ftArgs...)
		}
	}

	var total int64
	if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, false, 0, err
	}

	var results []EntryWithLibraryName
	err := query.Order("entries.created_at DESC").
		Offset(offset).
		Limit(limit + 1).
		Scan(&results).Error
	if err != nil {
		return nil, false, 0, err
	}

	hasMore := len(results) > limit
	if hasMore {
		results = results[:limit]
	}

	entries := make([]*types.Entry, len(results))
	for i, r := range results {
		k := r.Entry
		k.LibraryName = r.LibraryName
		entries[i] = &k
	}
	return entries, hasMore, total, nil
}

func (r *entryRepository) checkEntryLibrary(ctx context.Context, entry *types.Entry) error {
	var previous types.Entry
	if err := r.db.WithContext(ctx).Unscoped().Select("id", "library_id").First(&previous, "id=?", entry.ID).Error; err != nil {
		return err
	}
	if entry.LibraryID != previous.LibraryID {
		return fmt.Errorf("entry library_id is immutable")
	}
	return nil
}
