package service

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reliquary/internal/application/repository"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

type shelfWriteContextKey struct{}
type shelfWriteContext struct {
	Source      types.OperationSource
	ChangesetID string
}

func shelfSourceValid(source types.OperationSource) error {
	if !slices.Contains([]string{"web", "agent", "import"}, source.Type) ||
		(source.Type == "agent" && strings.TrimSpace(source.AgentName) == "") ||
		(source.Type == "import" && (strings.TrimSpace(source.ImportBatchID) == "" || source.FileDate == nil || source.FileDate.IsZero())) ||
		(source.Type != "agent" && source.AgentName != "") || (source.Type != "import" && (source.ImportBatchID != "" || source.FileDate != nil)) ||
		len([]rune(source.AgentName)) > 200 || len([]rune(source.ImportBatchID)) > 200 {
		return apperrors.NewBadRequestError("source requires web, a named agent, or an import batch with file_date")
	}
	return nil
}

func (s *EntityService) shelfLibrary(ctx context.Context, tx *gorm.DB, id string) (*types.Library, error) {
	spaceID, err := s.requireSpace(ctx)
	if err != nil {
		return nil, err
	}
	if _, ok := types.PrincipalFromContext(ctx); !ok {
		return nil, apperrors.NewUnauthorizedError("shelf editor required")
	}
	library, err := managedLibrary(tx, spaceID, id)
	if err != nil {
		return nil, err
	}
	if !library.ShelfEnabled {
		return nil, apperrors.NewBadRequestError("this operation requires an enabled shelf")
	}
	return library, nil
}

func shelfOperationValid(op types.ShelfOperation) error {
	if op.ID == "" || len(op.ID) > 100 {
		return apperrors.NewBadRequestError("operation id is required and limited to 100 bytes")
	}
	valid := false
	switch op.Type {
	case "import_notice":
		valid = op.Entity == nil && op.Patch == nil && op.Relation == nil && op.EntityID == ""
	case "save_import_plan":
		valid = op.ImportPlan != nil && op.Entity == nil && op.Patch == nil && op.Relation == nil
	case "classify_document":
		valid = op.Classification != nil && op.EntityID != "" && op.Revision > 0 && op.Entity == nil && op.Patch == nil && op.Relation == nil
	case "select_document_version":
		valid = op.VersionID != "" && op.EntityID != "" && op.Revision > 0 && op.Entity == nil && op.Patch == nil && op.Relation == nil
	case "reparse_document":
		valid = op.Reparse != nil && op.Entity == nil && op.Patch == nil && op.Relation == nil
	case "add_document_version":
		valid = op.Document != nil && op.Entity == nil && op.Patch == nil && op.Relation == nil
	case "create_certification", "update_certification":
		valid = op.Certification != nil && op.Entity == nil && op.Patch == nil && op.Relation == nil
	case "set_sale_statuses":
		valid = op.SaleStatuses != nil && op.Entity == nil && op.Patch == nil && op.Relation == nil
	case "set_parameter_values":
		valid = op.ParameterValues != nil && op.Entity == nil && op.Patch == nil && op.Relation == nil
	case "configure_shelf":
		valid = op.Library != nil && op.Entity == nil && op.Patch == nil && op.Relation == nil
	case "create_entity":
		valid = op.Entity != nil && op.Patch == nil && op.Relation == nil
	case "update_entity":
		valid = op.Entity == nil && op.Patch != nil && op.Relation == nil
	case "add_alias", "archive_entity", "missing_entity":
		valid = op.Entity == nil && op.Patch == nil && op.Relation == nil
	case "save_relation":
		valid = op.Entity == nil && op.Patch == nil && op.Relation != nil
	case "end_relation", "set_entity_link":
		valid = op.Entity == nil && op.Patch == nil && op.Relation == nil
	}
	if op.EffectiveAt != nil && op.Type != "set_sale_statuses" && op.Type != "set_parameter_values" && op.Type != "create_certification" && op.Type != "update_certification" {
		valid = false
	}
	if (op.Type != "set_sale_statuses" && op.SaleStatuses != nil) || (op.Type != "set_parameter_values" && op.ParameterValues != nil) {
		valid = false
	}
	if op.Type != "create_certification" && op.Type != "update_certification" && (op.Certification != nil || op.CertificationOwner != nil) {
		valid = false
	}
	if op.Type != "add_document_version" && op.Document != nil {
		valid = false
	}
	if op.Type != "save_import_plan" && op.ImportPlan != nil {
		valid = false
	}
	if op.Type != "classify_document" && op.Classification != nil || op.Type != "select_document_version" && op.VersionID != "" {
		valid = false
	}
	if op.Type != "reparse_document" && op.Reparse != nil {
		valid = false
	}
	if op.Type != "configure_shelf" && op.Library != nil {
		valid = false
	}
	if op.Type != "create_entity" && op.CandidateEntityID != "" {
		valid = false
	}
	if op.ParentID != "" && (op.Type != "create_entity" || op.Entity == nil || !slices.Contains([]string{"model", "product", "platform"}, op.Entity.Kind)) {
		valid = false
	}
	if !valid {
		return apperrors.NewBadRequestError("unknown operation type or incompatible operation payload")
	}
	for _, issue := range op.Issues {
		if op.Type == "import_notice" && slices.Contains([]string{"cross_table_contradiction", "unmapped_column"}, issue.Type) {
			continue
		}
		if op.Type == "import_notice" {
			return apperrors.NewBadRequestError("import notices only accept cross table contradictions or unmapped columns")
		}
		if !slices.Contains([]string{"unparseable", "uncertain", "required_missing", "cross_table_contradiction"}, issue.Type) {
			return apperrors.NewBadRequestError("unsupported submitter issue type")
		}
	}
	return nil
}

