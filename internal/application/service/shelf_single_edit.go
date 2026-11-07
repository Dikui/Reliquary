package service

import (
	"context"
	"errors"
	"strconv"

	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

// Existing editing routes explicitly confirm a single proposal on the caller's
// behalf. They never call a shelf's managed writer outside a changeset.
func (s *EntityService) shelfSingleEdit(ctx context.Context, libraryID string, op types.ShelfOperation) (types.JSON, bool, error) {
	if _, ok := ctx.Value(shelfWriteContextKey{}).(shelfWriteContext); ok {
		return nil, false, nil
	}
	spaceID, err := s.requireSpace(ctx)
	if err != nil {
		return nil, true, err
	}
	if libraryID == "" {
		switch {
		case op.EntityID != "":
			var entity types.Entity
			if err := s.db.WithContext(ctx).Where("id=? AND space_id=?", op.EntityID, spaceID).First(&entity).Error; err != nil {
				return nil, true, managedWriteError(err)
			}
			libraryID = entity.LibraryID
			if op.Type == "set_entity_link" {
				op.Revision = entity.Revision
			}
		case op.RelationID != "":
			var relation types.Relation
			if err := s.db.WithContext(ctx).Where("id=? AND space_id=?", op.RelationID, spaceID).First(&relation).Error; err != nil {
				return nil, true, managedWriteError(err)
			}
			libraryID = relation.LibraryID
		}
	}
	var library types.Library
	if err := s.db.WithContext(ctx).Select("id,shelf_enabled").Where("id=? AND space_id=?", libraryID, spaceID).First(&library).Error; err != nil {
		return nil, true, managedWriteError(err)
	}
	if !library.ShelfEnabled {
		return nil, false, nil
	}
	source := types.OperationSource{Type: "web"}
	if actor, ok := types.PrincipalFromContext(ctx); ok && actor.Type == types.PrincipalAPIKey {
		source.Type = "agent"
		source.AgentName = actor.Name
		if source.AgentName == "" {
			keyID, _ := strconv.ParseUint(actor.ID, 10, 64)
			if err := s.db.WithContext(ctx).Table("api_keys").Where("id=? AND space_id=?", keyID, spaceID).Select("name").Scan(&source.AgentName).Error; err != nil {
				return nil, true, err
			}
		}
		if source.AgentName == "" {
			source.AgentName = actor.StorageID()
		}
	}
	result, err := s.SubmitShelfChangeset(ctx, libraryID, types.ShelfChangesetInput{Source: source, Operations: []types.ShelfOperation{op}, Confirmed: true})
	if err != nil {
		var app *apperrors.AppError
		if errors.As(err, &app) {
			if draft, ok := app.Details.(*types.ShelfChangeset); ok && len(draft.Items) > 0 {
				for _, conflict := range draft.Items[0].Conflicts {
					switch conflict.Type {
					case "unparseable", "required_missing", "cross_table_contradiction":
						return nil, true, apperrors.NewBadRequestError(conflict.Message)
					case "cross_kind_same_name":
						return nil, true, apperrors.NewConflictError(conflict.Message).WithDetails(conflict.Details)
					}
				}
			}
		}
		return nil, true, err
	}
	return result.Items[0].After, true, nil
}

func requireShelfChangeset(ctx context.Context, library *types.Library) error {
	if library.ShelfEnabled {
		if _, ok := ctx.Value(shelfWriteContextKey{}).(shelfWriteContext); !ok {
			return apperrors.NewBadRequestError("shelf writes require a confirmed changeset")
		}
	}
	return nil
}
