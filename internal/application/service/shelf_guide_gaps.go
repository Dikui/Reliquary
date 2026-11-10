package service

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

// Each gap list is independently bounded after its full count is computed.
type ShelfGuidePage[T any] struct {
	Items     []T   `json:"items"`
	Total     int64 `json:"total"`
	Truncated bool  `json:"truncated"`
}

func (p *ShelfGuidePage[T]) add(item T) {
	p.Total++
	if len(p.Items) < 100 {
		p.Items = append(p.Items, item)
	} else {
		p.Truncated = true
	}
}

type ShelfGuideDocumentGap struct {
	ModelID      string `json:"model_id"`
	ModelName    string `json:"model_name"`
	CurrentStage string `json:"current_stage,omitempty"`
	types.ShelfChecklistItem
}

type ShelfGuideSaleGap struct {
	ModelID    string `json:"model_id"`
	ModelName  string `json:"model_name"`
	Region     string `json:"region"`
	RegionName string `json:"region_name"`
}

type ShelfGuidePending struct {
	ID        string                `json:"id"`
	Revision  int64                 `json:"revision"`
	Source    types.OperationSource `json:"source"`
	UpdatedAt time.Time             `json:"updated_at"`
}

type ShelfGuideGaps struct {
	NodeID            string                                `json:"node_id"`
	Kind              string                                `json:"kind"`
	ModelCount        int64                                 `json:"model_count"`
	Documents         ShelfGuidePage[ShelfGuideDocumentGap] `json:"documents"`
	SaleStatuses      ShelfGuidePage[ShelfGuideSaleGap]     `json:"sale_statuses"`
	PendingChangesets ShelfGuidePage[ShelfGuidePending]     `json:"pending_changesets"`
}

func (s *EntityService) shelfGuideGaps(ctx context.Context, library *types.Library, nodeID string, at time.Time) (*ShelfGuideGaps, error) {
	node, err := s.shelfGet(ctx, library.ID, nodeID, types.ShelfReadOptions{AsOf: &at})
	if err != nil {
		return nil, err
	}
	if !slices.Contains([]string{"product_line", "platform", "product", "model", "part"}, node.Entity.Kind) {
		return nil, apperrors.NewBadRequestError("guide node must be a product line, platform, product, model or part")
	}
	result := &ShelfGuideGaps{NodeID: nodeID, Kind: node.Entity.Kind,
		Documents:         ShelfGuidePage[ShelfGuideDocumentGap]{Items: []ShelfGuideDocumentGap{}},
		SaleStatuses:      ShelfGuidePage[ShelfGuideSaleGap]{Items: []ShelfGuideSaleGap{}},
		PendingChangesets: ShelfGuidePage[ShelfGuidePending]{Items: []ShelfGuidePending{}},
	}
	data, err := s.loadShelfChecklist(ctx, library, at)
	if err != nil {
		return nil, err
	}
	related := map[string]bool{nodeID: true}
	addModel := func(model types.ShelfNodeDetail) {
		result.ModelCount++
		related[model.Entity.ID] = true
		for _, ancestor := range data.ancestors(model.Entity.ID) {
			related[ancestor.ID] = true
		}
		if model.Shelf == nil {
			return
		}
		for _, group := range model.Shelf.Composition {
			for _, part := range group.Parts {
				related[part.ID] = true
			}
		}
		for _, row := range model.Shelf.Checklist {
			related[row.Requirement.ID] = true
			if row.Status == "missing" || row.Status == "draft_only" {
				result.Documents.add(ShelfGuideDocumentGap{ModelID: model.Entity.ID, ModelName: model.Entity.CanonicalName, CurrentStage: model.Shelf.CurrentStage, ShelfChecklistItem: row})
			}
		}
		for _, region := range library.ShelfConfig.Regions {
			if !slices.ContainsFunc(model.Shelf.SaleStatuses, func(status types.ShelfSaleStatus) bool { return status.Region == region.Key }) {
				result.SaleStatuses.add(ShelfGuideSaleGap{ModelID: model.Entity.ID, ModelName: model.Entity.CanonicalName, Region: region.Key, RegionName: region.Name})
			}
		}
	}
	if node.Entity.Kind == "model" {
		addModel(*node)
	} else if node.Entity.Kind != "part" {
		for offset := 0; ; offset += 100 {
			page, err := s.shelfQuery(ctx, library.ID, types.ShelfQueryRequest{AncestorID: nodeID, AsOf: &at, Limit: 100, Offset: offset})
			if err != nil {
				return nil, err
			}
			for _, item := range page.Items {
				addModel(item.ShelfNodeDetail)
			}
			if int64(offset+len(page.Items)) >= page.Total {
				break
			}
		}
	}
	// Include saved certifications, files and relation IDs so proposals that
	// edit an existing record are visible even without repeating its owner.
	for id, entity := range data.nodes {
		if entity.Kind == "certification" {
			var cert types.ShelfCertification
			if types.DecodeShelfValue(entity.Attributes["certification"], &cert) == nil && related[cert.OwnerID] {
				related[id] = true
			}
		}
	}
	for _, document := range data.documents {
		if related[documentIDValue(document.OwnerID)] {
			related[document.FileID] = true
		}
	}
	for _, edge := range data.edges {
		if related[edge.SubjectID] || node.Entity.Kind == "part" && edge.ObjectID == nodeID {
			related[edge.ID] = true
		}
	}
	for page := 1; ; page++ {
		drafts, err := s.ShelfChangesets(ctx, library.ID, "pending", "", page, 100)
		if err != nil {
			return nil, err
		}
		for _, draft := range drafts.Items {
			if shelfGuideChangesetInvolves(draft, related) {
				result.PendingChangesets.add(ShelfGuidePending{ID: draft.ID, Revision: draft.Revision, Source: draft.Source, UpdatedAt: draft.UpdatedAt})
			}
		}
		if int64(page*100) >= drafts.Total {
			break
		}
	}
	return result, nil
}