func (s *EntityService) SubmitShelfChangeset(ctx context.Context, libraryID string, input types.ShelfChangesetInput) (*types.ShelfChangeset, error) {
	if _, err := s.requireSpace(ctx); err != nil {
		return nil, err
	}
	if err := shelfSourceValid(input.Source); err != nil {
		return nil, err
	}
	if len(input.Operations) == 0 || len(input.Operations) > 500 {
		return nil, apperrors.NewBadRequestError("provide 1-500 shelf operations")
	}
	draft := &types.ShelfChangeset{ID: uuid.NewString(), LibraryID: libraryID, Revision: 1, Status: "pending", Source: input.Source, CreatedAt: time.Now(), UpdatedAt: time.Now(), Items: []types.ShelfChangesetItem{}}
	seen := map[string]bool{}
	for _, op := range input.Operations {
		if op.ID == "" {
			op.ID = uuid.NewString()
		}
		if err := shelfOperationValid(op); err != nil {
			return nil, err
		}
		if seen[op.ID] {
			return nil, apperrors.NewBadRequestError("operation ids must be unique")
		}
		seen[op.ID] = true
		id := op.EntityID
		if op.Type == "create_entity" || op.Type == "create_certification" || op.Type == "add_document_version" {
			id = uuid.NewString()
		}
		draft.Items = append(draft.Items, types.ShelfChangesetItem{ID: op.ID, EntityID: id, Operation: op, Before: types.JSON("null"), After: types.JSON("null"), Conflicts: []types.ShelfConflict{}})
	}
	var refreshIDs []string
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockEntitySpace(tx, types.MustSpaceIDFromContext(ctx)); err != nil {
			return err
		}
		library, err := s.shelfLibrary(ctx, tx, libraryID)
		if err != nil {
			return err
		}
		draft.SpaceID = library.SpaceID
		if err := shelfDecisions(draft, input.Decisions); err != nil {
			return err
		}
		if err := s.previewShelf(ctx, tx, draft); err != nil {
			return err
		}
		if input.Confirmed {
			if err := s.confirmShelf(ctx, tx, draft, true); err != nil {
				return err
			}
			refreshIDs, err = s.shelfHeaderEntries(ctx, tx, draft)
			if err != nil {
				return err
			}
		}
		return tx.Create(draft).Error
	})
	if err == nil {
		s.scheduleShelfHeaders(ctx, refreshIDs)
	}
	return draft, managedWriteError(err)
}

func shelfDecisions(draft *types.ShelfChangeset, decisions []types.ShelfDecision) error {
	seen := map[string]bool{}
	for _, decision := range decisions {
		if seen[decision.ItemID] {
			return apperrors.NewBadRequestError("duplicate item decision")
		}
		seen[decision.ItemID] = true
		index := slices.IndexFunc(draft.Items, func(item types.ShelfChangesetItem) bool { return item.ID == decision.ItemID })
		if index < 0 || !slices.Contains([]string{"apply", "keep", "discard", "defer", "same_entity", "retire", "replace", "return_to_submitter"}, decision.Action) {
			return apperrors.NewBadRequestError("decision requires a known item and action")
		}
		item := &draft.Items[index]
		if decision.Action == "return_to_submitter" {
			uncertain := slices.ContainsFunc(item.Conflicts, func(conflict types.ShelfConflict) bool { return conflict.Type == "uncertain" }) ||
				slices.ContainsFunc(item.Operation.Issues, func(conflict types.ShelfConflict) bool { return conflict.Type == "uncertain" })
			if draft.Source.Type != "agent" || !uncertain {
				return apperrors.NewBadRequestError("only uncertain agent items can be returned to the submitter")
			}
		}
		if decision.Action == "replace" {
			if err := replaceShelfItem(draft, index, decision); err != nil {
				return err
			}
			continue
		} else if decision.Replacement != nil || decision.Replacements != nil {
			return apperrors.NewBadRequestError("only replace accepts corrected operations")
		}
		if decision.Action == "same_entity" && (item.Operation.Type != "create_entity" || decision.TargetEntityID == "" || decision.TargetRevision < 1) {
			return apperrors.NewBadRequestError("same_entity requires a create proposal and a target revision")
		}
		if (decision.Action == "retire" || decision.Action == "keep") && item.Operation.Type != "missing_entity" {
			return apperrors.NewBadRequestError("retire and keep are decisions for missing entities")
		}
		item.Decision = &decision
	}
	return nil
}

// Expansion is itself a review decision. It only rewrites pending proposals;
// the existing transaction still validates and commits every selected operation.
func replaceShelfItem(draft *types.ShelfChangeset, index int, decision types.ShelfDecision) error {
	original := draft.Items[index]
	ops := decision.Replacements
	if decision.Replacement != nil {
		if ops != nil {
			return apperrors.NewBadRequestError("use replacement or replacements, not both")
		}
		ops = []types.ShelfOperation{*decision.Replacement}
	}
	if len(ops) == 0 || len(draft.Items)-1+len(ops) > 500 {
		return apperrors.NewBadRequestError("replace requires 1-500 corrected operations within the changeset limit")
	}
	ids := map[string]bool{}
	for i, item := range draft.Items {
		if i != index {
			ids[item.ID] = true
		}
	}
	replacements := make([]types.ShelfChangesetItem, 0, len(ops))
	for i, op := range ops {
		if i == 0 {
			op.ID = original.ID
		} else if op.ID == "" {
			op.ID = uuid.NewString()
		}
		if ids[op.ID] {
			return apperrors.NewBadRequestError("operation ids must be unique")
		}
		ids[op.ID] = true
		if original.Operation.CertificationOwner != nil && op.Type == "create_entity" && op.Entity != nil && op.Entity.Kind == "model" && op.ParentID == "" {
			return apperrors.NewBadRequestError("a new certification owner model requires parent_id")
		}
		if op.Certification != nil && op.CertificationOwner == nil {
			op.CertificationOwner = original.Operation.CertificationOwner
		}
		if op.ImportLocation == nil {
			op.ImportLocation = original.Operation.ImportLocation
		}
		if err := shelfOperationValid(op); err != nil {
			return err
		}
		id := op.EntityID
		if op.Type == "create_entity" || op.Type == "create_certification" || op.Type == "add_document_version" {
			id = uuid.NewString()
			if i == 0 && original.Operation.Type == op.Type && op.Type != "add_document_version" {
				id = original.EntityID
			}
		}
		applied := decision
		applied.ItemID = op.ID
		applied.Action = "apply"
		applied.Replacement = nil
		applied.Replacements = nil
		replacements = append(replacements, types.ShelfChangesetItem{ID: op.ID, EntityID: id, Operation: op, Before: types.JSON("null"), After: types.JSON("null"), Conflicts: []types.ShelfConflict{}, Decision: &applied})
	}
	items := append([]types.ShelfChangesetItem{}, draft.Items[:index]...)
	items = append(items, replacements...)
	items = append(items, draft.Items[index+1:]...)
	draft.Items = items
	return nil
}

