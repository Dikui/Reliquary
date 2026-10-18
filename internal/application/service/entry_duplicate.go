package service

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reliquary/internal/types"
)

const minimumSimHashRunes = 100

func normalizeRepostName(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(name)), " "))
}

// normalizeMessageURL gives repeated deliveries a stable key. Query values are
// preserved because they can identify different articles.
func normalizeMessageURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(strings.TrimRight(u.Host, "."))
	if (u.Scheme == "http" && u.Port() == "80") || (u.Scheme == "https" && u.Port() == "443") {
		u.Host = u.Hostname()
	}
	u.Fragment, u.RawPath = "", ""
	u.Path = strings.TrimRight(strings.ToLower(u.Path), "/")
	query := u.Query()
	for key := range query {
		lower := strings.ToLower(key)
		if strings.HasPrefix(lower, "utm_") || strings.HasPrefix(lower, "mc_") ||
			lower == "fbclid" || lower == "gclid" || lower == "dclid" || lower == "msclkid" ||
			lower == "igshid" || lower == "ref" || lower == "ref_src" || lower == "_hsenc" || lower == "_hsmi" {
			delete(query, key)
		}
	}
	u.RawQuery = query.Encode()
	return u.String()
}

func messageBodyFingerprint(body string) (string, int64, int) {
	normalized := strings.Join(strings.Fields(strings.ReplaceAll(body, "\r\n", "\n")), " ")
	if normalized == "" {
		return "", 0, 0
	}
	hash := sha256.Sum256([]byte(normalized))
	runes := []rune(strings.ToLower(normalized))
	var weights [64]int
	// Character shingles also cover Chinese and other text without word spaces.
	for i := 0; i+3 <= len(runes); i++ {
		feature := string(runes[i : i+3])
		if strings.IndexFunc(feature, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsNumber(r) }) < 0 {
			continue
		}
		// Short FNV shingles share high bits and collapse unrelated English bodies.
		featureHash := sha256.Sum256([]byte(feature))
		value := binary.BigEndian.Uint64(featureHash[:8])
		for bit := range weights {
			if value&(uint64(1)<<bit) != 0 {
				weights[bit]++
			} else {
				weights[bit]--
			}
		}
	}
	var simhash uint64
	for bit, weight := range weights {
		if weight > 0 {
			simhash |= uint64(1) << bit
		}
	}
	return hex.EncodeToString(hash[:]), int64(simhash), len(runes)
}

func messageSimHashThreshold() int {
	value, err := strconv.Atoi(os.Getenv("RELIQUARY_DUPLICATE_SIMHASH_DISTANCE"))
	if err != nil || value < 0 || value > 16 {
		return 3
	}
	return value
}

func setMessageFingerprint(k *types.Entry, body string) {
	if intake, _ := k.IntakeMetadata(); intake != nil {
		k.NormalizedURL = normalizeMessageURL(intake.SourceInfo.OriginalURL)
	}
	if k.NormalizedURL == "" {
		k.NormalizedURL = normalizeMessageURL(k.Source)
	}
	if body != "" {
		k.ContentHash, k.ContentSimHash, k.ContentRuneCount = messageBodyFingerprint(body)
	}
}

func hydrateMessageSource(k *types.Entry, metadata map[string]string) error {
	intake, err := k.IntakeMetadata()
	if err != nil || intake == nil {
		return err
	}
	if intake.SourceInfo.Author == "" {
		intake.SourceInfo.Author = metadata["author"]
	}
	if intake.SourceInfo.Publisher == "" {
		intake.SourceInfo.Publisher = metadata["publisher"]
	}
	if k.SourceKind != "mine" && intake.SourceInfo.OriginalURL == "" {
		intake.SourceInfo.OriginalURL = metadata["original_url"]
	}
	if intake.SourceInfo.PublishedAt == "" {
		intake.SourceInfo.PublishedAt = metadata["published_at"]
	}
	return k.SetIntakeMetadata(intake)
}

