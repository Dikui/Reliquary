package service

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

// Re-run imported identity checks for every preview and confirmation, including
// corrected operations submitted by MCP. No name mapping is saved in the plan.
func (s *EntityService) prepareShelfCertification(ctx context.Context, draft *types.ShelfChangeset, op types.ShelfOperation) (types.ShelfOperation, error) {
	if op.Certification == nil {
		return op, nil
	}
	record := *op.Certification
	library, err := s.shelfLibrary(ctx, s.db, draft.LibraryID)
	if err != nil {
		return op, err
	}
	suggestions := shelfCertificationSplitSuggestions(library.ShelfConfig, record, op.CertificationOwner != nil && !slices.Contains(op.CertificationOwner.MappedFields, "name"))
	details := map[string]any{}
	if len(suggestions) > 1 {
		details["split_suggestions"] = suggestions
	}
	standard, year, standardErr := library.ShelfConfig.ResolveStandardYear(record.Standard, record.Year)
	if standardErr == nil {
		record.Standard = standard.Key
		record.Year = year
	}
	if len(suggestions) > 1 {
		standardErr = apperrors.NewBadRequestError("uncertain: multiple standards need explicit split confirmation or correction")
	}
	if op.CertificationOwner == nil {
		if standardErr != nil {
			return op, apperrors.NewBadRequestError(standardErr.Error()).WithDetails(details)
		}
		op.Certification = &record
		return op, nil
	}
	missing := []string{}
	for _, field := range []struct{ name, value string }{{"certification_object", record.CertificationObject}, {"region", record.Region}} {
		if strings.TrimSpace(field.value) == "" {
			missing = append(missing, field.name)
		}
	}
	if len(missing) > 0 {
		details["missing_fields"] = missing
		return op, apperrors.NewBadRequestError("certification object and region are required").WithDetails(details)
	}
	if standardErr != nil {
		return op, apperrors.NewBadRequestError(standardErr.Error()).WithDetails(details)
	}
	builder, err := s.newImportMapBuilder(ctx, library, &types.ShelfImportPlan{}, shelfCertificationTime(draft, op))
	if err != nil {
		return op, err
	}
	if record.OwnerID == "" {
		owner, err := builder.recordOwner(types.ShelfImportSheet{OwnerKind: op.CertificationOwner.Kind, ProductLineID: op.CertificationOwner.ProductLineID}, op.CertificationOwner.Name)
		if err != nil {
			return op, apperrors.NewBadRequestError(err.Error()).WithDetails(map[string]any{"owner_name": op.CertificationOwner.Name, "owner_kind": op.CertificationOwner.Kind})
		}
		record.OwnerID = owner.ID
	} else {
		owner, err := shelfCertificationOwner(s.db, library, record.OwnerID)
		if err != nil {
			return op, err
		}
		if owner.Kind != op.CertificationOwner.Kind {
			return op, apperrors.NewBadRequestError("uncertain: selected owner has a different kind")
		}
		if _, err := builder.recordOwner(types.ShelfImportSheet{OwnerKind: owner.Kind, ProductLineID: op.CertificationOwner.ProductLineID}, owner.ID); err != nil {
			return op, apperrors.NewBadRequestError(err.Error())
		}
	}
	op.Certification = &record
	return op, nil
}

func shelfCertificationSplitSuggestions(config types.ShelfConfig, record types.ShelfCertificationInput, useStandardName bool) []types.ShelfCertificationInput {
	names := strings.FieldsFunc(record.Standard, func(r rune) bool { return strings.ContainsRune("、，,;；\n\r", r) })
	filtered := names[:0]
	for _, name := range names {
		if strings.TrimSpace(name) != "" {
			filtered = append(filtered, name)
		}
	}
	names = filtered
	if len(names) < 2 {
		return nil
	}
	result := make([]types.ShelfCertificationInput, 0, len(names))
	for _, name := range names {
		suggestion := record
		if useStandardName {
			suggestion.Name = name
		}
		suggestion.Standard = strings.TrimSpace(name)
		if standard, year, err := config.ResolveStandardYear(suggestion.Standard, suggestion.Year); err == nil {
			suggestion.Standard = standard.Key
			suggestion.Year = year
		}
		result = append(result, suggestion)
	}
	return result
}

// The first successful key lookup may occur after the submitter corrects an
// owner. Preserve fields absent from the worksheet, as normal imports do.
func mergeShelfImportedCertification(item *types.ShelfChangesetItem, entity *types.Entity) error {
	fields := item.Operation.CertificationOwner.MappedFields
	if len(fields) == 0 {
		return nil
	}
	var existing, proposed map[string]any
	if err := types.DecodeShelfValue(entity.Attributes["certification"], &existing); err != nil {
		return err
	}
	if err := json.Unmarshal(shelfJSON(item.Operation.Certification.ShelfCertification), &proposed); err != nil {
		return err
	}
	fields = append(append([]string{}, fields...), "owner_id", "standard", "certification_object", "region", "cell_part_id")
	for _, field := range fields {
		if value, ok := proposed[field]; ok {
			existing[field] = value
		} else {
			delete(existing, field)
		}
	}
	if item.Operation.Certification.Year != 0 {
		existing["year"] = item.Operation.Certification.Year
	}
	if item.Operation.Certification.Conditions != nil {
		existing["conditions"] = proposed["conditions"]
	}
	return types.DecodeShelfValue(existing, &item.Operation.Certification.ShelfCertification)
}