func (s *EntityService) ShelfChangeset(ctx context.Context, libraryID, id string) (*types.ShelfChangeset, error) {
	spaceID, err := s.requireSpace(ctx)
	if err != nil {
		return nil, err
	}
	var draft types.ShelfChangeset
	err = s.db.WithContext(ctx).Where("id=? AND library_id=? AND space_id=?", id, libraryID, spaceID).First(&draft).Error
	return &draft, managedWriteError(err)
}

func (s *EntityService) ShelfChangesets(ctx context.Context, libraryID, status, sourceType string, page, size int) (*types.ShelfChangesetPage, error) {
	spaceID, err := s.requireSpace(ctx)
	if err != nil {
		return nil, err
	}
	if page < 1 || size < 1 || size > 100 || (status != "" && status != "pending" && status != "confirmed") || (sourceType != "" && sourceType != "web" && sourceType != "agent" && sourceType != "import") {
		return nil, apperrors.NewBadRequestError("invalid changeset query")
	}
	var library types.Library
	if err := s.db.WithContext(ctx).Where("id=? AND space_id=? AND shelf_enabled", libraryID, spaceID).First(&library).Error; err != nil {
		return nil, managedWriteError(err)
	}
	q := s.db.WithContext(ctx).Model(&types.ShelfChangeset{}).Where("space_id=? AND library_id=?", spaceID, libraryID)
	if status != "" {
		q = q.Where("status=?", status)
	}
	if sourceType != "" {
		q = q.Where("source->>'type'=?", sourceType)
	}
	result := &types.ShelfChangesetPage{Items: []types.ShelfChangeset{}, Page: page, PageSize: size}
	if err := q.Count(&result.Total).Error; err != nil {
		return nil, err
	}
	err = q.Order("created_at DESC,id DESC").Limit(size).Offset((page - 1) * size).Find(&result.Items).Error
	return result, err
}

func (s *EntityService) ReviewShelfChangeset(ctx context.Context, libraryID, id string, input types.ShelfChangesetReview, confirm bool) (*types.ShelfChangeset, error) {
	if _, err := s.requireSpace(ctx); err != nil {
		return nil, err
	}
	var refreshIDs []string
	var draft types.ShelfChangeset
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockEntitySpace(tx, types.MustSpaceIDFromContext(ctx)); err != nil {
			return err
		}
		library, err := s.shelfLibrary(ctx, tx, libraryID)
		if err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND library_id=? AND space_id=?", id, libraryID, library.SpaceID).First(&draft).Error; err != nil {
			return err
		}
		if draft.Status != "pending" || draft.Revision != input.Revision {
			return apperrors.NewConflictError("changeset changed; reload before reviewing")
		}
		if err := shelfDecisions(&draft, input.Decisions); err != nil {
			return err
		}
		if err := s.previewShelf(ctx, tx, &draft); err != nil {
			return err
		}
		if confirm {
			if err := s.confirmShelf(ctx, tx, &draft, false); err != nil {
				return err
			}
			refreshIDs, err = s.shelfHeaderEntries(ctx, tx, &draft)
			if err != nil {
				return err
			}
		}
		draft.Revision++
		draft.UpdatedAt = time.Now()
		return tx.Save(&draft).Error
	})
	if err == nil {
		s.scheduleShelfHeaders(ctx, refreshIDs)
	}
	return &draft, managedWriteError(err)
}

