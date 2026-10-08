package modelcontext

import (
	"encoding/json"
	"strings"

	"reliquary/internal/models/chat"
	"reliquary/internal/types"
)

const (
	toolDatabaseQuery = "database_query"
	toolDataAnalysis  = "data_analysis"
)

// sourceKeySpace identifies which source-handle space an ID-bearing JSON key
// belongs to.
type sourceKeySpace int

const (
	spaceChunk sourceKeySpace = iota
	spaceDocument
	spaceDocumentRef // "knowledgeID|title" stored refs; only the ID is durable
	spaceKnowledgeBase
	spaceWeb
)

// sourceKeySpaces is the single table of ID-bearing keys the source codec
// understands. It drives both handle registration (registerSourceIDByKey) and
// the handle-shaped-value decode gate (walkJSON), so registration and decoding
// cannot drift apart. Every key referenced by a toolHandlePolicies sourceIDKeys
// set must appear here (enforced by a test).
var sourceKeySpaces = map[string]sourceKeySpace{
	"chunk_id": spaceChunk, "chunk_ids": spaceChunk,
	"knowledge_id": spaceDocument, "knowledge_ids": spaceDocument, "suspected_knowledge_ids": spaceDocument,
	"source_refs":    spaceDocumentRef,
	"knowledge_base": spaceKnowledgeBase, "knowledge_base_id": spaceKnowledgeBase,
	"knowledge_base_ids": spaceKnowledgeBase, "kb_id": spaceKnowledgeBase, "kb_ids": spaceKnowledgeBase,
	"url": spaceWeb, "urls": spaceWeb,
}

type toolHandlePolicy struct {
	sourceIDKeys   map[string]struct{}
	sourceTextKeys map[string]struct{}
	sourceOutput   bool
}

// toolHandlePolicies is the complete allowlist for built-in tool fields whose
// identifiers need more than the generic resource/source codecs. Field names
// alone are deliberately insufficient: a dynamic MCP tool may use the same
// name with unrelated semantics and must remain opaque.
var toolHandlePolicies = map[string]toolHandlePolicy{
	"knowledge_search":         {sourceIDKeys: map[string]struct{}{"knowledge_base_ids": {}}, sourceOutput: true},
	"grep_chunks":              {sourceOutput: true},
	"list_knowledge_chunks":    {sourceIDKeys: map[string]struct{}{"knowledge_id": {}, "chunk_id": {}}, sourceOutput: true},
	"get_document_info":        {sourceIDKeys: map[string]struct{}{"knowledge_ids": {}}, sourceOutput: true},
	"query_knowledge_graph":    {sourceIDKeys: map[string]struct{}{"knowledge_base_ids": {}}, sourceOutput: true},
	toolDatabaseQuery:          {sourceTextKeys: map[string]struct{}{"sql": {}}, sourceOutput: true},
	toolDataAnalysis:           {sourceIDKeys: map[string]struct{}{"knowledge_id": {}}, sourceTextKeys: map[string]struct{}{"sql": {}}},
	"data_schema":              {sourceIDKeys: map[string]struct{}{"knowledge_id": {}}},
	"web_fetch":                {sourceIDKeys: map[string]struct{}{"url": {}, "urls": {}}, sourceOutput: true},
	"knowledge_report_anomaly": {sourceIDKeys: map[string]struct{}{"knowledge_base_id": {}, "knowledge_ids": {}}},
	"knowledge_import_url":     {sourceIDKeys: map[string]struct{}{"knowledge_base_id": {}, "url": {}}},
	"knowledge_apply_anomaly":  {sourceIDKeys: map[string]struct{}{"knowledge_base_id": {}}},
	"thinking":                 {},
	"todo_write":               {},
}

// HasToolPolicy reports whether a tool has an explicit model-handle policy.
// Built-in tools must declare one (even an empty policy) so that adding a tool
// is a deliberate decision about what the model may see, rather than a silent
// fall-through to fully opaque output.
func HasToolPolicy(toolName string) bool {
	_, ok := toolHandlePolicies[toolName]
	return ok
}

