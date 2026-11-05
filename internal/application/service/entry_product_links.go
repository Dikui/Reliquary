package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reliquary/internal/types"
)

// Product matching is separate from local extraction: it only reads master
// records and never changes the local graph's resolved entity IDs.
func (s *entryService) associateProductMentions(ctx context.Context, entry *types.Entry, body string) ([]string, error) {
	ids := []string{}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockEntitySpaceShared(tx, entry.SpaceID); err != nil {
			return err
		}
		var documentLibrary types.Library
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id=? AND space_id=?", entry.LibraryID, entry.SpaceID).First(&documentLibrary).Error; err != nil {
			return err
		}
		targetIDs := append([]string{}, documentLibrary.ProductLibraryIDs...)
		if documentLibrary.ManagesEntities() {
			targetIDs = appendUniqueEntityString(targetIDs, documentLibrary.ID)
		}
		if len(targetIDs) == 0 {
			return nil
		}
		var targets []types.Library
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id IN ? AND space_id=? AND entity_mode=?", targetIDs, entry.SpaceID, types.EntityModeManaged).Order("id").Find(&targets).Error; err != nil {
			return err
		}
		targetIDs = nil
		for _, target := range targets {
			if slices.Contains(target.DocumentRelationTypes, "references") {
				targetIDs = append(targetIDs, target.ID)
			}
		}
		if len(targetIDs) == 0 {
			return nil
		}
		var current types.Entry
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND space_id=?", entry.ID, entry.SpaceID).First(&current).Error; err != nil {
			return err
		}
		if current.VaultBody != body {
			return fmt.Errorf("entry body changed during product matching")
		}
		var products []types.Entity
		if err := tx.Where("space_id=? AND library_id IN ? AND archived_at IS NULL AND merged_into_id=''", entry.SpaceID, targetIDs).Find(&products).Error; err != nil {
			return err
		}
		owners := map[string][]string{}
		add := func(name, id string) {
			name = strings.ToLower(strings.TrimSpace(name))
			if name != "" {
				owners[name] = appendUniqueEntityString(owners[name], id)
			}
		}
		productIDs := []string{}
		for _, product := range products {
			productIDs = append(productIDs, product.ID)
			add(product.CanonicalName, product.ID)
			add(product.Code, product.ID)
		}
		if len(productIDs) == 0 {
			return nil
		}
		var aliases []types.EntityAlias
		if err := tx.Where("space_id=? AND entity_id IN ?", entry.SpaceID, productIDs).Find(&aliases).Error; err != nil {
			return err
		}
		for _, alias := range aliases {
			add(alias.Alias, alias.EntityID)
		}
		evidence := map[string][]types.SourceLocation{}
		for name, matches := range owners {
			if len(matches) != 1 {
				continue
			}
			for _, span := range regexp.MustCompile("(?i)"+regexp.QuoteMeta(name)).FindAllStringIndex(body, -1) {
				first, _ := utf8.DecodeRuneInString(name)
				last, _ := utf8.DecodeLastRuneInString(name)
				before, _ := utf8.DecodeLastRuneInString(body[:span[0]])
				after, _ := utf8.DecodeRuneInString(body[span[1]:])
				if productWordRune(first) && productWordRune(before) || productWordRune(last) && productWordRune(after) {
					continue
				}
				start := utf8.RuneCountInString(body[:span[0]])
				end := start + utf8.RuneCountInString(body[span[0]:span[1]])
				evidence[matches[0]] = append(evidence[matches[0]], bodyRangeLocations(body, start, end, "exact")...)
			}
		}
		for id, locations := range evidence {
			encoded, err := json.Marshal(locations)
			if err != nil {
				return err
			}
			link := types.EntryEntity{SpaceID: entry.SpaceID, EntryID: entry.ID, EntityID: id, Origin: "extracted", RelationType: "references", Evidence: locations, BodyHash: fmt.Sprintf("%x", sha256.Sum256([]byte(body))), CreatedAt: time.Now()}
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "entry_id"}, {Name: "entity_id"}}, DoUpdates: clause.Assignments(map[string]any{"evidence": types.JSON(encoded), "body_hash": link.BodyHash}), Where: clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "entry_entities.origin<>'manual' AND entry_entities.excluded=false"}}}}).Create(&link).Error; err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return nil
	})
	return ids, err
}

func productWordRune(r rune) bool {
	return unicode.Is(unicode.Latin, r) || unicode.IsDigit(r) || r == '_'
}