// Validation runs the real managed writers inside a savepoint, then rolls back
// every catalog, history, log and projection change before saving the proposal.
// No filesystem or worker side effects run on the transaction-bound service.
func (s *EntityService) previewShelf(ctx context.Context, tx *gorm.DB, draft *types.ShelfChangeset) error {
	if err := tx.SavePoint("shelf_preview").Error; err != nil {
		return err
	}
	bound := s.shelfBound(tx)
	ctx = context.WithValue(ctx, shelfWriteContextKey{}, shelfWriteContext{draft.Source, draft.ID})
	var previewErr error
	for i := range draft.Items {
		item := &draft.Items[i]
		item.Warnings = nil
		item.Before = types.JSON("null")
		item.After = types.JSON("null")
		item.Conflicts = append([]types.ShelfConflict{}, item.Operation.Issues...)
		if item.Operation.Type == "import_notice" {
			continue
		}
		if item.Operation.Type == "missing_entity" {
			item.Conflicts = append(item.Conflicts, types.ShelfConflict{Type: "disappeared", Message: "choose retire or keep; missing rows are never deleted"})
		}
		if len(item.Operation.Issues) > 0 {
			continue
		}
		before, err := bound.shelfBefore(ctx, draft, item)
		if err != nil {
			var app *apperrors.AppError
			if !errors.As(err, &app) {
				previewErr = err
				break
			}
			item.Conflicts = append(item.Conflicts, shelfErrorConflict(err))
			continue
		}
		item.Before = before
		if item.Operation.Type == "create_entity" && item.Operation.CandidateEntityID != "" && (item.Decision == nil || item.Decision.Action != "same_entity") {
			target, err := bound.shelfEntity(ctx, draft.LibraryID, item.Operation.CandidateEntityID)
			if err != nil {
				item.Conflicts = append(item.Conflicts, shelfErrorConflict(err))
				continue
			}
			if target.Kind != item.Operation.Entity.Kind || target.ArchivedAt != nil {
				item.Conflicts = append(item.Conflicts, types.ShelfConflict{Type: "uncertain", Message: "candidate must be active and in the same kind"})
				continue
			}
			item.Conflicts = append(item.Conflicts, types.ShelfConflict{Type: "suspected_same", Candidates: []types.ManagedNameConfirmation{{Name: item.Operation.Entity.Name, OtherEntityID: target.ID, OtherKind: target.Kind}}})
			continue
		}
		if err := tx.SavePoint("shelf_item").Error; err != nil {
			previewErr = err
			break
		}
		result, err := bound.executeShelf(ctx, draft, item)
		if err != nil {
			if rollback := tx.RollbackTo("shelf_item").Error; rollback != nil {
				previewErr = rollback
				break
			}
			var app *apperrors.AppError
			if !errors.As(err, &app) {
				previewErr = err
				break
			}
			conflict := shelfErrorConflict(err)
			if item.Operation.Type == "create_entity" && item.Operation.Entity != nil && conflict.Type == "revision_conflict" {
				candidates, e := bound.shelfSameNameCandidates(ctx, draft, item.Operation.Entity)
				if e != nil {
					previewErr = e
					break
				}
				if len(candidates) > 0 {
					conflict.Type = "suspected_same"
					conflict.Candidates = candidates
				}
			}
			item.Conflicts = append(item.Conflicts, conflict)
			continue
		}
		item.After = shelfJSON(result)
		item.Warnings, err = bound.shelfStatusWarnings(ctx, draft, item)
		if err != nil {
			previewErr = err
			break
		}
		if item.Decision != nil && slices.Contains([]string{"discard", "defer", "keep", "return_to_submitter"}, item.Decision.Action) {
			if err := tx.RollbackTo("shelf_item").Error; err != nil {
				previewErr = err
				break
			}
		}
		created := (item.Operation.Type == "create_entity" || item.Operation.Type == "create_certification") && (item.Decision == nil || item.Decision.Action != "same_entity")
		created = created || (item.Operation.Type == "save_import_plan" && string(item.Before) == "null")
		created = created || (item.Operation.Type == "add_document_version" && string(item.Before) == "null")
		created = created || (item.Operation.Type == "save_relation" && item.Operation.RelationID == "") || (item.Operation.Type == "set_entity_link" && string(item.Before) == "null")
		if created {
			item.Conflicts = append(item.Conflicts, types.ShelfConflict{Type: "new"})
		} else if item.Operation.Type != "missing_entity" {
			conflict := types.ShelfConflict{Type: "modified"}
			if draft.Source.Type == "import" && item.EntityID != "" {
				conflict.ManualBefore, err = bound.shelfManualBefore(ctx, draft.ID, item.EntityID, item.Before, item.After)
				if err != nil {
					previewErr = err
					break
				}
			}
			item.Conflicts = append(item.Conflicts, conflict)
		}
	}
	if err := tx.RollbackTo("shelf_preview").Error; err != nil {
		return err
	}
	return previewErr
}

func shelfErrorConflict(err error) types.ShelfConflict {
	conflict := types.ShelfConflict{Type: "unparseable", Message: err.Error()}
	var app *apperrors.AppError
	if errors.As(err, &app) {
		conflict.Message = app.Message
		conflict.Details = app.Details
		if app.HTTPCode == 409 {
			conflict.Type = "revision_conflict"
		}
		var names *types.ManagedNameConflictDetails
		if app.Details != nil {
			names, _ = app.Details.(*types.ManagedNameConflictDetails)
		}
		if names != nil {
			conflict.Type = "cross_kind_same_name"
		}
		if strings.HasPrefix(app.Message, "uncertain:") {
			conflict.Type = "uncertain"
		}
		if strings.Contains(app.Message, "required") {
			conflict.Type = "required_missing"
		}
		if strings.Contains(app.Message, "hierarchy") || strings.Contains(app.Message, "relationship") || strings.Contains(app.Message, "child entities") {
			conflict.Type = "cross_table_contradiction"
		}
	}
	return conflict
}

func shelfJSON(value any) types.JSON { data, _ := json.Marshal(value); return types.JSON(data) }

func (s *EntityService) shelfBound(tx *gorm.DB) *EntityService {
	return &EntityService{db: tx, models: s.models, files: s.files, nameEmbeddingRebuilder: s.nameEmbeddingRebuilder}
}