func findMessageDuplicate(tx *gorm.DB, k *types.Entry, earlierPendingOnly ...bool) (*types.Entry, string, int, error) {
	rules, err := messageTemplateRules(tx, k)
	if err != nil {
		return nil, "", 0, err
	}
	if !rules.Identity.Body || (rules.Identity.SourceRef && k.SourceRef != "") {
		return nil, "", 0, nil
	}
	query := tx.Model(&types.Entry{}).Where("space_id = ? AND library_id = ? AND id <> ?", k.SpaceID, k.LibraryID, k.ID).
		Where("parse_status NOT IN ?", []string{types.ParseStatusFailed, types.ManualEntryStatusDraft, types.ParseStatusDeleting, types.ParseStatusCancelled})
	// Parser workers can see several already-persisted pending messages. A
	// stable owner order prevents them from choosing and then deleting each
	// other while duplicate cleanup happens outside the fingerprint transaction.
	if len(earlierPendingOnly) > 0 && earlierPendingOnly[0] && k.ID != "" && !k.CreatedAt.IsZero() {
		query = query.Where("parse_status <> ?", types.ParseStatusRejected)
		query = query.Where("(parse_status IN ? OR created_at < ? OR (created_at = ? AND id < ?))",
			[]string{types.ParseStatusCompleted, types.ParseStatusFinalizing}, k.CreatedAt, k.CreatedAt, k.ID)
	}
	base := func() *gorm.DB { return query.Session(&gorm.Session{}) }
	find := func(q *gorm.DB) (*types.Entry, error) {
		var existing types.Entry
		err := q.Order("created_at ASC, id ASC").First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return &existing, err
	}
	if rules.Identity.URL && k.NormalizedURL != "" {
		item, err := find(base().Where("normalized_url = ?", k.NormalizedURL))
		if item != nil || err != nil {
			return item, "url", 0, err
		}
		// A previously recorded repost URL points at the same canonical message.
		if rules.MergeReposts {
			owners := tx.Model(&types.EntryRepostSource{}).Select("entry_id").Where("space_id = ? AND library_id = ? AND normalized_url = ?", k.SpaceID, k.LibraryID, k.NormalizedURL)
			item, err = find(base().Where("id IN (?)", owners))
			if item != nil || err != nil {
				return item, "url", 0, err
			}
		}
	}
	if rules.Identity.Identifier && k.Identifier != "" {
		item, err := find(base().Where("identifier<>'' AND md5(identifier)=md5(?) AND identifier=?", k.Identifier, k.Identifier))
		if item != nil || err != nil {
			return item, "identifier", 0, err
		}
	}
	if rules.Identity.PublisherDate && k.NormalizedURL == "" {
		key := timelySourceIdentity(k)
		if key != "" {
			var candidates []types.Entry
			if err := base().Select("id, title, metadata").Where("normalized_url=''").Order("created_at ASC,id ASC").Find(&candidates).Error; err != nil {
				return nil, "", 0, err
			}
			for i := range candidates {
				if timelySourceIdentity(&candidates[i]) == key {
					item, err := find(base().Where("id=?", candidates[i].ID))
					return item, "publisher_date", 0, err
				}
			}
		}
	}
	if strings.TrimSpace(k.ContentHash) != "" {
		item, err := find(base().Where("content_hash = ?", k.ContentHash))
		if item != nil || err != nil {
			return item, "content_hash", 0, err
		}
	}
	if rules.Identity.NearDuplicate && k.ContentRuneCount >= minimumSimHashRunes && strings.TrimSpace(k.Title) != "" {
		var candidate struct {
			types.Entry
			Distance int
		}
		// PostgreSQL's bit_count avoids loading every body into the application.
		// Shared language or page templates alone cannot establish article identity.
		distance := "bit_count(content_sim_hash::bit(64) # CAST(? AS bigint)::bit(64))"
		err := base().Select("entries.*, "+distance+" AS distance", k.ContentSimHash).
			Where("lower(btrim(title)) = lower(?)", strings.TrimSpace(k.Title)).
			Where("content_rune_count >= ? AND content_rune_count BETWEEN ? AND ?", minimumSimHashRunes, k.ContentRuneCount*4/5, k.ContentRuneCount*5/4).
			Where(distance+" <= ?", k.ContentSimHash, messageSimHashThreshold()).Order("distance ASC, created_at ASC, id ASC").First(&candidate).Error
		if err == nil {
			return &candidate.Entry, "simhash", candidate.Distance, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, "", 0, err
		}
	}
	return nil, "", 0, nil
}

