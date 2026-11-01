package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"gorm.io/gorm"
	"reliquary/internal/types"
)

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

func setMessageFingerprint(k *types.Entry, body string) {
	if intake, _ := k.IntakeMetadata(); intake != nil {
		k.NormalizedURL = normalizeMessageURL(intake.SourceInfo.OriginalURL)
	}
	if k.NormalizedURL == "" {
		k.NormalizedURL = normalizeMessageURL(k.Source)
	}
	if body != "" {
		normalized := strings.Join(strings.Fields(strings.ReplaceAll(body, "\r\n", "\n")), " ")
		k.ContentHash = ""
		if normalized != "" {
			k.ContentHash = fmt.Sprintf("%x", sha256.Sum256([]byte(normalized)))
		}
		k.ContentRuneCount = len([]rune(normalized))
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

func lockMessageDeduplication(tx *gorm.DB, k *types.Entry) error {
	return tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", fmt.Sprintf("message-dedup:%d:%s", k.SpaceID, k.LibraryID)).Error
}

// saveMessageFingerprint retains source and body hashes for historical material and rejection records.
func (s *entryService) saveMessageFingerprint(ctx context.Context, incoming *types.Entry, body string) error {
	setMessageFingerprint(incoming, body)
	if s.db == nil {
		return nil
	}
	return s.db.WithContext(ctx).Model(&types.Entry{}).Where("space_id = ? AND library_id = ? AND id = ?", incoming.SpaceID, incoming.LibraryID, incoming.ID).Updates(map[string]any{
		"normalized_url": incoming.NormalizedURL, "content_hash": incoming.ContentHash, "content_rune_count": incoming.ContentRuneCount,
	}).Error
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

func identityFingerprint(kind, value string) string {
	hash := sha256.Sum256([]byte(kind + ":" + value))
	return hex.EncodeToString(hash[:])
}

// timelySourceIdentity is retained only for matching human rejection records.
func timelySourceIdentity(entry *types.Entry) string {
	source, err := entrySourceInfo(entry)
	if err != nil || isMissingRejectionPublisher(source.Publisher) || strings.TrimSpace(entry.Title) == "" {
		return ""
	}
	date := parsePublicationTime(source.PublishedAt)
	if date == nil {
		return ""
	}
	value, _ := json.Marshal([]string{normalizeRepostName(source.Publisher), normalizeRepostName(entry.Title), date.UTC().Format(time.DateOnly)})
	return identityFingerprint("publisher_date", string(value))
}

func isMissingRejectionPublisher(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "unknown", "n/a", "null", "none", "未知", "不详", "未提供":
		return true
	}
	return false
}