func (s *EntityService) confirmShelf(ctx context.Context, tx *gorm.DB, draft *types.ShelfChangeset, immediate bool) error {
	for i := range draft.Items {
		item := &draft.Items[i]
		if item.Decision == nil && immediate {
			safe := true
			for _, c := range item.Conflicts {
				if (c.Type != "new" && c.Type != "modified") || len(c.ManualBefore) > 0 {
					safe = false
				}
			}
			if safe {
				item.Decision = &types.ShelfDecision{ItemID: item.ID, Action: "apply"}
			}
		}
		if item.Decision == nil {
			return apperrors.NewConflictError("every item needs a decision").WithDetails(draft)
		}
		if item.Decision.Action == "return_to_submitter" {
			return apperrors.NewConflictError("returned items require clarification from the submitter").WithDetails(draft)
		}
		if slices.Contains([]string{"discard", "defer", "keep"}, item.Decision.Action) {
			continue
		}
		for _, conflict := range item.Conflicts {
			if item.Operation.Type == "import_notice" && slices.Contains([]string{"cross_table_contradiction", "unmapped_column"}, conflict.Type) {
				continue
			}
			if conflict.Type == "new" || conflict.Type == "modified" || (conflict.Type == "disappeared" && item.Decision.Action == "retire") {
				continue
			}
			return apperrors.NewConflictError("resolve blocking conflicts before confirmation").WithDetails(draft)
		}
	}
	ctx = context.WithValue(ctx, shelfWriteContextKey{}, shelfWriteContext{draft.Source, draft.ID})
	bound := s.shelfBound(tx)
	deferred := []types.ShelfChangesetItem{}
	for i := range draft.Items {
		item := &draft.Items[i]
		switch item.Decision.Action {
		case "discard", "keep":
			continue
		case "defer":
			copy := *item
			copy.Operation = shelfResolvedOperation(draft, copy.Operation)
			if copy.Operation.Type == "update_certification" && copy.Operation.EntityID == "" {
				copy.Operation.EntityID = copy.EntityID
			}
			if copy.Operation.Type != "create_entity" && copy.Operation.Type != "create_certification" && copy.Operation.Type != "add_document_version" {
				copy.EntityID = copy.Operation.EntityID
			}
			copy.Decision = nil
			deferred = append(deferred, copy)
			continue
		}
		result, err := bound.executeShelf(ctx, draft, item)
		if err != nil {
			return err
		}
		item.After = shelfJSON(result)
	}
	if len(deferred) > 0 {
		next := types.ShelfChangeset{ID: uuid.NewString(), SpaceID: draft.SpaceID, LibraryID: draft.LibraryID, Revision: 1, Status: "pending", Source: draft.Source, Items: deferred, CreatedAt: time.Now(), UpdatedAt: time.Now()}
		if err := tx.Create(&next).Error; err != nil {
			return err
		}
		draft.DeferredChangesetID = next.ID
	}
	now := time.Now()
	draft.Status = "confirmed"
	draft.ConfirmedAt = &now
	return nil
}

func (s *EntityService) shelfEntity(ctx context.Context, libraryID, id string) (*types.Entity, error) {
	var entity types.Entity
	if err := s.db.WithContext(ctx).Where("id=? AND library_id=? AND space_id=? AND merged_into_id=''", id, libraryID, types.MustSpaceIDFromContext(ctx)).First(&entity).Error; err != nil {
		return nil, managedWriteError(err)
	}
	rows := []types.Entity{entity}
	if err := s.loadAliases(ctx, entity.SpaceID, rows); err != nil {
		return nil, err
	}
	entity = rows[0]
	if err := hydrateManagedEntityAttributes(ctx, s.db, []*types.Entity{&entity}, time.Now()); err != nil {
		return nil, err
	}
	return &entity, nil
}

func (s *EntityService) shelfBefore(ctx context.Context, draft *types.ShelfChangeset, item *types.ShelfChangesetItem) (types.JSON, error) {
	op, err := s.prepareShelfCertification(ctx, draft, shelfResolvedOperation(draft, item.Operation))
	if err != nil {
		return types.JSON("null"), err
	}
	if op.Certification != nil {
		normalized := *op.Certification
		// Keep intra-batch references live when a preceding proposal is replaced.
		if strings.HasPrefix(item.Operation.Certification.OwnerID, "$") {
			normalized.OwnerID = item.Operation.Certification.OwnerID
		}
		if strings.HasPrefix(item.Operation.Certification.CellPartID, "$") {
			normalized.CellPartID = item.Operation.Certification.CellPartID
		}
		item.Operation.Certification = &normalized
	}
	if op.Type == "classify_document" || op.Type == "select_document_version" {
		return s.shelfDocumentEditBefore(ctx, draft, shelfResolvedOperation(draft, op))
	}
	if op.Type == "save_import_plan" {
		plan, err := s.shelfImportPlan(ctx, draft.LibraryID, op.ImportPlan.Key)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return types.JSON("null"), nil
		}
		return shelfJSON(plan), err
	}
	if op.Type == "reparse_document" {
		version, err := s.shelfReparseVersion(ctx, draft, shelfResolvedOperation(draft, op))
		return shelfJSON(version), err
	}
	if op.Type == "add_document_version" {
		return s.shelfDocumentBefore(ctx, draft, item)
	}
	if op.Type == "configure_shelf" {
		var library types.Library
		err := s.db.WithContext(ctx).Where("id=? AND space_id=?", draft.LibraryID, draft.SpaceID).First(&library).Error
		return shelfJSON(library), err
	}
	if op.Type == "create_entity" || op.Type == "create_certification" {
		return types.JSON("null"), nil
	}
	if op.Type == "set_entity_link" {
		var row types.EntryEntity
		err := s.db.WithContext(ctx).Where("entry_id=? AND entity_id=? AND space_id=?", op.EntryID, shelfReference(draft, op.EntityID), draft.SpaceID).First(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return types.JSON("null"), nil
		}
		return shelfJSON(row), err
	}
	if op.Type == "save_relation" || op.Type == "end_relation" {
		if op.RelationID == "" {
			return types.JSON("null"), nil
		}
		var row types.Relation
		err := s.db.WithContext(ctx).Where("id=? AND library_id=? AND space_id=?", op.RelationID, draft.LibraryID, draft.SpaceID).First(&row).Error
		return shelfJSON(row), managedWriteError(err)
	}
	if op.Type == "update_certification" {
		resolved := op
		entity, err := s.resolveShelfCertification(ctx, draft, resolved, shelfCertificationTime(draft, resolved), item.Decision)
		if err == nil {
			item.EntityID = entity.ID
			if item.Operation.CertificationOwner != nil && item.Operation.Revision == 0 {
				if err := mergeShelfImportedCertification(item, entity); err != nil {
					return types.JSON("null"), err
				}
				item.Operation.Revision = entity.Revision
			}
		}
		return shelfJSON(entity), err
	}
	entity, err := s.shelfEntity(ctx, draft.LibraryID, shelfReference(draft, op.EntityID))
	return shelfJSON(entity), err
}

func shelfReference(draft *types.ShelfChangeset, id string) string {
	if strings.HasPrefix(id, "$") {
		for _, item := range draft.Items {
			if item.ID == strings.TrimPrefix(id, "$") {
				if item.Decision != nil && item.Decision.Action == "same_entity" {
					return item.Decision.TargetEntityID
				}
				return item.EntityID
			}
		}
	}
	return id
}

