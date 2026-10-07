package service

import (
	"encoding/json"
	"fmt"
	"math"
	"reliquary/internal/types"
	"strings"
)

// Template aliases normalize reads without changing the attributed source record.
func canonicalContextField(domain *types.KnowledgeDomain, label string) string {
	if domain != nil {
		for _, field := range domain.Fields {
			for _, name := range append([]string{field.Key, field.Label}, field.Aliases...) {
				if normalizeContextName(label) == normalizeContextName(name) {
					return field.Key
				}
			}
		}
		for _, relation := range domain.Relations {
			for _, name := range append([]string{relation.Key, relation.Label}, relation.Aliases...) {
				if normalizeContextName(label) == normalizeContextName(name) {
					return relation.Key
				}
			}
		}
	}
	return strings.TrimSpace(label)
}

func contextFieldNames(domain *types.KnowledgeDomain, label string) []string {
	canonical := canonicalContextField(domain, label)
	names := []string{canonical}
	if domain != nil {
		for _, field := range domain.Fields {
			if field.Key == canonical {
				names = append(names, append([]string{field.Label}, field.Aliases...)...)
			}
		}
		for _, relation := range domain.Relations {
			if relation.Key == canonical {
				names = append(names, append([]string{relation.Label}, relation.Aliases...)...)
			}
		}
	}
	for i := range names {
		names[i] = normalizeContextName(names[i])
	}
	return uniqueContextIDs(names)
}

func contextDomainPrompt(domain *types.KnowledgeDomain) string {
	var prompt strings.Builder
	prompt.WriteString("Use the domain template's canonical field and relation keys for equivalent source labels. Preserve source labels in source_predicate. Fields outside the template remain open fields under their original source names. Keep numeric values and units exactly as stated; do not convert or infer them.\n")
	if domain == nil {
		return prompt.String()
	}
	fmt.Fprintf(&prompt, "Domain: %s\nObject types: %s\n", domain.Name, strings.Join(domain.EntityTypes, ", "))
	fields, _ := json.Marshal(domain.Fields)
	relations, _ := json.Marshal(domain.Relations)
	units, _ := json.Marshal(domain.UnitFamilies)
	fmt.Fprintf(&prompt, "Fields (keys, labels, aliases, value types, unit families, applicable object types): %s\nRelations (keys, labels, aliases, subject and object types): %s\nUnit families: %s\n%s", fields, relations, units, domain.ExtractionHints)
	return prompt.String()
}

func contextDomainUnit(domain *types.KnowledgeDomain, unit string) (string, float64, bool) {
	if domain != nil && strings.TrimSpace(unit) != "" {
		for _, family := range domain.UnitFamilies {
			for _, definition := range family.Units {
				for _, name := range append([]string{definition.Name}, definition.Aliases...) {
					if normalizeContextName(unit) == normalizeContextName(name) {
						return family.Key, definition.Factor, true
					}
				}
			}
		}
	}
	return normalizeContextName(unit), 1, false
}

func contextFactMatchesValue(domain *types.KnowledgeDomain, fact types.KnowledgeFact, q types.KnowledgeContextQuery) bool {
	if q.MinValue == nil && q.MaxValue == nil && q.Unit == "" {
		return true
	}
	unit, factor, known := contextDomainUnit(domain, fact.Unit)
	wantedUnit, wantedFactor, wantedKnown := contextDomainUnit(domain, q.Unit)
	if q.Unit != "" && (unit != wantedUnit || known != wantedKnown) {
		return false
	}
	if q.MinValue == nil && q.MaxValue == nil {
		return true
	}
	if fact.Kind != types.KnowledgeFactAttribute || fact.ValueType != "number" {
		return false
	}
	var value float64
	if json.Unmarshal(fact.Value, &value) != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return false
	}
	// Numeric units must belong to a declared family. Unknown units never imply a conversion.
	if domain != nil && q.Unit != "" {
		for _, field := range domain.Fields {
			if field.Key == canonicalContextField(domain, fact.Predicate) && field.UnitFamily != "" && unit != field.UnitFamily {
				return false
			}
		}
	}
	if q.Unit != "" && (!known || !wantedKnown) {
		return false
	}
	// A missing unit must not be interpreted as tokens or another unit.
	if q.Unit == "" && unit != "" {
		return false
	}
	value *= factor
	return (q.MinValue == nil || value >= *q.MinValue*wantedFactor) && (q.MaxValue == nil || value <= *q.MaxValue*wantedFactor)
}
