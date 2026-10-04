package service

import (
	"encoding/json"
	"math"
	"strings"

	"reliquary/internal/types"
)

// A small selection vocabulary, rather than a second schema engine. Unknown
// fields keep their source label. Values, units and quotes remain unchanged.
var contextFieldAliases = map[string][]string{
	"context_length":   {"context_length", "context length", "context window", "context_window", "max context tokens", "上下文长度", "上下文窗口"},
	"local_deployment": {"local_deployment", "local deployment", "offline deployment", "offline_deployment", "本地部署", "离线部署"},
	"license":          {"license", "licence", "许可证", "许可协议"},
	"released_on":      {"released_on", "release date", "release_date", "发布日期"},
	"developed_by":     {"developed_by", "developed by", "开发者", "开发机构"},
	"successor_of":     {"successor_of", "successor of", "previous version", "前一版本", "上一版本"},
}

func canonicalContextField(label string) string {
	normalized := normalizeContextName(label)
	for canonical, aliases := range contextFieldAliases {
		for _, alias := range aliases {
			if normalized == alias {
				return canonical
			}
		}
	}
	return strings.TrimSpace(label)
}

func contextFieldNames(label string) []string {
	canonical := canonicalContextField(label)
	if aliases, ok := contextFieldAliases[canonical]; ok {
		return aliases
	}
	return []string{strings.ToLower(canonical)}
}

const contextSelectionFieldsPrompt = `For model selection, use these field names when the source supports them: context_length (token capacity), local_deployment, license, released_on; relations developed_by and successor_of. Match equivalent source labels to these names. Keep numeric values and units exactly as stated; do not convert or infer them. Other supported fields may retain their source names. Model versions are distinct objects; a family and its version are not aliases. Preserve deployment conditions, limitations, and conflicting claims in the verbatim quote.`

func contextNumericUnit(unit string) (string, float64) {
	switch normalizeContextName(unit) {
	case "token", "tokens", "令牌":
		return "tokens", 1
	case "k tokens", "ktokens", "k token", "千token", "千 tokens":
		return "tokens", 1000
	case "m tokens", "mtokens", "m token", "百万token":
		return "tokens", 1000000
	default:
		return normalizeContextName(unit), 1
	}
}

func contextFactMatchesValue(fact types.KnowledgeFact, q types.KnowledgeContextQuery) bool {
	if q.MinValue == nil && q.MaxValue == nil && q.Unit == "" {
		return true
	}
	unit, factor := contextNumericUnit(fact.Unit)
	wantedUnit, wantedFactor := contextNumericUnit(q.Unit)
	if q.Unit != "" && unit != wantedUnit {
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
	// A missing unit must not be interpreted as tokens or another unit.
	if q.Unit == "" && unit != "" {
		return false
	}
	value *= factor
	return (q.MinValue == nil || value >= *q.MinValue*wantedFactor) && (q.MaxValue == nil || value <= *q.MaxValue*wantedFactor)
}