// Freeze references before a deferred item leaves its original batch.
func shelfResolvedOperation(draft *types.ShelfChangeset, op types.ShelfOperation) types.ShelfOperation {
	op.ParentID = shelfReference(draft, op.ParentID)
	op.EntityID = shelfReference(draft, op.EntityID)
	if op.Classification != nil {
		classification := *op.Classification
		classification.OwnerID = shelfReference(draft, classification.OwnerID)
		classification.RequirementID = shelfReference(draft, classification.RequirementID)
		classification.RequirementIDs = shelfRequirementReferences(draft, classification.RequirementIDs)
		classification.Conditions = shelfResolvedConditions(draft, classification.Conditions)
		op.Classification = &classification
	}
	if op.Document != nil {
		record := *op.Document
		record.OwnerID = shelfReference(draft, record.OwnerID)
		record.RequirementID = shelfReference(draft, record.RequirementID)
		record.RequirementIDs = shelfRequirementReferences(draft, record.RequirementIDs)
		record.Conditions = shelfResolvedConditions(draft, record.Conditions)
		op.Document = &record
	}
	if op.Certification != nil {
		record := *op.Certification
		record.OwnerID = shelfReference(draft, record.OwnerID)
		record.CellPartID = shelfReference(draft, record.CellPartID)
		record.Conditions = shelfResolvedConditions(draft, record.Conditions)
		op.Certification = &record
	}
	if op.Relation != nil {
		relation := *op.Relation
		relation.SubjectID = shelfReference(draft, relation.SubjectID)
		relation.ObjectID = shelfReference(draft, relation.ObjectID)
		op.Relation = &relation
	}
	if op.SaleStatuses != nil {
		values := append([]types.ShelfSaleStatus{}, (*op.SaleStatuses)...)
		for i := range values {
			values[i].Conditions = shelfResolvedConditions(draft, values[i].Conditions)
		}
		op.SaleStatuses = &values
	}
	if op.ParameterValues != nil {
		fields := map[string][]types.ShelfParameterValue{}
		for key, raw := range *op.ParameterValues {
			values := append([]types.ShelfParameterValue{}, raw...)
			for i := range values {
				values[i].Conditions = shelfResolvedConditions(draft, values[i].Conditions)
			}
			fields[key] = values
		}
		op.ParameterValues = &fields
	}
	if op.Library != nil && op.Library.EntitySchema != nil {
		patch := *op.Library
		schema := *patch.EntitySchema
		schema.Fields = append([]types.EntityFieldDefinition{}, schema.Fields...)
		for i := range schema.Fields {
			schema.Fields[i].ProductLineIDs = append([]string{}, schema.Fields[i].ProductLineIDs...)
			for j, id := range schema.Fields[i].ProductLineIDs {
				schema.Fields[i].ProductLineIDs[j] = shelfReference(draft, id)
			}
		}
		patch.EntitySchema = &schema
		op.Library = &patch
	}
	return op
}

func shelfResolvedConditions(draft *types.ShelfChangeset, raw []types.ShelfCondition) []types.ShelfCondition {
	conditions := append([]types.ShelfCondition{}, raw...)
	for i := range conditions {
		conditions[i].PartIDs = append([]string{}, conditions[i].PartIDs...)
		for j, id := range conditions[i].PartIDs {
			conditions[i].PartIDs[j] = shelfReference(draft, id)
		}
	}
	return conditions
}