func sourceArgumentAllowed(toolName, key string) bool {
	policy, ok := toolHandlePolicies[toolName]
	if !ok {
		return false
	}
	_, ok = policy.sourceIDKeys[strings.ToLower(key)]
	return ok
}

func sourceOutputAllowed(toolName string) bool {
	if toolName == "" {
		// Preserve the generic low-level facade for non-Agent callers. Agent
		// lifecycles always provide the concrete tool name.
		return true
	}
	policy, ok := toolHandlePolicies[toolName]
	return ok && policy.sourceOutput
}

func sourceCompactionAllowed(toolName string) bool {
	if toolName == "" {
		return true
	}
	_, ok := toolHandlePolicies[toolName]
	return ok
}

// decodeToolPolicies handles the deliberately small set of arguments whose
// model handles are embedded in structured text or belong to a tool-private
// identity space. Generic free text is never rewritten.
func (r *Registry) decodeToolPolicies(call *types.LLMToolCall) {
	if r == nil || call == nil {
		return
	}
	policy, ok := toolHandlePolicies[call.Function.Name]
	if !ok {
		return
	}
	call.Function.Arguments = rewriteJSONStringValues(
		call.Function.Arguments,
		func(key, value string) string {
			if _, ok := policy.sourceTextKeys[key]; ok {
				return r.sources.DecodeKnownQuotedText(value)
			}
			return value
		},
	)
}

// encodeReplayedToolPolicies applies the exact inverse of decodeToolPolicies
// to assistant tool calls replayed into a later model round. Only durable
// values already proven and registered in this request are compacted.
func (r *Registry) encodeReplayedToolPolicies(call *chat.ToolCall) {
	if r == nil || call == nil {
		return
	}
	policy, ok := toolHandlePolicies[call.Function.Name]
	if !ok {
		return
	}
	call.Function.Arguments = rewriteJSONStringValues(
		call.Function.Arguments,
		func(key, value string) string {
			if _, ok := policy.sourceTextKeys[key]; ok {
				return r.sources.CompactKnownText(value)
			}
			return value
		},
	)
}

func (r *Registry) unresolvedPrivateToolHandles(toolName, raw string) []string {
	if r == nil || raw == "" {
		return nil
	}
	policy, ok := toolHandlePolicies[toolName]
	if !ok || len(policy.sourceTextKeys) == 0 {
		return nil
	}
	var unresolved []string
	_ = walkJSONStringValues(raw, func(key, value string) string {
		value = strings.TrimSpace(value)
		if _, ok := policy.sourceTextKeys[key]; ok {
			unresolved = append(unresolved, r.sources.UnresolvedQuotedTextHandles(value)...)
		}
		return value
	})
	return unresolved
}

func rewriteJSONStringValues(raw string, rewrite func(key, value string) string) string {
	var value interface{}
	if json.Unmarshal([]byte(raw), &value) != nil {
		return raw
	}
	value = walkJSONValue("", value, rewrite)
	encoded, err := json.Marshal(value)
	if err != nil {
		return raw
	}
	return string(encoded)
}

func walkJSONStringValues(raw string, rewrite func(key, value string) string) error {
	var value interface{}
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return err
	}
	walkJSONValue("", value, rewrite)
	return nil
}

func walkJSONValue(key string, value interface{}, rewrite func(key, value string) string) interface{} {
	switch typed := value.(type) {
	case string:
		return rewrite(strings.ToLower(key), typed)
	case []interface{}:
		for i := range typed {
			typed[i] = walkJSONValue(key, typed[i], rewrite)
		}
	case map[string]interface{}:
		for childKey, item := range typed {
			typed[childKey] = walkJSONValue(childKey, item, rewrite)
		}
	}
	return value
}
