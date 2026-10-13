package service

import (
	"bytes"
	"context"
	"encoding/json"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reliquary/internal/application/repository"
	"reliquary/internal/types"
)

// Preserve stored JSON rather than persisting defaults used to display or run
// an existing library. Only semantic changes in submitted sections are
// applied; null, absent fields and extension metadata remain intact.
func (s *libraryService) persistLibrarySettings(ctx context.Context, library, before *types.Library, config *types.LibraryConfig) error {
	if s.db == nil {
		return s.persistLibrary(ctx, library)
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if config != nil && config.ModelTiers != nil {
			if err := tx.Model(&types.Space{}).Where("id=?", library.SpaceID).Update("embedding_model_id", config.ModelTiers.Embedding).Error; err != nil {
				return err
			}
		}
		var locked types.Library
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&locked, "id=? AND space_id=?", library.ID, library.SpaceID).Error; err != nil {
			return err
		}
		var raw struct{ ChunkingConfig, ModelTiers, IndexingStrategy *string }
		if err := tx.Raw(`SELECT chunking_config::text,model_tiers::text,indexing_strategy::text FROM libraries WHERE id=?`, library.ID).Scan(&raw).Error; err != nil {
			return err
		}
		old := map[string]*string{"chunking_config": raw.ChunkingConfig, "model_tiers": raw.ModelTiers, "indexing_strategy": raw.IndexingStrategy}
		displayed := *before
		displayed.EnsureDefaults()
		prior := map[string]any{"chunking_config": displayed.ChunkingConfig, "model_tiers": displayed.ModelTiers, "indexing_strategy": displayed.IndexingStrategy}
		desired := map[string]any{}
		if config != nil {
			if config.ChunkingConfig != nil {
				desired["chunking_config"] = config.ChunkingConfig
			}
			if config.ModelTiers != nil {
				desired["model_tiers"] = config.ModelTiers
			}
			if config.IndexingStrategy != nil {
				desired["indexing_strategy"] = config.IndexingStrategy
			}
		}
		updates := map[string]any{}
		for field, original := range old {
			var value any
			if original != nil {
				value = *original
			}
			if submitted, present := desired[field]; present {
				merged, err := preserveUneditedSettingsJSON(original, prior[field], submitted)
				if err != nil {
					return err
				}
				value = merged
			}
			updates[field] = gorm.Expr("?::jsonb", value)
		}
		if err := repository.NewLibraryRepository(tx).UpdateLibrary(ctx, library); err != nil {
			return err
		}
		return tx.Model(&types.Library{}).Where("id=?", library.ID).Updates(updates).Error
	})
}

func preserveUneditedSettingsJSON(original *string, before, after any) (any, error) {
	a, err := json.Marshal(before)
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(after)
	if err != nil {
		return nil, err
	}
	var old, new, raw map[string]json.RawMessage
	if err = json.Unmarshal(a, &old); err != nil {
		return nil, err
	}
	if err = json.Unmarshal(b, &new); err != nil {
		return nil, err
	}
	if original != nil {
		if err = json.Unmarshal([]byte(*original), &raw); err != nil {
			return nil, err
		}
	}
	if raw == nil {
		raw = map[string]json.RawMessage{}
	}
	changed := false
	for key, value := range new {
		if !bytes.Equal(old[key], value) {
			raw[key] = value
			changed = true
		}
	}
	for key := range old {
		if _, present := new[key]; !present {
			delete(raw, key)
			changed = true
		}
	}
	if !changed {
		if original == nil {
			return nil, nil
		}
		return *original, nil
	}
	result, err := json.Marshal(raw)
	return string(result), err
}