func recordMessageRepost(tx *gorm.DB, existing, incoming *types.Entry, method string, distance int) error {
	if existing.SpaceID != incoming.SpaceID || existing.LibraryID != incoming.LibraryID {
		return fmt.Errorf("repost entries must belong to the same library")
	}
	rules, err := messageTemplateRules(tx, incoming)
	if err != nil {
		return err
	}
	if !rules.MergeReposts {
		existing.DuplicateMatchMethod = method
		return nil
	}
	if err := lockEntitySpaceShared(tx, existing.SpaceID); err != nil {
		return err
	}
	source := types.SourceInfo{OriginalURL: incoming.Source}
	if intake, _ := incoming.IntakeMetadata(); intake != nil {
		source = intake.SourceInfo
	}
	normalizedURL := normalizeMessageURL(source.OriginalURL)
	// Publisher and author distinguish the provenance of repeated deliveries.
	origin := normalizedURL + "\x00" + normalizeRepostName(source.Publisher) + "\x00" + normalizeRepostName(source.Author)
	if normalizedURL == "" {
		origin += "\x00" + source.PublishedAt
	}
	hash := sha256.Sum256([]byte(origin))
	repost := types.EntryRepostSource{
		ID: uuid.NewString(), SpaceID: existing.SpaceID, LibraryID: incoming.LibraryID, EntryID: existing.ID,
		OriginalURL: source.OriginalURL, NormalizedURL: normalizedURL, SourceFingerprint: hex.EncodeToString(hash[:]),
		Author: source.Author, Publisher: source.Publisher, PublishedAt: source.PublishedAt, PublishedAtMissing: parsePublicationTime(source.PublishedAt) == nil,
		MatchMethod: method, SimHashDistance: distance, CreatedAt: time.Now(),
	}
	updates := clause.Assignments(map[string]any{
		"original_url": gorm.Expr("EXCLUDED.original_url"),
		"author":       gorm.Expr("EXCLUDED.author"),
		"publisher":    gorm.Expr("EXCLUDED.publisher"),
		// A delivery without a date cannot erase an already verified date.
		"published_at":         gorm.Expr("CASE WHEN EXCLUDED.published_at_missing AND NOT entry_repost_sources.published_at_missing THEN entry_repost_sources.published_at ELSE EXCLUDED.published_at END"),
		"published_at_missing": gorm.Expr("entry_repost_sources.published_at_missing AND EXCLUDED.published_at_missing"),
	})
	if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "space_id"}, {Name: "library_id"}, {Name: "entry_id"}, {Name: "source_fingerprint"}}, DoUpdates: updates}).Create(&repost).Error; err != nil {
		return err
	}
	// A URL may have collected repost provenance before its parser discovered
	// that the body belongs to another message. Keep those sources on the owner.
	if incoming.ID != "" && incoming.ID != existing.ID {
		var previous []types.EntryRepostSource
		if err := tx.Where("space_id = ? AND library_id = ? AND entry_id = ?", existing.SpaceID, existing.LibraryID, incoming.ID).Find(&previous).Error; err != nil {
			return err
		}
		for _, source := range previous {
			if err := tx.Where("space_id = ? AND library_id = ? AND id = ?", existing.SpaceID, existing.LibraryID, source.ID).Delete(&types.EntryRepostSource{}).Error; err != nil {
				return err
			}
			source.EntryID = existing.ID
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "space_id"}, {Name: "library_id"}, {Name: "entry_id"}, {Name: "source_fingerprint"}}, DoNothing: true}).Create(&source).Error; err != nil {
				return err
			}
		}
	}
	existing.DuplicateMatchMethod = method
	return nil
}

func (s *entryService) recordKnownMessageRepost(ctx context.Context, existing *types.Entry, libraryID string, source types.SourceInfo, method string) error {
	if s.db == nil || (source.OriginalURL == "" && source.Publisher == "" && source.Author == "") {
		existing.DuplicateMatchMethod = method
		return nil
	}
	incoming := &types.Entry{SpaceID: existing.SpaceID, LibraryID: libraryID}
	if err := incoming.SetIntakeMetadata(&types.EntryIntakeMetadata{SourceInfo: source}); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockMessageDeduplication(tx, incoming); err != nil {
			return err
		}
		return recordMessageRepost(tx, existing, incoming, method, 0)
	})
}

func lockMessageDeduplication(tx *gorm.DB, k *types.Entry) error {
	return tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", fmt.Sprintf("message-dedup:%d:%s", k.SpaceID, k.LibraryID)).Error
}