func shelfGuideChangesetInvolves(draft types.ShelfChangeset, related map[string]bool) bool {
	for _, item := range draft.Items {
		op := shelfResolvedOperation(&draft, item.Operation)
		if op.Type == "configure_shelf" {
			return true
		}
		ids := append(shelfGuideOperationReferences(op), item.EntityID)
		if item.Decision != nil {
			ids = append(ids, item.Decision.TargetEntityID)
			for _, confirmation := range item.Decision.NameConfirmations {
				ids = append(ids, confirmation.OtherEntityID)
			}
		}
		// Existing relations and files can change owner. Retain explicit old
		// identities without interpreting arbitrary attributes or issue details.
		var before struct {
			ID        string `json:"id"`
			OwnerID   string `json:"owner_id"`
			SubjectID string `json:"subject_id"`
			ObjectID  string `json:"object_id"`
		}
		if json.Unmarshal(item.Before, &before) == nil {
			ids = append(ids, before.ID, before.OwnerID, before.SubjectID, before.ObjectID)
		}
		for _, id := range ids {
			if related[id] {
				return true
			}
		}
	}
	return false
}

func shelfGuideOperationReferences(op types.ShelfOperation) []string {
	ids := []string{op.EntityID, op.CandidateEntityID, op.RelationID}
	conditions := []types.ShelfCondition{}
	if op.Relation != nil {
		ids = append(ids, op.Relation.SubjectID, op.Relation.ObjectID)
	}
	if op.Document != nil {
		ids = append(ids, op.Document.OwnerID)
		conditions = append(conditions, op.Document.Conditions...)
	}
	if op.Classification != nil {
		ids = append(ids, op.Classification.OwnerID)
		conditions = append(conditions, op.Classification.Conditions...)
	}
	if op.Certification != nil {
		ids = append(ids, op.Certification.OwnerID, op.Certification.CellPartID)
		conditions = append(conditions, op.Certification.Conditions...)
	}
	if op.SaleStatuses != nil {
		for _, status := range *op.SaleStatuses {
			conditions = append(conditions, status.Conditions...)
		}
	}
	if op.ParameterValues != nil {
		for _, values := range *op.ParameterValues {
			for _, value := range values {
				conditions = append(conditions, value.Conditions...)
			}
		}
	}
	for _, condition := range conditions {
		ids = append(ids, condition.PartIDs...)
	}
	return ids
}