func (s *EntityService) executeShelf(ctx context.Context, draft *types.ShelfChangeset, item *types.ShelfChangesetItem) (any, error) {
	op, err := s.prepareShelfCertification(ctx, draft, shelfResolvedOperation(draft, item.Operation))
	if err != nil {
		return nil, err
	}
	if op.Type == "import_notice" {
		return op.Issues, nil
	}
	if op.ImportLocation != nil && draft.Source.Type == "import" {
		source := draft.Source
		source.Worksheet = op.ImportLocation.Worksheet
		source.Cells = op.ImportLocation.Cells
		ctx = context.WithValue(ctx, shelfWriteContextKey{}, shelfWriteContext{source, draft.ID})
	}
	if op.Type == "save_import_plan" {
		return s.writeShelfImportPlan(ctx, draft, *op.ImportPlan)
	}
	if op.Type == "classify_document" || op.Type == "select_document_version" {
		return s.writeShelfDocumentEdit(ctx, draft, op)
	}
	if op.Type == "reparse_document" {
		return s.writeShelfReparse(ctx, draft, op)
	}
	confirmations := []types.ManagedNameConfirmation{}
	if item.Decision != nil {
		confirmations = item.Decision.NameConfirmations
	}
	if op.Type == "add_document_version" {
		return s.writeShelfDocument(ctx, draft, item, op)
	}
	if op.Type == "create_certification" || op.Type == "update_certification" {
		return s.writeShelfCertification(ctx, draft, item, op)
	}
	if op.Type == "configure_shelf" {
		var before types.Library
		if err := s.db.WithContext(ctx).Where("id=? AND space_id=?", draft.LibraryID, draft.SpaceID).First(&before).Error; err != nil {
			return nil, err
		}
		if op.LibraryUpdatedAt == nil {
			return nil, apperrors.NewBadRequestError("library_updated_at is required")
		}
		if !before.UpdatedAt.Equal(*op.LibraryUpdatedAt) {
			return nil, apperrors.NewConflictError("shelf settings changed; reload before saving")
		}
		bound := &libraryService{db: s.db, repo: repository.NewLibraryRepository(s.db)}
		result, err := bound.ConfigureLibrary(ctx, draft.LibraryID, *op.Library)
		if err != nil {
			return nil, err
		}
		return result, NewOperationLogService(s.db).RecordTx(ctx, s.db, "configure_shelf", draft.LibraryID, before, result)
	}
	if op.Type == "create_entity" {
		input := *op.Entity
		if input.Kind == "document_file" {
			return nil, apperrors.NewBadRequestError("use add_document_version to create a document file")
		}
		if input.Kind == "certification" {
			return nil, apperrors.NewBadRequestError("use create_certification to create a certification record")
		}
		input.NameConfirmations = append(append([]types.ManagedNameConfirmation{}, input.NameConfirmations...), confirmations...)
		if draft.Source.Type == "import" {
			input.EffectiveAt = draft.Source.FileDate
		}
		if strings.TrimSpace(input.Name) == "" || input.Kind == "" {
			return nil, apperrors.NewBadRequestError("name and kind are required")
		}
		if item.Decision != nil && item.Decision.Action == "same_entity" {
			target, err := s.shelfEntity(ctx, draft.LibraryID, item.Decision.TargetEntityID)
			if err != nil {
				return nil, err
			}
			if input.Kind == "document_requirement" && requirementListName(target.Attributes) != requirementListName(input.Attributes) {
				return nil, apperrors.NewBadRequestError("uncertain: same requirement must belong to the same list")
			}
			if target.Kind != input.Kind || target.ArchivedAt != nil {
				return nil, apperrors.NewBadRequestError("same entity must be an active entity in the same kind")
			}
			aliases := append(append([]string{}, target.Aliases...), input.Aliases...)
			aliases = append(aliases, input.Name)
			return s.UpdateManagedEntity(ctx, target.ID, types.ManagedEntityPatch{Revision: item.Decision.TargetRevision, Aliases: &aliases, NameConfirmations: input.NameConfirmations})
		}
		entity, err := s.createManagedEntity(ctx, draft.LibraryID, input, item.EntityID)
		if err != nil {
			return nil, err
		}
		if op.ParentID != "" {
			relation := types.ManagedRelationInput{SubjectID: entity.ID, ObjectID: op.ParentID, RelationType: "belongs_to"}
			if draft.Source.Type == "import" {
				relation.ValidFrom = draft.Source.FileDate
			}
			if _, err := s.SaveManagedRelation(ctx, draft.LibraryID, "", relation); err != nil {
				return nil, err
			}
		}
		return entity, nil
	}
	if op.Type == "save_relation" || op.Type == "end_relation" {
		relationType := ""
		if op.Relation != nil {
			relationType = op.Relation.RelationType
		}
		if slices.Contains([]string{"has_document", "fulfills"}, relationType) {
			return nil, apperrors.NewBadRequestError("use add_document_version to classify a document")
		}
		if op.RelationID != "" {
			var existing types.Relation
			if err := s.db.WithContext(ctx).Where("id=? AND space_id=? AND library_id=?", op.RelationID, draft.SpaceID, draft.LibraryID).First(&existing).Error; err != nil {
				return nil, managedWriteError(err)
			}
			if slices.Contains([]string{"has_document", "fulfills"}, existing.RelationType) {
				return nil, apperrors.NewBadRequestError("document classification is retained with its versions")
			}
		}
	}
	if op.Type == "save_relation" {
		input := *op.Relation
		if draft.Source.Type == "import" {
			input.ValidFrom = draft.Source.FileDate
		}
		input.SubjectID = shelfReference(draft, input.SubjectID)
		input.ObjectID = shelfReference(draft, input.ObjectID)
		if input.SubjectID == "" || input.ObjectID == "" || input.RelationType == "" {
			return nil, apperrors.NewBadRequestError("relationship endpoints and type are required")
		}
		if op.RelationID != "" {
			if _, err := s.shelfBefore(ctx, draft, item); err != nil {
				return nil, err
			}
		}
		return s.SaveManagedRelation(ctx, draft.LibraryID, op.RelationID, input)
	}
	if op.Type == "end_relation" {
		if _, err := s.shelfBefore(ctx, draft, item); err != nil {
			return nil, err
		}
		var err error
		if draft.Source.Type == "import" {
			op.ValidTo = draft.Source.FileDate
		}
		if op.ValidTo != nil {
			err = s.DeleteManagedRelation(ctx, op.RelationID, op.Revision, *op.ValidTo)
		} else {
			err = s.DeleteManagedRelation(ctx, op.RelationID, op.Revision)
		}
		if err != nil {
			return nil, err
		}
		var ended types.Relation
		err = s.db.WithContext(ctx).Where("id=? AND space_id=? AND library_id=?", op.RelationID, draft.SpaceID, draft.LibraryID).First(&ended).Error
		return &ended, err
	}
	id := shelfReference(draft, op.EntityID)
	if id == "" {
		return nil, apperrors.NewBadRequestError("entity_id is required")
	}
	entity, err := s.shelfEntity(ctx, draft.LibraryID, id)
	if err != nil {
		return nil, err
	}
	patch := types.ManagedEntityPatch{Revision: op.Revision, NameConfirmations: confirmations}
	switch op.Type {
	case "set_sale_statuses", "set_parameter_values":
		return s.setShelfModelValues(ctx, draft, entity, op)
	case "update_entity":
		patch = *op.Patch
		if patch.Kind != nil && *patch.Kind != entity.Kind && (entity.Kind == "document_file" || *patch.Kind == "document_file") {
			return nil, apperrors.NewBadRequestError("document file kind cannot be changed")
		}
		if patch.Kind != nil && *patch.Kind != entity.Kind && (entity.Kind == "certification" || *patch.Kind == "certification") {
			return nil, apperrors.NewBadRequestError("certification kind cannot be changed")
		}
		if patch.Attributes != nil && entity.Kind == "certification" {
			return nil, apperrors.NewBadRequestError("use update_certification to replace a certification record")
		}
		patch.NameConfirmations = append(append([]types.ManagedNameConfirmation{}, patch.NameConfirmations...), confirmations...)
	case "add_alias":
		if strings.TrimSpace(op.Alias) == "" {
			return nil, apperrors.NewBadRequestError("alias is required")
		}
		aliases := append(append([]string{}, entity.Aliases...), op.Alias)
		patch.Aliases = &aliases
	case "archive_entity", "missing_entity":
		if op.Type == "missing_entity" && (item.Decision == nil || item.Decision.Action != "retire") {
			return entity, nil
		}
		archived := true
		patch.Archived = &archived
	case "set_entity_link":
		if op.Revision != entity.Revision {
			return nil, apperrors.NewConflictError("entity changed; reload before linking")
		}
		if err := s.SetEntryEntityLink(ctx, op.EntryID, id, op.Excluded, op.RelationType); err != nil {
			return nil, err
		}
		var link types.EntryEntity
		err := s.db.WithContext(ctx).Where("entry_id=? AND entity_id=? AND space_id=?", op.EntryID, id, draft.SpaceID).First(&link).Error
		return &link, err
	default:
		return nil, apperrors.NewBadRequestError("unsupported shelf operation")
	}
	if draft.Source.Type == "import" && patch.Attributes != nil {
		patch.EffectiveAt = draft.Source.FileDate
	}
	return s.UpdateManagedEntity(ctx, id, patch)
}