// checkMessageDuplicate is also called before parser output reaches any model.
func (s *entryService) checkMessageDuplicate(ctx context.Context, incoming *types.Entry, body string) (existing *types.Entry, err error) {
	setMessageFingerprint(incoming, body)
	if s.db == nil {
		return nil, nil
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockMessageDeduplication(tx, incoming); err != nil {
			return err
		}
		var method string
		var distance int
		existing, method, distance, err = findMessageDuplicate(tx, incoming, true)
		if err != nil {
			return err
		}
		if existing != nil {
			return recordMessageRepost(tx, existing, incoming, method, distance)
		}
		return tx.Model(&types.Entry{}).Where("space_id = ? AND library_id = ? AND id = ?", incoming.SpaceID, incoming.LibraryID, incoming.ID).Updates(map[string]any{
			"normalized_url": incoming.NormalizedURL, "content_hash": incoming.ContentHash, "content_sim_hash": incoming.ContentSimHash, "content_rune_count": incoming.ContentRuneCount,
		}).Error
	})
	return existing, err
}

// createMessageWithDeduplication reserves the body fingerprint in the same
// transaction as creation so concurrent identical intake has one owner.
func (s *entryService) createMessageWithDeduplication(ctx context.Context, incoming *types.Entry, body string) (existing *types.Entry, err error) {
	setMessageFingerprint(incoming, body)
	if s.db == nil {
		return nil, s.repo.CreateEntry(ctx, incoming)
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockMessageDeduplication(tx, incoming); err != nil {
			return err
		}
		var method string
		var distance int
		existing, method, distance, err = findMessageDuplicate(tx, incoming)
		if err != nil {
			return err
		}
		if existing != nil {
			return recordMessageRepost(tx, existing, incoming, method, distance)
		}
		return tx.Create(incoming).Error
	})
	return existing, err
}

func loadEntryRepostSources(db *gorm.DB, spaceID uint64, entryIDs, libraryIDs []string) ([]types.EntryRepostSource, error) {
	items := []types.EntryRepostSource{}
	if db == nil || len(entryIDs) == 0 {
		return items, nil
	}
	owners := db.Model(&types.Entry{}).Select("library_id").Where("space_id=? AND id IN ?", spaceID, uniqueEntryIDs(entryIDs))
	query := db.Where("space_id = ? AND library_id IN (?) AND entry_id IN ?", spaceID, owners, uniqueEntryIDs(entryIDs))
	if len(libraryIDs) > 0 {
		query = query.Where("library_id IN ?", libraryIDs)
	}
	err := query.Order("created_at ASC, id ASC").Find(&items).Error
	return items, err
}

func (s *entryService) stopDuplicateProcessing(ctx context.Context, incoming *types.Entry) error {
	if err := s.clearIntakeSource(ctx, incoming); err != nil {
		return err
	}
	s.tracker().FinalizeAttempt(ctx, incoming.ID, s.tracker().LatestAttempt(ctx, incoming.ID), "done", types.JSONMap{"duplicate": true}, "", "")
	return s.repo.HardDeleteEntry(ctx, incoming.SpaceID, incoming.ID)
}

func parsePublicationTime(value string) *time.Time {
	value = strings.TrimSpace(value)
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, time.DateOnly, "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return &parsed
		}
	}
	return nil
}

func uniqueEntryIDs(ids []string) []string {
	result := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		if id != "" && !seen[id] {
			result = append(result, id)
			seen[id] = true
		}
	}
	return result
}

// messageTemplateRules resolves behavior from the owning library, including legacy fixtures.
func messageTemplateRules(db *gorm.DB, entry *types.Entry) (types.TemplateRules, error) {
	var library types.Library
	if err := db.Model(&types.Library{}).Select("template").Where("space_id=? AND id=?", entry.SpaceID, entry.LibraryID).Scan(&library).Error; err != nil {
		return types.TemplateRules{}, err
	}
	return intakeTemplateRules(&library), nil
}

func identityFingerprint(kind, value string) string {
	hash := sha256.Sum256([]byte(kind + ":" + value))
	return hex.EncodeToString(hash[:])
}

func timelySourceIdentity(entry *types.Entry) string {
	source, err := entrySourceInfo(entry)
	if err != nil || missingSourceValue(source.Publisher) || strings.TrimSpace(entry.Title) == "" {
		return ""
	}
	date := parsePublicationTime(source.PublishedAt)
	if date == nil {
		return ""
	}
	value, _ := json.Marshal([]string{normalizeRepostName(source.Publisher), normalizeRepostName(entry.Title), date.UTC().Format(time.DateOnly)})
	return identityFingerprint("publisher_date", string(value))
}