func (s *EntityService) shelfSameNameCandidates(ctx context.Context, draft *types.ShelfChangeset, input *types.ManagedEntityInput) ([]types.ManagedNameConfirmation, error) {
	var entities []types.Entity
	if err := s.db.WithContext(ctx).Where("library_id=? AND space_id=? AND kind=? AND archived_at IS NULL AND merged_into_id=''", draft.LibraryID, draft.SpaceID, input.Kind).Find(&entities).Error; err != nil {
		return nil, err
	}
	if err := s.loadAliases(ctx, draft.SpaceID, entities); err != nil {
		return nil, err
	}
	candidates := []types.ManagedNameConfirmation{}
	for _, entity := range entities {
		for _, name := range append([]string{entity.CanonicalName, entity.Code}, entity.Aliases...) {
			if name != "" && normalizeEntityName(name) == normalizeEntityName(input.Name) {
				candidates = append(candidates, types.ManagedNameConfirmation{Name: input.Name, OtherEntityID: entity.ID, OtherKind: entity.Kind})
				break
			}
		}
	}
	return candidates, nil
}

func shelfChangedFields(raw types.JSON) map[string]any {
	var value map[string]any
	_ = json.Unmarshal(raw, &value)
	fields := map[string]any{}
	for _, key := range []string{"canonical_name", "code", "description", "kind", "aliases"} {
		fields[key] = value[key]
	}
	if attributes, ok := value["attributes"].(map[string]any); ok {
		for key, value := range attributes {
			fields["attributes."+key] = value
		}
	}
	return fields
}

func (s *EntityService) shelfManualBefore(ctx context.Context, changesetID, id string, before, after types.JSON) (map[string]any, error) {
	old, next := shelfChangedFields(before), shelfChangedFields(after)
	changed := map[string]any{}
	for key, value := range old {
		if !reflect.DeepEqual(value, next[key]) {
			changed[key] = value
		}
	}
	if len(changed) == 0 {
		return nil, nil
	}
	var logs []types.OperationLog
	if err := s.db.WithContext(ctx).Where("space_id=? AND object_id=? AND changeset_id<>? AND operation_type IN ?", types.MustSpaceIDFromContext(ctx), id, changesetID, []string{"create_entity", "update_entity"}).Order("created_at DESC,id DESC").Find(&logs).Error; err != nil {
		return nil, err
	}
	manual := map[string]any{}
	for key, value := range changed {
		for _, log := range logs {
			a, b := shelfChangedFields(log.After), shelfChangedFields(log.Before)
			if reflect.DeepEqual(a[key], b[key]) {
				continue
			}
			if (log.Source.Type == "web" || log.Source.Type == "agent" || log.Source.Type == "") && reflect.DeepEqual(a[key], value) {
				if attribute, ok := strings.CutPrefix(key, "attributes."); ok {
					attrs, exists := manual["attributes"].(map[string]any)
					if !exists {
						attrs = map[string]any{}
						manual["attributes"] = attrs
					}
					attrs[attribute] = value
				} else {
					manual[key] = value
				}
			}
			break
		}
	}
	return manual, nil
}

// Read the affected entry ids before commit so a failed read still rolls back.
// Scheduling runs only after a successful commit, never during preview.
func (s *EntityService) shelfHeaderEntries(ctx context.Context, tx *gorm.DB, draft *types.ShelfChangeset) ([]string, error) {
	if s.headerRefresher == nil {
		return nil, nil
	}
	entityIDs, entryIDs := []string{}, []string{}
	for _, item := range draft.Items {
		if item.Decision == nil || slices.Contains([]string{"discard", "defer", "keep"}, item.Decision.Action) {
			continue
		}
		if item.Operation.Type == "set_entity_link" {
			entryIDs = append(entryIDs, item.Operation.EntryID)
		}
		var entity types.Entity
		if json.Unmarshal(item.After, &entity) == nil && entity.ID != "" && entity.CanonicalName != "" {
			entityIDs = append(entityIDs, entity.ID)
		}
	}
	if len(entityIDs) > 0 {
		var linked []string
		if err := tx.WithContext(ctx).Model(&types.EntryEntity{}).Where("space_id=? AND entity_id IN ?", draft.SpaceID, entityIDs).Distinct("entry_id").Pluck("entry_id", &linked).Error; err != nil {
			return nil, err
		}
		entryIDs = append(entryIDs, linked...)
	}
	slices.Sort(entryIDs)
	return slices.Compact(entryIDs), nil
}
func (s *EntityService) scheduleShelfHeaders(ctx context.Context, ids []string) {
	if s.headerRefresher == nil || entityDatabaseInTransaction(s.db) {
		return
	}
	for _, id := range ids {
		s.headerRefresher.ScheduleRefresh(ctx, id)
	}
}

func shelfRequirementReferences(draft *types.ShelfChangeset, ids []string) []string {
	result := make([]string, len(ids))
	for i, id := range ids {
		result[i] = shelfReference(draft, id)
	}
	return result
}
